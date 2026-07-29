package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func (p *OpenAICompatible) ListModels(ctx context.Context) ([]contract.Model, error) {
	cfg := p.snapshot()
	p.mu.RLock()
	catalogETag := p.catalogETag
	cachedCatalog := append([]contract.Model(nil), p.catalogModels...)
	p.mu.RUnlock()
	if strings.TrimSpace(cfg.Settings.Provider.BaseURL) == "" || strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("provider base URL and API key are required")
	}
	base := strings.TrimSuffix(strings.TrimRight(cfg.Settings.Provider.BaseURL, "/"), "/chat/completions")
	candidates := []string{}
	if strings.HasSuffix(base, "/v1") {
		candidates = append(candidates,
			base+"/muhiyacode/models",
			base+"/models",
			strings.TrimSuffix(base, "/v1")+"/models",
		)
	} else {
		candidates = append(candidates,
			base+"/v1/muhiyacode/models",
			base+"/v1/models",
			base+"/models",
		)
	}
	var messages []string
	for _, endpoint := range unique(candidates) {
		etag := ""
		if strings.Contains(endpoint, "/muhiyacode/models") {
			etag = catalogETag
		}
		models, responseETag, notModified, err := fetchModels(ctx, cfg, endpoint, etag)
		if err == nil && notModified && len(cachedCatalog) > 0 {
			return cachedCatalog, nil
		}
		if err == nil {
			if strings.Contains(endpoint, "/muhiyacode/models") {
				p.mu.Lock()
				p.catalogETag = responseETag
				p.catalogModels = append([]contract.Model(nil), models...)
				p.mu.Unlock()
			}
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

func fetchModels(parent context.Context, cfg Config, endpoint, etag string) ([]contract.Model, string, bool, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("X-Client-App", "MuhiyaCode")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	// Note: fetchModels has no ChatRequest. The session pin is meaningful only
	// for chat calls (which carry the cached prefix); model discovery never
	// participates in upstream cache routing.
	response, err := cfg.Client.Do(req)
	if err != nil {
		return nil, "", false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return nil, response.Header.Get("ETag"), true, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return nil, "", false, fmt.Errorf("%s returned %d: %s", endpoint, response.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, "", false, err
	}
	models := normalizeModels(payload)
	if container, ok := payload.(map[string]any); ok {
		if schema, _ := numberValue(container["schema_version"]); schema == 2 {
			records, recordsOK := container["models"].([]any)
			if !recordsOK || len(records) == 0 || len(models) != len(records) {
				return nil, "", false, fmt.Errorf("%s returned an invalid catalog v2 document", endpoint)
			}
		}
	}
	return models, response.Header.Get("ETag"), false, nil
}

func normalizeModels(payload any) []contract.Model {
	container, _ := payload.(map[string]any)
	if schema, _ := numberValue(container["schema_version"]); schema == 2 {
		if records, ok := container["models"].([]any); ok {
			return normalizeMuhiyaCatalog(records)
		}
	}
	data, _ := container["data"].([]any)
	if data == nil {
		data, _ = payload.([]any)
	}
	var result []contract.Model
	for _, raw := range data {
		item, _ := raw.(map[string]any)
		id := stringValue(item["virtual_name"])
		if id == "" {
			id = stringValue(item["alias"])
		}
		if id == "" {
			id = stringValue(item["id"])
		}
		if id == "" {
			id = stringValue(item["name"])
		}
		if id == "" {
			continue
		}
		name := stringValue(item["display_name"])
		if name == "" {
			name = stringValue(item["virtual_name"])
		}
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
		codingTier, _ := numberValue(item["coding_tier"])
		if codingTier == 0 && info != nil {
			codingTier, _ = numberValue(info["coding_tier"])
		}
		health := stringValue(item["health"])
		if health == "" && info != nil {
			health = stringValue(info["health"])
		}
		result = append(result, contract.Model{
			ID: id, Name: name, Description: stringValue(item["description"]),
			ContextLimit: limit, MaxOutput: maxOutput, CodingTier: codingTier,
			InputCostPerMillion:  modelFloat(item, info, "input_cost_per_million"),
			OutputCostPerMillion: modelFloat(item, info, "output_cost_per_million"),
			Health:               health, Provider: provider, Source: "endpoint",
		})
	}
	return result
}

func normalizeMuhiyaCatalog(records []any) []contract.Model {
	result := make([]contract.Model, 0, len(records))
	for _, raw := range records {
		item, _ := raw.(map[string]any)
		id := stringValue(item["model_id"])
		recordID := stringValue(item["record_id"])
		target := stringValue(item["target_model"])
		if id == "" || recordID == "" || target == "" {
			continue
		}
		contextLimit, _ := numberValue(item["context_window"])
		maxOutput, _ := numberValue(item["max_output_tokens"])
		epoch, _ := numberValue(item["compatibility_epoch"])
		pricingMap, _ := item["pricing"].(map[string]any)
		capabilityMap, _ := item["capabilities"].(map[string]any)

		var cacheContract json.RawMessage
		if encoded, err := json.Marshal(item["cache_contract"]); err == nil {
			cacheContract = encoded
		}
		var tiers []contract.PricingTier
		if encoded, err := json.Marshal(pricingMap["tiers"]); err == nil {
			_ = json.Unmarshal(encoded, &tiers)
		}
		var capabilities contract.ModelCapabilities
		if encoded, err := json.Marshal(capabilityMap); err == nil {
			_ = json.Unmarshal(encoded, &capabilities)
		}

		inputNano, _ := int64Value(pricingMap["input_nano_usd_per_million"])
		outputNano, _ := int64Value(pricingMap["output_nano_usd_per_million"])
		cacheReadNano, _ := int64Value(pricingMap["cache_read_nano_usd_per_million"])
		cacheWriteNano, _ := int64Value(pricingMap["cache_write_nano_usd_per_million"])
		result = append(result, contract.Model{
			ID:                       id,
			Name:                     firstNonBlank(stringValue(item["display_name"]), id),
			RecordID:                 recordID,
			TargetModel:              target,
			Tags:                     stringSlice(item["tags"]),
			ProviderFamily:           stringValue(item["provider_family"]),
			AdapterVersion:           stringValue(item["adapter_version"]),
			CompatibilityEpoch:       epoch,
			CacheContract:            cacheContract,
			SupportedParameters:      stringSlice(item["supported_parameters"]),
			PricingRuleSetID:         stringValue(pricingMap["rule_set_id"]),
			InputNanoPerMillion:      inputNano,
			OutputNanoPerMillion:     outputNano,
			CacheReadNanoPerMillion:  cacheReadNano,
			CacheWriteNanoPerMillion: cacheWriteNano,
			PricingTiers:             tiers,
			Capabilities:             capabilities,
			ContextLimit:             contextLimit,
			MaxOutput:                maxOutput,
			InputCostPerMillion:      float64(inputNano) / 1_000_000_000,
			OutputCostPerMillion:     float64(outputNano) / 1_000_000_000,
			Health:                   stringValue(item["health"]),
			Provider:                 stringValue(item["provider_family"]),
			Source:                   "endpoint",
			Description:              stringValue(item["description"]),
		})
	}
	return result
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringSlice(value any) []string {
	raw, _ := value.([]any)
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text := stringValue(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func int64Value(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
