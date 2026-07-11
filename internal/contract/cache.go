package contract

import "time"

// CacheAttribution explains why a request's cache-eligible prefix missed.
type CacheAttribution string

const (
	CacheAttributionNA        CacheAttribution = "n/a"
	CacheAttributionColdStart CacheAttribution = "cold-start"
	CacheAttributionAgent     CacheAttribution = "agent"
	CacheAttributionProvider  CacheAttribution = "provider"
)

// UsageRecord is the append-only, provider-faithful accounting record for one
// request. Pointer-valued token fields distinguish an unavailable value from a
// provider-reported zero.
type UsageRecord struct {
	Seq                int              `json:"seq"`
	At                 time.Time        `json:"at"`
	Model              string           `json:"model"`
	PromptTokens       *int             `json:"prompt_tokens"`
	CompletionTokens   *int             `json:"completion_tokens"`
	CacheReadTokens    *int             `json:"cache_read_tokens"`
	CacheMissTokens    *int             `json:"cache_miss_tokens"`
	MissDerived        bool             `json:"miss_derived"`
	HitRate            *float64         `json:"hit_rate"`
	PrefixChanged      bool             `json:"prefix_changed"`
	ChangeReasons      []string         `json:"change_reasons"`
	Attribution        CacheAttribution `json:"attribution"`
	UsageContradictory bool             `json:"usage_contradictory,omitempty"`
	Diagnostic         string           `json:"diagnostic,omitempty"`
}

// HitRate returns a provider-derived rate without clamping or smoothing.
// A missing operand or zero denominator is unavailable rather than zero/NaN.
func HitRate(read, miss *int) *float64 {
	if read == nil || miss == nil {
		return nil
	}
	denominator := *read + *miss
	if denominator == 0 {
		return nil
	}
	value := float64(*read) / float64(denominator)
	return &value
}

// SessionUsageAggregate is derived from UsageRecords on every session load.
// It is never persisted independently, so it cannot drift from the audit log.
type SessionUsageAggregate struct {
	Requests             int      `json:"requests"`
	SumPrompt            int      `json:"sum_prompt"`
	SumCompletion        int      `json:"sum_completion"`
	SumCacheRead         int      `json:"sum_cache_read"`
	SumCacheMiss         int      `json:"sum_cache_miss"`
	SessionHitRate       *float64 `json:"session_hit_rate"`
	SteadyStateHitRate   *float64 `json:"steady_state_hit_rate"`
	UnavailableRequests  int      `json:"unavailable_requests"`
	PromptAvailable      int      `json:"prompt_available_requests"`
	CompletionAvailable  int      `json:"completion_available_requests"`
	CacheAvailable       int      `json:"cache_available_requests"`
	SteadyStateCacheRead int      `json:"steady_state_cache_read"`
	SteadyStateCacheMiss int      `json:"steady_state_cache_miss"`
}

// AggregateUsage folds the persisted log using only available values.
func AggregateUsage(records []UsageRecord) SessionUsageAggregate {
	var aggregate SessionUsageAggregate
	var sessionRateRead, sessionRateMiss int
	var sessionRateAvailable bool
	var steadyStateRateAvailable bool
	for _, record := range records {
		aggregate.Requests++
		if record.PromptTokens != nil {
			aggregate.SumPrompt += *record.PromptTokens
			aggregate.PromptAvailable++
		}
		if record.CompletionTokens != nil {
			aggregate.SumCompletion += *record.CompletionTokens
			aggregate.CompletionAvailable++
		}
		// The displayed sums preserve every independently reported value. Rates,
		// however, may only consume records where both operands were available;
		// otherwise a partial provider payload would fabricate a denominator.
		if record.CacheReadTokens != nil {
			aggregate.SumCacheRead += *record.CacheReadTokens
		}
		if record.CacheMissTokens != nil {
			aggregate.SumCacheMiss += *record.CacheMissTokens
		}
		if record.CacheReadTokens == nil || record.CacheMissTokens == nil {
			aggregate.UnavailableRequests++
			continue
		}
		aggregate.CacheAvailable++
		sessionRateAvailable = true
		sessionRateRead += *record.CacheReadTokens
		sessionRateMiss += *record.CacheMissTokens
		if record.Attribution != CacheAttributionColdStart {
			steadyStateRateAvailable = true
			aggregate.SteadyStateCacheRead += *record.CacheReadTokens
			aggregate.SteadyStateCacheMiss += *record.CacheMissTokens
		}
	}
	aggregate.SessionHitRate = hitRateValues(sessionRateRead, sessionRateMiss, sessionRateAvailable)
	aggregate.SteadyStateHitRate = hitRateValues(aggregate.SteadyStateCacheRead, aggregate.SteadyStateCacheMiss, steadyStateRateAvailable)
	return aggregate
}

func hitRateValues(read, miss int, available bool) *float64 {
	if !available || read+miss == 0 {
		return nil
	}
	value := float64(read) / float64(read+miss)
	return &value
}

type InvalidationCause string

const (
	InvalidationFold          InvalidationCause = "fold"
	InvalidationTrim          InvalidationCause = "trim"
	InvalidationCompact       InvalidationCause = "compact"
	InvalidationWindowDrop    InvalidationCause = "window-drop"
	InvalidationToolsetChange InvalidationCause = "toolset-change"
	InvalidationModelSwitch   InvalidationCause = "model-switch"
	InvalidationPromptRebuild InvalidationCause = "prompt-rebuild"
	InvalidationUserCompact   InvalidationCause = "user-compact"
	InvalidationProbeChange   InvalidationCause = "probe-change"
)

type InvalidationTrigger string

const (
	InvalidationPressure     InvalidationTrigger = "pressure"
	InvalidationUserAction   InvalidationTrigger = "user-action"
	InvalidationConfigChange InvalidationTrigger = "config-change"
	InvalidationBoundary     InvalidationTrigger = "boundary"
)

// InvalidationEvent records one attributable rewrite before the request that
// first transmits the changed bytes.
type InvalidationEvent struct {
	At         time.Time           `json:"at"`
	Cause      InvalidationCause   `json:"cause"`
	Trigger    InvalidationTrigger `json:"trigger"`
	Scope      string              `json:"scope"`
	Pressure   *float64            `json:"pressure"`
	RequestSeq int                 `json:"request_seq"`
}
