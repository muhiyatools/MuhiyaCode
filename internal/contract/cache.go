package contract

import "time"

// CacheAttribution explains why a request's cache-eligible prefix missed.
type CacheAttribution string

const (
	CacheAttributionNA           CacheAttribution = "n/a"
	CacheAttributionColdStart    CacheAttribution = "cold-start"
	CacheAttributionAgent        CacheAttribution = "agent"
	CacheAttributionAgentSuspect CacheAttribution = "agent-suspect"
	CacheAttributionProvider     CacheAttribution = "provider"
)

type UsageStream string

const (
	UsageStreamMain     UsageStream = "main"
	UsageStreamAux      UsageStream = "aux"
	UsageStreamSubagent UsageStream = "subagent"
)

// UsageRecord is the append-only, provider-faithful accounting record for one
// request. Pointer-valued token fields distinguish an unavailable value from a
// provider-reported zero.
type UsageRecord struct {
	Seq    int         `json:"seq"`
	At     time.Time   `json:"at"`
	Model  string      `json:"model"`
	Stream UsageStream `json:"stream,omitempty"`
	// Pin is the provider cache identity ROLE this request rode (":main",
	// ":sub:<kind>", ":sub:onboarding", ":aux") — feature 011 D8. Per-(model,
	// pin) aggregation is what makes mixed-model cache health measurable
	// (SC-005). Empty on records persisted before feature 011.
	Pin                string           `json:"pin,omitempty"`
	PromptTokens       *int             `json:"prompt_tokens"`
	CompletionTokens   *int             `json:"completion_tokens"`
	CacheReadTokens    *int             `json:"cache_read_tokens"`
	CacheMissTokens    *int             `json:"cache_miss_tokens"`
	NewTailTokens      *int             `json:"new_tail_tokens,omitempty"`
	MissDerived        bool             `json:"miss_derived"`
	HitRate            *float64         `json:"hit_rate"`
	PrefixChanged      bool             `json:"prefix_changed"`
	ChangeReasons      []string         `json:"change_reasons"`
	Attribution        CacheAttribution `json:"attribution"`
	UsageContradictory bool             `json:"usage_contradictory,omitempty"`
	Diagnostic         string           `json:"diagnostic,omitempty"`
	// CostUSD is the gateway-reported request cost (USD) from the muhiya_log
	// chunk. nil = not reported. Persisted so per-task and session credits can be
	// scanned from the append-only log and survive resume. LogID cross-checks
	// against the gateway's request_logs ledger (SC-004).
	CostUSD *float64 `json:"cost_usd,omitempty"`
	// CostEstimated mirrors the gateway usage_estimated flag for this request.
	CostEstimated bool `json:"cost_estimated,omitempty"`
	// LogID is the gateway request_logs.id from muhiya_log.log_id, for the
	// benchmark credits cross-check. Empty when the chunk was absent.
	LogID string `json:"log_id,omitempty"`
	// DurationMS is the wall-clock time of the provider request (start of the call
	// to end of the stream), for the session usage panel's API-time figure
	// (feature 008 UD-6). nil = unknown (older persisted records, pre-send
	// failures). Nullable JSON keeps usage.jsonl backward/forward compatible.
	DurationMS *int64 `json:"duration_ms,omitempty"`
}

// PairingRate is the per-(model, pin) cache aggregate (feature 011 D8):
// steady-state hit rate excludes each pairing's first cache-reporting request
// (the cold write), so a mixed-model session's per-provider cache health is
// comparable against single-model baselines (SC-005). Reported=false means the
// provider sent no cache fields for this pairing — shown as "not reported",
// never fabricated (Constitution VI).
type PairingRate struct {
	Model              string   `json:"model"`
	Pin                string   `json:"pin"`
	Requests           int      `json:"requests"`
	CacheReadTokens    int      `json:"cacheReadTokens"`
	CacheMissTokens    int      `json:"cacheMissTokens"`
	SteadyStateHitRate *float64 `json:"steadyStateHitRate,omitempty"`
	Reported           bool     `json:"reported"`
	// PromptTokens/CompletionTokens (feature 012 R-D13) sum the pairing's
	// provider-reported spend — exact per-pin attribution for the benchmark
	// (retires the feature-011 whole-task review-spend approximation).
	PromptTokens     int `json:"promptTokens,omitempty"`
	CompletionTokens int `json:"completionTokens,omitempty"`
}

// PerPairingRates aggregates usage records per (model, pin), preserving first-
// appearance order for stable display. Records without a pin (pre-011) group
// under their stream name as a best-effort label.
func PerPairingRates(records []UsageRecord) []PairingRate {
	type bucket struct {
		rate     PairingRate
		sawFirst bool // first cache-reporting record (cold write) already skipped
	}
	var order []string
	buckets := map[string]*bucket{}
	for _, record := range records {
		pin := record.Pin
		if pin == "" {
			pin = string(record.Stream)
		}
		key := record.Model + "|" + pin
		b, ok := buckets[key]
		if !ok {
			b = &bucket{rate: PairingRate{Model: record.Model, Pin: pin}}
			buckets[key] = b
			order = append(order, key)
		}
		b.rate.Requests++
		if record.PromptTokens != nil {
			b.rate.PromptTokens += *record.PromptTokens
		}
		if record.CompletionTokens != nil {
			b.rate.CompletionTokens += *record.CompletionTokens
		}
		if record.CacheReadTokens == nil || record.CacheMissTokens == nil {
			continue
		}
		b.rate.Reported = true
		if !b.sawFirst {
			b.sawFirst = true // cold write: excluded from the steady-state rate
			continue
		}
		b.rate.CacheReadTokens += *record.CacheReadTokens
		b.rate.CacheMissTokens += *record.CacheMissTokens
	}
	result := make([]PairingRate, 0, len(order))
	for _, key := range order {
		b := buckets[key]
		read, miss := b.rate.CacheReadTokens, b.rate.CacheMissTokens
		b.rate.SteadyStateHitRate = HitRate(&read, &miss)
		result = append(result, b.rate)
	}
	return result
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

// CreditsResult reports a scanned credits sum over a set of usage records under
// the 003 member-set rules (data-model §1.1): records that carry no provider
// usage at all are excluded (failed/timed-out calls, never guessed); among the
// remaining priced-eligible members, any nil CostUSD makes the whole sum
// unavailable (partial money totals are dishonest). Estimated ORs across members.
type CreditsResult struct {
	// USD is nil when unavailable (no eligible members, or an eligible member had
	// no CostUSD); otherwise the summed USD cost.
	USD *float64
	// Estimated is true when any summed member was cost-estimated.
	Estimated bool
	// Priced / Eligible count members for the honesty line in /context.
	Priced   int
	Eligible int
}

// hasProviderUsage reports whether a record carries any provider-reported usage.
// Records with none (failed/empty aux calls) are excluded from credits.
func (r UsageRecord) hasProviderUsage() bool {
	return r.PromptTokens != nil || r.CompletionTokens != nil ||
		r.CacheReadTokens != nil || r.CacheMissTokens != nil
}

// SumCreditsUSD scans records under the member-set rules. It never estimates a
// missing cost; an eligible member without CostUSD collapses the sum to nil.
func SumCreditsUSD(records []UsageRecord) CreditsResult {
	var result CreditsResult
	var sum float64
	haveNil := false
	for _, record := range records {
		if !record.hasProviderUsage() {
			continue
		}
		result.Eligible++
		if record.CostUSD == nil {
			haveNil = true
			continue
		}
		result.Priced++
		sum += *record.CostUSD
		if record.CostEstimated {
			result.Estimated = true
		}
	}
	if result.Eligible == 0 || haveNil {
		return result
	}
	result.USD = &sum
	return result
}

// ModelUsageRow is one model's aggregated usage across all streams of a session
// (feature 008 UD-7): the per-model breakdown behind the usage panel. Cost follows
// the same member-set honesty rules as SumCreditsUSD, applied per model: a
// priced-eligible record without a cost collapses that MODEL's cost to nil
// (unavailable), never a partial sum.
type ModelUsageRow struct {
	Model          string
	Requests       int
	UncachedIn     int  // Σ cache-miss tokens (provider-reported); the billed input
	Output         int  // Σ completion tokens
	CacheRead      int  // Σ cache-read tokens
	CacheAvailable bool // true only when every provider-usage member reports read + miss
	CostUSD        *float64
	Estimated      int // count of cost-estimated requests in this row
}

// AggregateUsageByModel groups records by their recorded model ID, in first-seen
// order (deterministic for a given record log). Records without provider usage are
// skipped entirely (same exclusion as credits). The caller renders a Total row by
// summing; costs sum only when every row has one.
func AggregateUsageByModel(records []UsageRecord) []ModelUsageRow {
	index := map[string]int{}
	var rows []ModelUsageRow
	costNil := map[string]bool{}
	cacheUnavailable := map[string]bool{}
	for _, record := range records {
		if !record.hasProviderUsage() {
			continue
		}
		i, ok := index[record.Model]
		if !ok {
			i = len(rows)
			index[record.Model] = i
			rows = append(rows, ModelUsageRow{Model: record.Model})
		}
		row := &rows[i]
		row.Requests++
		if record.CacheMissTokens != nil {
			row.UncachedIn += *record.CacheMissTokens
		}
		if record.CacheReadTokens == nil || record.CacheMissTokens == nil {
			cacheUnavailable[record.Model] = true
		}
		if record.CompletionTokens != nil {
			row.Output += *record.CompletionTokens
		}
		if record.CacheReadTokens != nil {
			row.CacheRead += *record.CacheReadTokens
		}
		if record.CostEstimated {
			row.Estimated++
		}
		switch {
		case record.CostUSD == nil:
			costNil[record.Model] = true
		case !costNil[record.Model]:
			if row.CostUSD == nil {
				row.CostUSD = new(float64)
			}
			*row.CostUSD += *record.CostUSD
		}
	}
	for i := range rows {
		if costNil[rows[i].Model] {
			rows[i].CostUSD = nil
		}
		rows[i].CacheAvailable = !cacheUnavailable[rows[i].Model]
	}
	return rows
}

// SessionUsageAggregate is derived from UsageRecords on every session load.
// It is never persisted independently, so it cannot drift from the audit log.
type SessionUsageAggregate struct {
	Requests         int `json:"requests"`
	MainRequests     int `json:"main_requests"`
	AuxRequests      int `json:"aux_requests"`
	SubagentRequests int `json:"subagent_requests"`
	SumPrompt        int `json:"sum_prompt"`
	SumCompletion    int `json:"sum_completion"`
	SumCacheRead     int `json:"sum_cache_read"`
	SumCacheMiss     int `json:"sum_cache_miss"`
	// PairedCacheRead/PairedCacheMiss sum only records where BOTH operands were
	// reported (any stream). Every rate — including the per-task delta the TUI
	// summary divides — must consume these, never the one-sided display sums
	// above: a record that reported a read with an unknowable miss would
	// otherwise fabricate part of a hit-rate denominator.
	PairedCacheRead      int      `json:"paired_cache_read"`
	PairedCacheMiss      int      `json:"paired_cache_miss"`
	SessionHitRate       *float64 `json:"session_hit_rate"`
	SteadyStateHitRate   *float64 `json:"steady_state_hit_rate"`
	PrefixStabilityRate  *float64 `json:"prefix_stability_rate"`
	UnavailableRequests  int      `json:"unavailable_requests"`
	PromptAvailable      int      `json:"prompt_available_requests"`
	CompletionAvailable  int      `json:"completion_available_requests"`
	CacheAvailable       int      `json:"cache_available_requests"`
	SteadyStateCacheRead int      `json:"steady_state_cache_read"`
	SteadyStateCacheMiss int      `json:"steady_state_cache_miss"`
	PrefixStableRead     int      `json:"prefix_stable_read"`
	PrefixStableEligible int      `json:"prefix_stable_eligible"`
}

// AggregateUsage folds the persisted log using only available values.
func AggregateUsage(records []UsageRecord) SessionUsageAggregate {
	var aggregate SessionUsageAggregate
	var sessionRateRead, sessionRateMiss int
	var sessionRateAvailable bool
	var steadyStateRateAvailable bool
	var prefixStabilityAvailable bool
	for _, record := range records {
		aggregate.Requests++
		switch record.Stream {
		case UsageStreamAux:
			aggregate.AuxRequests++
		case UsageStreamSubagent:
			aggregate.SubagentRequests++
		default:
			aggregate.MainRequests++
		}
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
		aggregate.PairedCacheRead += *record.CacheReadTokens
		aggregate.PairedCacheMiss += *record.CacheMissTokens
		// Empty stream is legacy main-session data. Auxiliary records and n/a
		// records remain in reconciling sums but never enter cache-rate KPIs.
		mainStream := record.Stream == "" || record.Stream == UsageStreamMain
		if mainStream && record.Attribution != CacheAttributionNA {
			sessionRateAvailable = true
			sessionRateRead += *record.CacheReadTokens
			sessionRateMiss += *record.CacheMissTokens
		}
		if mainStream && record.Attribution != CacheAttributionNA && record.Attribution != CacheAttributionColdStart {
			steadyStateRateAvailable = true
			aggregate.SteadyStateCacheRead += *record.CacheReadTokens
			aggregate.SteadyStateCacheMiss += *record.CacheMissTokens
			if record.PromptTokens != nil && record.NewTailTokens != nil {
				eligible := max(0, *record.PromptTokens-*record.NewTailTokens)
				aggregate.PrefixStableRead += *record.CacheReadTokens
				aggregate.PrefixStableEligible += eligible
				prefixStabilityAvailable = prefixStabilityAvailable || eligible > 0
			}
		}
	}
	aggregate.SessionHitRate = hitRateValues(sessionRateRead, sessionRateMiss, sessionRateAvailable)
	aggregate.SteadyStateHitRate = hitRateValues(aggregate.SteadyStateCacheRead, aggregate.SteadyStateCacheMiss, steadyStateRateAvailable)
	aggregate.PrefixStabilityRate = ratioValues(aggregate.PrefixStableRead, aggregate.PrefixStableEligible, prefixStabilityAvailable)
	return aggregate
}

func ratioValues(numerator, denominator int, available bool) *float64 {
	if !available || denominator <= 0 {
		return nil
	}
	value := float64(numerator) / float64(denominator)
	return &value
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
)

// PrefixShapeSnapshot is the per-session prefix_shape.json sidecar (Ultimate Polish
// C3): the session-stable prefix components (system message, tool set, model) hashed
// after the first request, restored on resume so the first request of the resumed
// session can attribute a skills/tools/model change instead of cold-starting
// silently. Only session-stable regions are stored — history is not (it legitimately
// grows and its replay determinism is guarded separately).
type PrefixShapeSnapshot struct {
	Version    int    `json:"version"`
	SystemHash string `json:"systemHash"`
	ToolsHash  string `json:"toolsHash"`
	ModelID    string `json:"modelId"`
}

// PrefixShapeSnapshotVersion is the current prefix_shape.json schema version.
const PrefixShapeSnapshotVersion = 1

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

// HarnessEventClass groups harness-CAUSED friction events for telemetry (Stability
// Overhaul T010) — a gate rejection, a tool failure, a provider error/retry, a
// bounded recovery/breaker, or a UI-layer anomaly. This is the harness's own
// self-report of where it made the user's work harder, so those events stop being
// discovered only via screenshots.
type HarnessEventClass string

const (
	HarnessGate     HarnessEventClass = "gate"     // a harness gate rejected a call
	HarnessTool     HarnessEventClass = "tool"     // a tool call failed
	HarnessProvider HarnessEventClass = "provider" // provider/network error or retry
	HarnessRecovery HarnessEventClass = "recovery" // a bounded fallback / breaker fired
	HarnessUI       HarnessEventClass = "ui"       // a UI-layer anomaly (e.g. recovered panic)
)

// HarnessEvent is one recorded friction event. Detail is already redacted and
// length-bounded by the recorder.
type HarnessEvent struct {
	At     time.Time         `json:"at"`
	Class  HarnessEventClass `json:"class"`
	Code   string            `json:"code"`
	Detail string            `json:"detail"`
}
