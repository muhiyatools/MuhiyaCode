package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type Config struct {
	Settings        contract.Settings
	APIKey          string
	Client          *http.Client
	IdleTimeout     time.Duration
	RequestLifetime time.Duration
	MaxRetries      int
	// StreamRetryObserver, when set, is called once per mid-stream retry so the
	// harness can telemeter transport friction that would otherwise be invisible
	// (the retry itself is silent to the user by design â€” a 2s recovery should
	// not become a notification).
	StreamRetryObserver func(error)
	RawUsageObserver    RawUsageObserver
}

// streamRetryDelay is short on purpose: the connection dropped, it did not rate
// limit us. Long enough to let a transient blip clear, short enough that the
// user reads it as a pause rather than a stall.
const streamRetryDelay = 2 * time.Second

// RawUsagePayload exposes the provider's unmodified usage object for benchmark
// ground-truth logs. It contains no authorization headers or API key.
type RawUsagePayload struct {
	At      time.Time       `json:"at"`
	Model   string          `json:"model"`
	Payload json.RawMessage `json:"payload"`
}

type RawUsageObserver func(RawUsagePayload) error

type OpenAICompatible struct {
	mu            sync.RWMutex
	config        Config
	client        *http.Client
	catalogETag   string
	catalogModels []contract.Model
	// pinRejected latches when an upstream-affinity request came back with a
	// non-retryable status. The pin is a pure optimization â€” it buys a warm
	// cache â€” so the moment a route proves it cannot accept the field, we stop
	// sending it for the rest of the process rather than failing every task on
	// a caching nicety. See the retry loop in Chat.
	// No process-global routing capability latch: request routing is driven by
	// the documented session identifier and remains independent per session.
}

type chatAttempt struct {
	model   contract.Model
	profile ModelProfile
	input   contract.ChatRequest
	number  int
}

func NewOpenAICompatible(config Config) *OpenAICompatible {
	if config.Client == nil {
		config.Client = &http.Client{}
	}
	// These are PAIRED with the gateway's own bounds (gateway repo
	// proxy/handler.go:22 httpClient.Timeout, :49 streamIdleTimeout). Each CLI
	// value sits just INSIDE its gateway counterpart so the CLI â€” the side that
	// can actually recover, via the stream retry in Chat below â€” is the one that
	// times out first. Inverting this would surface the gateway's hard close as
	// an unrecoverable transport error instead of a retried turn.
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 110 * time.Second // gateway cuts a silent stream at 120s
	}
	if config.RequestLifetime <= 0 {
		config.RequestLifetime = 14 * time.Minute // gateway's total upstream bound is 15m
	}
	if config.MaxRetries <= 0 {
		// P0-W3 (UMI-08): one transport retry maximum. With MaxRetries=1 the
		// provider makes at most two upstream calls per logical turn (initial +
		// one pre-byte retry, or initial + one mid-stream retry). The previous
		// default of 3 allowed up to five upstream calls (4 pre-byte + 1
		// mid-stream), multiplying cost on a bad key or malformed request.
		config.MaxRetries = 1
	}
	return &OpenAICompatible{config: config, client: config.Client}
}

// snapshot copies the current config under a short-lived read lock so that
// long-running requests never hold the lock for their whole lifetime; a
// concurrent UpdateConfig would otherwise stall behind in-flight streams.
func (p *OpenAICompatible) snapshot() Config {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

func (p *OpenAICompatible) Chat(ctx context.Context, input contract.ChatRequest) (contract.ChatResponse, error) {
	cfg := p.snapshot()
	model, err := resolveModel(cfg, input.ModelID)
	if err != nil {
		return contract.ChatResponse{}, err
	}
	profile := ResolveCatalogModelProfile(model)
	var last error
	streamRetried := false
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		current := chatAttempt{model: model, profile: profile, input: input, number: attempt + 1}
		response, streamed, retryAfter, err := p.chatOnce(ctx, cfg, current)
		if err == nil {
			return response, nil
		}
		last = err
		var httpErr *HTTPError
		isStatusError := errors.As(err, &httpErr)
		retryable := isStatusError && httpErr.Retryable
		// A TRANSPORT failure before any bytes arrived is retryable. The
		// !isStatusError term matters: isNetworkError only excludes cancellation,
		// so without it an explicitly non-retryable status (400, 401, 404) was
		// promoted back to retryable and re-sent three times â€” a bad API key
		// hammered the gateway, and a malformed request burned the user's clock
		// reproducing the same 400.
		if !retryable && !streamed && !isStatusError && isNetworkError(err) {
			retryable = true
		}
		// A stream that DIED after headers is retried exactly once. It used to be
		// terminal, which meant one dropped connection killed the whole task â€”
		// the turn loop's only move on a Chat error is to abort.
		//
		// The retry is close to free: the partial is discarded, the request bytes
		// are identical, and the prefix we just sent is still warm in the
		// provider's cache, so the second attempt pays cache-hit input rates.
		// Bounded at one so a genuinely dead upstream still surfaces promptly, and
		// never attempted for an HTTP-status failure (that body already landed;
		// re-sending would just repeat it).
		if streamed && !streamRetried && !isStatusError && isNetworkError(err) && ctx.Err() == nil {
			streamRetried = true
			if input.OnStreamReset != nil {
				input.OnStreamReset()
			}
			if cfg.StreamRetryObserver != nil {
				cfg.StreamRetryObserver(err)
			}
			if sleepErr := sleepContext(ctx, streamRetryDelay); sleepErr != nil {
				return contract.ChatResponse{}, sleepErr
			}
			continue
		}
		if streamed || !retryable || attempt == cfg.MaxRetries || ctx.Err() != nil {
			if streamed && input.OnStreamReset != nil {
				input.OnStreamReset()
			}
			return contract.ChatResponse{}, err
		}
		delay := retryAfter
		if delay <= 0 {
			delay = time.Second*time.Duration(1<<attempt) + time.Duration(rand.IntN(250))*time.Millisecond
		}
		if err := sleepContext(ctx, delay); err != nil {
			return contract.ChatResponse{}, err
		}
	}
	return contract.ChatResponse{}, last
}

func (p *OpenAICompatible) chatOnce(parent context.Context, cfg Config, attempt chatAttempt) (contract.ChatResponse, bool, time.Duration, error) {
	model := attempt.model
	profile := attempt.profile
	input := attempt.input
	// Timeout model (feature 007 R1): RequestLifetime is a FIRST-BYTE deadline. It
	// bounds connect + TLS + the wait for response headers, during which DeepSeek may
	// queue a request for up to ~10 minutes behind keep-alive traffic. Once headers
	// arrive and the stream is flowing, this deadline is retired (below) and the
	// rolling IdleTimeout alone governs, so a long answer after a long queue is never
	// cut off by a whole-attempt cap.
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	lifetime := time.AfterFunc(cfg.RequestLifetime, func() { cancel(errStreamStalled) })
	defer lifetime.Stop()
	temperature := profile.Temperature
	if input.Temperature != nil {
		temperature = *input.Temperature
	}
	maxTokens := input.MaxTokens
	if maxTokens <= 0 {
		maxTokens = profile.MaxOutputTokens
	}
	// Capability guard (feature 007 CP-3): never request more output than the
	// provider's documented ceiling. A no-op for the common 16k default; a runaway
	// guard for an over-large caller value.
	if clamped, wasClamped := profile.ClampOutputTokens(maxTokens); wasClamped {
		log.Printf("[gateway] requested max_tokens %d exceeds the documented %s ceiling %d; clamping", maxTokens, profile.Family, profile.OutputTokenLimit)
		maxTokens = clamped
	}
	body := map[string]any{
		"model": model.ID, "messages": replayMessages(input.Messages, profile, input.Reasoning), "temperature": temperature,
		"top_p": profile.TopP, "max_tokens": maxTokens, "stream": true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if len(input.Tools) > 0 {
		body["tools"] = input.Tools
		choice := input.ToolChoice
		if choice == "" {
			choice = "auto"
		}
		body["tool_choice"] = choice
	}
	// Upstream affinity (OpenRouter). A model slug on OpenRouter is served by
	// several upstream providers (minimax-m3 lists NINE), and each keeps its OWN
	// prompt cache. Without a preference OpenRouter is free to re-route between
	// turns, and the next request re-reads the entire conversation uncached â€”
	// with byte-identical input, so nothing on our side can detect or prevent it.
	//
	// allow_fallbacks is set to false to lock prompt cache affinity to the learned
	// upstream provider and prevent silent cache-destroying re-routes. If an
	// upstream outage or 4xx rejection occurs, the gateway client latches
	// rejectUpstreamPin() off for the process and retries without the pin, ensuring
	// tasks continue safely even during provider outages.
	//
	// The field is passed through to OpenRouter verbatim by the gateway and
	// ignored by every provider that does not understand it, so it is safe on
	// any route.
	if input.SessionID != "" && supportsSessionIDBody(cfg.Settings.Provider.BaseURL) {
		body["session_id"] = input.SessionID
	}
	if profile.Family == "minimax" && profileSupportsParam(profile, "reasoning_split") {
		body["reasoning_split"] = true
	}
	if input.Reasoning != "" && profileSupportsParam(profile, "reasoning_effort") {
		// Send the raw effort level. The gateway (X-Muhiya-Effort header, which
		// it prefers over this body field) maps it per provider; we must not
		// downgrade it client-side or DeepSeek would never see "max". Omitted from
		// the body for a model whose profile does not list reasoning_effort (B-2);
		// the header still carries effort, so nothing is lost.
		body["reasoning_effort"] = string(input.Reasoning)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return contract.ChatResponse{}, false, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(cfg, "chat/completions"), bytes.NewReader(payload))
	if err != nil {
		return contract.ChatResponse{}, false, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("X-Client-App", "MuhiyaCode")
	req.Header.Set("HTTP-Referer", "https://muhiya.com")
	req.Header.Set("X-Title", "MuhiyaCode")
	if input.Reasoning != "" {
		req.Header.Set("X-Muhiya-Effort", string(input.Reasoning))
	}
	// X-Session-Id is the OpenRouter cache/routing identity. The legacy
	// X-Muhiya-Session header remains for the Muhiya gateway during migration.
	if input.SessionID != "" {
		req.Header.Set("X-Session-Id", input.SessionID)
		req.Header.Set("X-Muhiya-Session", input.SessionID)
	}
	// RequestID groups every attempt for one logical turn. The attempt header
	// keeps transport retries independently observable.
	// Replaying one attempt remains idempotent at the gateway.
	if input.RequestID != "" {
		req.Header.Set("X-Muhiya-Request-ID", input.RequestID)
	}
	req.Header.Set("X-Muhiya-Attempt", strconv.Itoa(attempt.number))
	req.Header.Set("X-Muhiya-Cache-Epoch", strconv.FormatUint(input.CacheEpoch, 10))
	if model.RecordID != "" {
		req.Header.Set("X-Muhiya-Expected-Model-Record", model.RecordID)
		req.Header.Set("X-Muhiya-Expected-Target-Model", model.TargetModel)
		req.Header.Set("X-Muhiya-Compatibility-Epoch", strconv.Itoa(model.CompatibilityEpoch))
	}
	response, err := cfg.Client.Do(req)
	if err != nil {
		// The first-byte (lifetime) deadline fires here as a context cancel; surface
		// it as a retryable stall, not the raw context.Canceled that reads as a user
		// cancel and is never retried.
		if stall := stalledError(parent, ctx, "waiting for the first byte"); stall != nil {
			return contract.ChatResponse{}, false, 0, stall
		}
		return contract.ChatResponse{}, false, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		httpErr := &HTTPError{Status: response.StatusCode, Body: strings.TrimSpace(string(body)), Retryable: response.StatusCode == 408 || response.StatusCode == 429 || response.StatusCode >= 500}
		return contract.ChatResponse{}, false, retryAfter(response.Header.Get("Retry-After")), httpErr
	}
	if model.RecordID != "" {
		resolvedRecord := strings.TrimSpace(response.Header.Get("X-Muhiya-Resolved-Model-Record"))
		resolvedTarget := strings.TrimSpace(response.Header.Get("X-Muhiya-Resolved-Target-Model"))
		if resolvedRecord != model.RecordID || resolvedTarget != model.TargetModel {
			return contract.ChatResponse{}, false, 0, &HTTPError{
				Status: http.StatusConflict,
				Body: fmt.Sprintf(
					"model resolution receipt mismatch: expected record=%s target=%s, got record=%s target=%s",
					model.RecordID, model.TargetModel, resolvedRecord, resolvedTarget,
				),
				Retryable: false,
			}
		}
	}

	// Streaming begins: headers are in, so retire the first-byte deadline and let the
	// rolling idle timeout govern the stream. Keep-alive comment/empty lines reset
	// this timer (see the scan loop), so a queued-but-alive stream survives well past
	// RequestLifetime as long as the provider keeps signalling (feature 007 R1).
	lifetime.Stop()
	idle := time.AfterFunc(cfg.IdleTimeout, func() { cancel(errStreamStalled) })
	defer idle.Stop()
	acc := NewStreamAccumulatorForProfile(profile, input.OnToken, input.OnReasoningToken)
	rawUsageCount := 0
	var lastRawUsage json.RawMessage
	// 003 (D1): the gateway emits a non-standard final chunk carrying muhiya_log
	// {cost, log_id, usage_estimated} for allowlisted client apps (MuhiyaCode is
	// on the allowlist). Capture the last one; absence is normal (generic/older
	// gateways) and simply leaves cost unavailable â€” never estimated locally.
	var costMeta *muhiyaLogMeta
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		idle.Reset(cfg.IdleTimeout)
		line := scanner.Text()
		if meta, ok := muhiyaLogFromSSELine(line); ok {
			costMeta = meta
		}
		if err := acc.ConsumeLine(line); err != nil {
			return contract.ChatResponse{}, acc.ReceivedData(), 0, err
		}
		if cfg.RawUsageObserver != nil {
			if usage, ok := rawUsageFromSSELine(line); ok {
				// A2/T006: capture rather than record-and-fail inline. A provider
				// (or the gateway's own fallback/stream-recovery paths) can emit
				// zero or several usage frames for one request; that is a metrics
				// imperfection, not a reason to abort an otherwise-successful
				// turn. We record the LAST frame after the stream completes.
				rawUsageCount++
				lastRawUsage = usage
			}
		}
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return contract.ChatResponse{}, acc.ReceivedData(), 0, parent.Err()
			}
			return contract.ChatResponse{}, acc.ReceivedData(), 0, &StalledError{Phase: "mid-stream"}
		default:
		}
	}
	if err := scanner.Err(); err != nil {
		// A stalled read surfaces here as raw context.Canceled; reclassify a
		// timer-induced stall as retryable so the stream retry (and the turn-loop
		// recovery) fire instead of the task dying with "stopped."
		if stall := stalledError(parent, ctx, "mid-stream"); stall != nil {
			return contract.ChatResponse{}, acc.ReceivedData(), 0, stall
		}
		return contract.ChatResponse{}, acc.ReceivedData(), 0, err
	}
	if !acc.ReceivedData() {
		return contract.ChatResponse{}, false, 0, errors.New("provider stream ended without any recognizable data")
	}
	if cfg.RawUsageObserver != nil {
		// A2/T006: tolerate 0 or >1 usage frames. Record the last frame when one
		// was present; a zero-frame response leaves usage as parsed/estimated by
		// the accumulator (and is flagged estimated on the gateway request-log
		// path, not here). Only a genuine recording-hook failure surfaces.
		switch {
		case rawUsageCount == 0:
			log.Printf("[gateway] provider emitted no usage object for one request; usage recorded as estimated")
		case rawUsageCount > 1:
			log.Printf("[gateway] provider emitted %d usage objects for one request; recording the last", rawUsageCount)
			if err := cfg.RawUsageObserver(RawUsagePayload{At: time.Now().UTC(), Model: model.ID, Payload: lastRawUsage}); err != nil {
				return contract.ChatResponse{}, acc.ReceivedData(), 0, fmt.Errorf("record raw provider usage: %w", err)
			}
		default:
			if err := cfg.RawUsageObserver(RawUsagePayload{At: time.Now().UTC(), Model: model.ID, Payload: lastRawUsage}); err != nil {
				return contract.ChatResponse{}, acc.ReceivedData(), 0, fmt.Errorf("record raw provider usage: %w", err)
			}
		}
	}
	result := acc.Result()
	visible, inlineReasoning := SplitThinkBlocks(result.Content)
	reasoning := strings.TrimSpace(strings.Join(nonempty(result.Reasoning, inlineReasoning), "\n"))
	usage := result.Usage
	if costMeta != nil {
		usage.CostUSD = costMeta.cost
		usage.CostEstimated = costMeta.estimated
		usage.CostLogID = costMeta.logID
	}
	usage.Upstream = result.Upstream
	return contract.ChatResponse{Content: visible, Reasoning: reasoning, ReasoningDetails: result.ReasoningDetails, ToolCalls: result.ToolCalls, Usage: usage, FinishReason: result.FinishReason, TruncatedCalls: result.TruncatedCalls}, acc.ReceivedData(), 0, nil
}

// profileSupportsParam reports whether an optional request parameter may be sent
// to this model. An empty SupportedParams is permissive (a generic endpoint we
// have not profiled); otherwise the param must be listed. This keeps the request
// builder honest about model.go's "never emits an unsupported param" contract
// (B-2). Effort still reaches the gateway via the X-Muhiya-Effort header, which
// it prefers, so gating reasoning_effort out of the body loses nothing.
func profileSupportsParam(profile ModelProfile, param string) bool {
	if len(profile.SupportedParams) == 0 {
		return param != "reasoning_effort"
	}
	for _, supported := range profile.SupportedParams {
		if supported == param {
			return true
		}
	}
	return false
}

func supportsSessionIDBody(baseURL string) bool {
	host := strings.ToLower(baseURL)
	return strings.Contains(host, "openrouter.ai") || strings.Contains(host, "muhiya.com")
}

// muhiyaLogMeta is the parsed muhiya_log object from a gateway meta chunk.
type muhiyaLogMeta struct {
	cost      *float64
	estimated bool
	logID     string
}

// muhiyaLogFromSSELine extracts the muhiya_log object from one SSE data line.
// Missing or malformed muhiya_log yields ok=false; the caller treats absence as
// "cost unavailable" and never fabricates a value.
func muhiyaLogFromSSELine(line string) (*muhiyaLogMeta, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data:") {
		return nil, false
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" || data == "[DONE]" || !strings.Contains(data, "muhiya_log") {
		return nil, false
	}
	var envelope struct {
		MuhiyaLog *struct {
			Cost           *float64 `json:"cost"`
			LogID          string   `json:"log_id"`
			UsageEstimated bool     `json:"usage_estimated"`
		} `json:"muhiya_log"`
	}
	if json.Unmarshal([]byte(data), &envelope) != nil || envelope.MuhiyaLog == nil {
		return nil, false
	}
	return &muhiyaLogMeta{cost: envelope.MuhiyaLog.Cost, estimated: envelope.MuhiyaLog.UsageEstimated, logID: envelope.MuhiyaLog.LogID}, true
}

func replayMessages(messages []contract.Message, profile ModelProfile, _ contract.ReasoningTier) []contract.Message {
	result := make([]contract.Message, len(messages))
	copy(result, messages)
	for index := range result {
		// Historical reasoning output is used for single-turn output generation,
		// never multi-turn context replay. Retaining large reasoning payloads in past
		// assistant turns bloats request size and invalidates upstream prompt caches.
		if profile.ReasoningReplay != ReasoningReplayPreserve {
			result[index].ReasoningContent = nil
			result[index].ReasoningDetails = nil
		}
		if profile.Family == "deepseek" && result[index].Role == contract.RoleAssistant && len(result[index].ToolCalls) > 0 {
			empty := ""
			result[index].ReasoningContent = &empty
		}
	}
	return repairToolMessageSequence(result)
}

// repairToolMessageSequence is a last-mile wire invariant: every assistant
// tool_calls message must be followed immediately by one tool result for each
// announced call ID. Valid histories pass through byte-for-byte. If a resumed,
// compacted, or provider-rescued history lost one result, synthesize a bounded
// failure result so the next request can recover instead of being rejected as
// malformed by the upstream. Orphan tool messages are omitted for the same
// reason; they cannot be meaningful without their announcing assistant turn.
func repairToolMessageSequence(messages []contract.Message) []contract.Message {
	result := make([]contract.Message, 0, len(messages))
	for index := 0; index < len(messages); {
		message := messages[index]
		if message.Role == contract.RoleTool {
			index++
			continue
		}
		result = append(result, message)
		index++
		if message.Role != contract.RoleAssistant || len(message.ToolCalls) == 0 {
			continue
		}
		expected := make(map[string]bool, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			expected[call.ID] = true
		}
		seen := make(map[string]bool, len(message.ToolCalls))
		for index < len(messages) && messages[index].Role == contract.RoleTool {
			tool := messages[index]
			if expected[tool.ToolCallID] && !seen[tool.ToolCallID] {
				result = append(result, tool)
				seen[tool.ToolCallID] = true
			}
			index++
		}
		for _, call := range message.ToolCalls {
			if seen[call.ID] {
				continue
			}
			result = append(result, contract.Message{
				Role: contract.RoleTool, ToolCallID: call.ID,
				Content: "Tool result unavailable: the harness repaired a missing historical tool-call pairing. Re-run the call if its result is still needed.",
			})
		}
	}
	return result
}

// StableRequestMessages exposes the provider's final replay representation so
// the orchestrator hashes the same bytes the gateway serializes.
func (p *OpenAICompatible) StableRequestMessages(input contract.ChatRequest) ([]contract.Message, error) {
	cfg := p.snapshot()
	model, err := resolveModel(cfg, input.ModelID)
	if err != nil {
		return nil, err
	}
	return replayMessages(input.Messages, ResolveCatalogModelProfile(model), input.Reasoning), nil
}

func rawUsageFromSSELine(line string) (json.RawMessage, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data:") {
		return nil, false
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" || data == "[DONE]" {
		return nil, false
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(data), &envelope) != nil {
		return nil, false
	}
	usage, exists := envelope["usage"]
	if !exists || bytes.Equal(bytes.TrimSpace(usage), []byte("null")) {
		return nil, false
	}
	return append(json.RawMessage(nil), usage...), true
}

type HTTPError struct {
	Status    int
	Body      string
	Retryable bool
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("provider request failed (%d): %s", e.Status, e.Body)
}

func resolveModel(cfg Config, id string) (contract.Model, error) {
	if strings.TrimSpace(cfg.Settings.Provider.BaseURL) == "" {
		return contract.Model{}, errors.New("missing provider baseUrl")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return contract.Model{}, errors.New("missing API key")
	}
	if id == "" {
		id = cfg.Settings.Provider.ActiveModelID
	}
	for _, model := range cfg.Settings.Provider.Models {
		if model.ID == id || strings.EqualFold(model.ID, id) {
			if model.ContextLimit <= 0 {
				profile := ResolveCatalogModelProfile(model)
				model.ContextLimit = profile.DefaultContextWindow
			}
			return model, nil
		}
	}
	profile := ResolveModelProfile(id)
	return contract.Model{
		ID:           id,
		Name:         id,
		ContextLimit: profile.DefaultContextWindow,
		MaxOutput:    profile.MaxOutputTokens,
		Source:       "dynamic",
	}, nil
}

func endpoint(cfg Config, suffix string) string {
	base := strings.TrimRight(cfg.Settings.Provider.BaseURL, "/")
	if strings.HasSuffix(base, "/chat/completions") && suffix == "chat/completions" {
		return base
	}
	return base + "/" + suffix
}

func retryAfter(value string) time.Duration {
	if seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && seconds > 0 {
		return min(time.Duration(seconds*float64(time.Second)), 30*time.Second)
	}
	return 0
}

func nonempty(values ...string) []string {
	var result []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func unique(values []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// errStreamStalled is the cancel CAUSE our own idle/lifetime timers attach to
// the request context. context.Cause(ctx) returns it only when a timer fired,
// which is how a timeout is told apart from a user cancel (whose cause comes
// from the parent context).
var errStreamStalled = errors.New("provider stream stalled")

// StalledError marks a request that OUR OWN idle or first-byte (lifetime) timer
// aborted because the provider went silent â€” NOT a user cancel. It is
// deliberately not context.Canceled/DeadlineExceeded: isNetworkError, Recoverable,
// and FriendlyRequestError each special-case it as a RETRYABLE stall, whereas a
// raw context.Canceled reads as "the user stopped" and is never retried. That
// distinction is the whole fix â€” a half-open TCP stall used to surface as
// context.Canceled and kill the task with a misleading "stopped."
type StalledError struct {
	Phase string // "waiting for the first byte" or "mid-stream", for diagnostics
}

func (e *StalledError) Error() string {
	if e.Phase != "" {
		return "provider went silent (" + e.Phase + ") past the timeout"
	}
	return "provider went silent past the timeout"
}

// stalledError returns a retryable *StalledError when THIS request's own timer
// cancelled ctx (cause == errStreamStalled) and the parent (user) context is
// still live; otherwise nil, leaving the original error unchanged (a genuine
// user cancel, or an unrelated transport failure).
func stalledError(parent, ctx context.Context, phase string) error {
	if parent.Err() != nil {
		return nil
	}
	if errors.Is(context.Cause(ctx), errStreamStalled) {
		return &StalledError{Phase: phase}
	}
	return nil
}

func isNetworkError(err error) bool {
	var stall *StalledError
	if errors.As(err, &stall) {
		return true // a stall we induced is retryable exactly like a dropped connection
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}
