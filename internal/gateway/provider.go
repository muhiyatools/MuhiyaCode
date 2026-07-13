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
	Settings         contract.Settings
	APIKey           string
	Client           *http.Client
	IdleTimeout      time.Duration
	RequestLifetime  time.Duration
	MaxRetries       int
	RawUsageObserver RawUsageObserver
}

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
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 90 * time.Second
	}
	if config.RequestLifetime <= 0 {
		config.RequestLifetime = 10 * time.Minute
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
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		response, streamed, retryAfter, err := p.chatOnce(ctx, cfg, model, profile, input)
		if err == nil {
			return response, nil
		}
		last = err
		var httpErr *HTTPError
		retryable := errors.As(err, &httpErr) && httpErr.Retryable
		if !retryable && !streamed && isNetworkError(err) {
			retryable = true
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
	ctx, cancel := context.WithTimeout(parent, cfg.RequestLifetime)
	defer cancel()
	temperature := profile.Temperature
	if input.Temperature != nil {
		temperature = *input.Temperature
	}
	maxTokens := input.MaxTokens
	if maxTokens <= 0 {
		maxTokens = profile.MaxOutputTokens
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

	idle := time.AfterFunc(cfg.IdleTimeout, cancel)
	defer idle.Stop()
	acc := NewStreamAccumulator(input.OnToken, input.OnReasoningToken)
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
	return contract.ChatResponse{Content: visible, Reasoning: reasoning, ToolCalls: result.ToolCalls, Usage: usage}, acc.ReceivedData(), 0, nil
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
		// Never replay captured reasoning. DeepSeek thinking-mode tool-call
		// turns require the key to exist, so emit only the minimal empty form.
		result[index].ReasoningContent = nil
		if profile.Family == "deepseek" && result[index].Role == contract.RoleAssistant && len(result[index].ToolCalls) > 0 {
			empty := ""
			result[index].ReasoningContent = &empty
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
