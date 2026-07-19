package orchestrator

import (
	"encoding/json"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type ContextReport struct {
	// PerPairing (feature 011 D8/T033): per-(model, pin) cache health rows for
	// the /context readout — the mixed-model visibility SC-005 requires.
	PerPairing []contract.PairingRate

	HistoryTokens      int
	ContextLimit       int
	Percent            float64
	Usage              contract.Usage
	UsageAggregate     contract.SessionUsageAggregate
	Invalidations      []contract.InvalidationEvent
	PressureTokens     int
	PressureEstimated  bool
	PressurePercent    float64
	MaintenanceLatched bool
	// 003 (T032): session credits scanned from all usage records under the
	// member-set rules. SessionCreditsUSD is nil when unavailable; the priced/
	// eligible counts drive the honesty line in the context modal.
	SessionCreditsUSD       *float64
	SessionCreditsEstimated bool
	SessionCreditsPriced    int
	SessionCreditsEligible  int
	// Feature 008 (UD-6/UD-7): the session usage panel's inputs. APITimeMS sums
	// the persisted per-request durations (rebuilds on resume); ActiveMS and the
	// lines± counters are session-scoped accumulators (reset on resume, labeled
	// "this session"); ByModel is the per-model breakdown over all streams.
	APITimeMS    int64
	ActiveMS     int64
	LinesAdded   int
	LinesRemoved int
	ByModel      []contract.ModelUsageRow
	// Categories (UD-10..12) is the estimated usage-by-category split of the
	// current context window; every row is labeled estimated by the display.
	Categories []ContextCategory
}

// ContextCategory is one estimated slice of the context window (feature 008
// UD-10): a display name, its estimated token count, and its percent of the
// window. Sizes come from component byte lengths the engine already holds,
// converted via the history's calibrated tokens-per-char ratio.
type ContextCategory struct {
	Name    string
	Tokens  int
	Percent float64
}

func (e *Engine) ContextReport() ContextReport {
	history := e.history.EstimatedTokens()
	limit := e.contextLimit()
	pressure := e.contextPressure()
	e.taskMu.Lock()
	latched := e.maintenanceLatched
	credits := contract.SumCreditsUSD(e.usageRecords)
	byModel := contract.AggregateUsageByModel(e.usageRecords)
	perPairing := contract.PerPairingRates(e.usageRecords)
	var apiTimeMS int64
	for _, record := range e.usageRecords {
		if record.DurationMS != nil {
			apiTimeMS += *record.DurationMS
		}
	}
	activeMS := e.sessionActiveMS
	linesAdded, linesRemoved := e.sessionLinesAdded, e.sessionLinesRemoved
	e.taskMu.Unlock()
	return ContextReport{
		PerPairing:              perPairing,
		HistoryTokens:           history,
		ContextLimit:            limit,
		Percent:                 float64(history) / float64(max(1, limit)) * 100,
		Usage:                   e.Usage(),
		UsageAggregate:          e.UsageAggregate(),
		Invalidations:           e.InvalidationEvents(),
		PressureTokens:          pressure.Tokens,
		PressureEstimated:       pressure.Estimated,
		PressurePercent:         pressure.Ratio * 100,
		MaintenanceLatched:      latched,
		SessionCreditsUSD:       credits.USD,
		SessionCreditsEstimated: credits.Estimated,
		SessionCreditsPriced:    credits.Priced,
		SessionCreditsEligible:  credits.Eligible,
		APITimeMS:               apiTimeMS,
		ActiveMS:                activeMS,
		LinesAdded:              linesAdded,
		LinesRemoved:            linesRemoved,
		ByModel:                 byModel,
		Categories:              e.contextCategories(history, limit),
	}
}

// recordAssemblySizes stores the byte sizes of the request-assembly components
// (feature 008 T025) so contextCategories can estimate the window split without
// recomputing prompts or re-marshaling tool schemas on the read side. Called once
// per task at assembly; values are session-stable between tool-boundary changes.
func (e *Engine) recordAssemblySizes(promptText string, promptContext PromptContext, definitions []contract.ToolDefinition) {
	toolChars := 0
	if raw, err := json.Marshal(definitions); err == nil {
		toolChars = len(raw)
	}
	projectChars := len(promptContext.ProjectContextBlock) + len(renderSkillsSection(promptContext.Skills))
	e.taskMu.Lock()
	e.assemblyPromptChars = len(promptText)
	e.assemblyToolDefChars = toolChars
	e.assemblyProjectChars = projectChars
	e.taskMu.Unlock()
}

// contextCategories estimates the usage-by-category split of the context window
// (feature 008 UD-10..12): system prompt · tool definitions · project memory &
// skills · conversation · summary · free. Char sizes convert through the
// history's calibrated tokens-per-char ratio, so the split tracks the real
// tokenizer as calibration improves; every figure is an estimate and the display
// labels it so. Before the first task assembles a request, the component sizes
// are unknown and only the history-derived rows render (no fabricated zeros).
func (e *Engine) contextCategories(historyTokens, limit int) []ContextCategory {
	e.taskMu.Lock()
	promptChars, toolChars, projectChars := e.assemblyPromptChars, e.assemblyToolDefChars, e.assemblyProjectChars
	e.taskMu.Unlock()
	summaryTokens := e.history.TokensForChars(len(e.history.CompactSummary()))
	conversation := max(0, historyTokens-summaryTokens)
	var categories []ContextCategory
	add := func(name string, tokens int) {
		if tokens <= 0 {
			return
		}
		categories = append(categories, ContextCategory{Name: name, Tokens: tokens, Percent: float64(tokens) / float64(max(1, limit)) * 100})
	}
	used := conversation + summaryTokens
	if promptChars > 0 {
		system := e.history.TokensForChars(max(0, promptChars-projectChars))
		tools := e.history.TokensForChars(toolChars)
		project := e.history.TokensForChars(projectChars)
		add("System prompt", system)
		add("Tool definitions", tools)
		add("Project memory & skills", project)
		used += system + tools + project
	}
	add("Conversation", conversation)
	add("Summary", summaryTokens)
	add("Free", max(0, limit-used))
	return categories
}

func (e *Engine) emitContext(lastRequest int) {
	history := e.history.EstimatedTokens()
	used := max(history, lastRequest)
	percent := float64(used) / float64(max(1, e.contextLimit())) * 100
	e.taskMu.Lock()
	e.taskPeakContext = max(e.taskPeakContext, percent)
	e.taskMu.Unlock()
	if e.callbacks.Context != nil {
		e.callbacks.Context(contract.ContextInfo{HistoryTokens: history, LastRequestTokens: lastRequest, ContextLimit: e.contextLimit(), Percent: min(100, percent)})
	}
}
