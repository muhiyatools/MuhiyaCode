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
	// (the retry itself is silent to the user by design — a 2s recovery should
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
	mu     sync.RWMutex
	config Config
	client *http.Client
}

func NewOpenAICompatible(config Config) *OpenAICompatible {
	if config.Client == nil {
		config.Client = &http.Client{}
	}
	// These are PAIRED with the gateway's own bounds (gateway repo
	// proxy/handler.go:22 httpClient.Timeout, :49 streamIdleTimeout). Each CLI
	// value sits just INSIDE its gateway counterpart so the CLI — the side that
	// can actually recover, via the stream retry in Chat below — is the one that
	// times out first. Inverting this would surface the gateway's hard close as
	// an unrecoverable transport error instead of a retried turn.
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 110 * time.Second // gateway cuts a silent stream at 120s
	}
	if config.RequestLifetime <= 0 {
		config.RequestLifetime = 14 * time.Minute // gateway's total upstream bound is 15m
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
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
	profile := ResolveModelProfile(model.ID + " " + model.Name)
	var last error
	streamRetried := false
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		response, streamed, retryAfter, err := p.chatOnce(ctx, cfg, model, profile, input)
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
		// promoted back to retryable and re-sent three times — a bad API key
		// hammered the gateway, and a malformed request burned the user's clock
		// reproducing the same 400.
		if !retryable && !streamed && !isStatusError && isNetworkError(err) {
			retryable = true
		}
		// A stream that DIED after headers is retried exactly once. It used to be
		// terminal, which meant one dropped connection killed the whole task —
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
			if cfg.StreamRetryObserver != nil {
				cfg.StreamRetryObserver(err)
			}
			if sleepErr := sleepContext(ctx, streamRetryDelay); sleepErr != nil {
				return contract.ChatResponse{}, sleepErr
			}
			continue
		}
		if streamed || !retryable || attempt == cfg.MaxRetries || ctx.Err() != nil {
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

func (p *OpenAICompatible) chatOnce(parent context.Context, cfg Config, model contract.Model, profile ModelProfile, input contract.ChatRequest) (contract.ChatResponse, bool, time.Duration, error) {
	// Timeout model (feature 007 R1): RequestLifetime is a FIRST-BYTE deadline. It
	// bounds connect + TLS + the wait for response headers, during which DeepSeek may
	// queue a request for up to ~10 minutes behind keep-alive traffic. Once headers
	// arrive and the stream is flowing, this deadline is retired (below) and the
	// rolling IdleTimeout alone governs, so a long answer after a long queue is never
	// cut off by a whole-attempt cap.
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	lifetime := time.AfterFunc(cfg.RequestLifetime, cancel)
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
	if input.Reasoning != "" {
		// Send the raw effort level. The gateway (X-Muhiya-Effort header, which
		// it prefers over this body field) maps it per provider; we must not
		// downgrade it client-side or DeepSeek would never see "max".
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
	if input.Reasoning != "" {
		req.Header.Set("X-Muhiya-Effort", string(input.Reasoning))
	}
	// X-Muhiya-Session is a per-stream pin; derived once and stable across the
	// whole session so the gateway keeps a stable upstream model routing for
	// the cache namespace. The orchestrator threads it from e.session.ID with
	// a per-stream suffix (":main" / ":sub" / ":aux"). Never regenerated here.
	if input.SessionID != "" {
		req.Header.Set("X-Muhiya-Session", input.SessionID)
	}
	response, err := cfg.Client.Do(req)
	if err != nil {
		return contract.ChatResponse{}, false, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		httpErr := &HTTPError{Status: response.StatusCode, Body: strings.TrimSpace(string(body)), Retryable: response.StatusCode == 408 || response.StatusCode == 429 || response.StatusCode >= 500}
		return contract.ChatResponse{}, false, retryAfter(response.Header.Get("Retry-After")), httpErr
	}

	// Streaming begins: headers are in, so retire the first-byte deadline and let the
	// rolling idle timeout govern the stream. Keep-alive comment/empty lines reset
	// this timer (see the scan loop), so a queued-but-alive stream survives well past
	// RequestLifetime as long as the provider keeps signalling (feature 007 R1).
	lifetime.Stop()
	idle := time.AfterFunc(cfg.IdleTimeout, cancel)
	defer idle.Stop()
	acc := NewStreamAccumulatorForProfile(profile, input.OnToken, input.OnReasoningToken)
	rawUsageCount := 0
	var lastRawUsage json.RawMessage
	// 003 (D1): the gateway emits a non-standard final chunk carrying muhiya_log
	// {cost, log_id, usage_estimated} for allowlisted client apps (MuhiyaCode is
	// on the allowlist). Capture the last one; absence is normal (generic/older
	// gateways) and simply leaves cost unavailable — never estimated locally.
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
			return contract.ChatResponse{}, acc.ReceivedData(), 0, fmt.Errorf("provider stream stalled or request timed out: %w", ctx.Err())
		default:
		}
	}
	if err := scanner.Err(); err != nil {
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
	return contract.ChatResponse{Content: visible, Reasoning: reasoning, ReasoningDetails: result.ReasoningDetails, ToolCalls: result.ToolCalls, Usage: usage}, acc.ReceivedData(), 0, nil
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
		if profile.ReasoningReplay == ReasoningReplayPreserve && result[index].Role == contract.RoleAssistant {
			if len(result[index].ReasoningDetails) == 0 && result[index].ReasoningContent != nil && strings.TrimSpace(*result[index].ReasoningContent) != "" {
				result[index].ReasoningDetails = reasoningDetailsFromText(*result[index].ReasoningContent)
			}
			// MiniMax consumes reasoning_details, never DeepSeek's reasoning_content.
			result[index].ReasoningContent = nil
			continue
		}
		// Today's default and DeepSeek policy strip captured reasoning. DeepSeek
		// thinking-mode tool-call turns retain its required empty key.
		result[index].ReasoningContent = nil
		result[index].ReasoningDetails = nil
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

func reasoningDetailsFromText(text string) json.RawMessage {
	details, _ := json.Marshal([]map[string]string{{"type": "text", "text": text}})
	return details
}

// StableRequestMessages exposes the provider's final replay representation so
// the orchestrator hashes the same bytes the gateway serializes.
func (p *OpenAICompatible) StableRequestMessages(input contract.ChatRequest) ([]contract.Message, error) {
	cfg := p.snapshot()
	model, err := resolveModel(cfg, input.ModelID)
	if err != nil {
		return nil, err
	}
	return replayMessages(input.Messages, ResolveModelProfile(model.ID+" "+model.Name), input.Reasoning), nil
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

func (p *OpenAICompatible) ListModels(ctx context.Context) ([]contract.Model, error) {
	cfg := p.snapshot()
	if strings.TrimSpace(cfg.Settings.Provider.BaseURL) == "" || strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("provider base URL and API key are required")
	}
	base := strings.TrimSuffix(strings.TrimRight(cfg.Settings.Provider.BaseURL, "/"), "/chat/completions")
	candidates := []string{}
	if strings.HasSuffix(base, "/v1") {
		candidates = append(candidates, base+"/models", strings.TrimSuffix(base, "/v1")+"/models")
	} else {
		candidates = append(candidates, base+"/v1/models", base+"/models")
	}
	var messages []string
	for _, endpoint := range unique(candidates) {
		models, err := fetchModels(ctx, cfg, endpoint)
		if err == nil {
			return models, nil
		}
		messages = append(messages, err.Error())
	}
	return nil, fmt.Errorf("model discovery failed: %s", strings.Join(messages, "; "))
}

// UpdateConfig applies credentials and model settings for future requests. A
// request already in flight keeps its original immutable configuration.
func (p *OpenAICompatible) UpdateConfig(settings contract.Settings, apiKey string) {
	p.mu.Lock()
	p.config.Settings = settings
	p.config.APIKey = apiKey
	p.mu.Unlock()
}

func fetchModels(parent context.Context, cfg Config, endpoint string) ([]contract.Model, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("X-Client-App", "MuhiyaCode")
	// Note: fetchModels has no ChatRequest. The session pin is meaningful only
	// for chat calls (which carry the cached prefix); model discovery never
	// participates in upstream cache routing.
	response, err := cfg.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return nil, fmt.Errorf("%s returned %d: %s", endpoint, response.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return normalizeModels(payload), nil
}

func normalizeModels(payload any) []contract.Model {
	container, _ := payload.(map[string]any)
	data, _ := container["data"].([]any)
	if data == nil {
		data, _ = payload.([]any)
	}
	var result []contract.Model
	for _, raw := range data {
		item, _ := raw.(map[string]any)
		id := stringValue(item["id"])
		if id == "" {
			id = stringValue(item["name"])
		}
		if id == "" {
			continue
		}
		name := stringValue(item["display_name"])
		if name == "" {
			name = stringValue(item["name"])
		}
		if name == "" {
			name = id
		}
		info, _ := item["info"].(map[string]any)
		limit, _ := numberValue(item["context_window"])
		if limit == 0 && info != nil {
			limit, _ = numberValue(info["context_window"])
		}
		maxOutput, _ := numberValue(item["max_output_tokens"])
		if maxOutput == 0 {
			maxOutput, _ = numberValue(item["max_tokens"])
		}
		if maxOutput == 0 && info != nil {
			maxOutput, _ = numberValue(info["max_output_tokens"])
		}
		provider := stringValue(item["owned_by"])
		if provider == "" && info != nil {
			provider = stringValue(info["provider"])
		}
		result = append(result, contract.Model{ID: id, Name: name, Description: stringValue(item["description"]), ContextLimit: limit, MaxOutput: maxOutput, Provider: provider, Source: "endpoint"})
	}
	return result
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
		if model.ID == id {
			if model.ContextLimit <= 0 {
				return contract.Model{}, errors.New("model context limit is not configured")
			}
			return model, nil
		}
	}
	return contract.Model{}, fmt.Errorf("unknown model id %q", id)
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

func isNetworkError(err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}
