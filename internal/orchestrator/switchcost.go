package orchestrator

import (
	"github.com/muhiya/muhiyacode/internal/contract"
)

// The economics of changing models mid-session.
//
// Provider prefix caches are per-model (DeepSeek states this explicitly, and it
// is the safe assumption for every provider we route to). So the moment a task
// moves to a model this session has not used, that model sees the entire
// conversation for the first time and bills ALL of it as uncached input. On a
// long session that single decision can cost more than the whole task it was
// meant to improve.
//
// Two facts make the decision tractable, and both are already recorded:
//   - how big the conversation is right now (provider-reported prompt tokens),
//   - which models this session has already warmed (the usage ledger's Model
//     field, one row per request).
//
// So the rule is: switching is free-ish while the conversation is small, and
// cheap at any size when returning to a model already warm in this session.
// Otherwise the cold start has to be paid, and it is capped.

// coldStartCapTokens bounds what a switch may re-send uncached to a model this
// session has never used. Below the cap the cold start is a rounding error
// against the task; above it, the switch has to justify itself by going to a
// model that is already warm.
//
// 25k is deliberately generous for early-session upgrades (a fresh session's
// first tasks sit far below it) and deliberately hard against the case that
// actually burns money: a 200k-token conversation being re-sent in full because
// one task looked slightly harder.
const coldStartCapTokens = 25_000

// switchEconomics is what the harness knows about a candidate switch, and what
// the advisor is told so its judgment is informed rather than blind.
type switchEconomics struct {
	// ColdStartTokens is the conversation size the new model would re-read.
	ColdStartTokens int
	// Warm reports that this session already has usage on the candidate, so its
	// provider cache may still hold the prefix and the re-read may be cheap.
	Warm bool
	// Affordable is the harness verdict: warm, or cold but small enough.
	Affordable bool
}

// noteModelWarm records that a model has now seen the conversation at a given
// history revision. Called once per main-loop request, under taskMu.
//
// Only MAIN-stream requests may call this, and that restriction is the whole
// point. Aux calls — the advisor itself, compaction summaries — run on a cheap
// model with a tiny one-shot prompt that has nothing to do with the session's
// prefix. Counting those would mark the utility model "warm" and hand the
// advisor exactly the wrong recommendation: switch to the one model guaranteed
// to cold-start.
func (e *Engine) noteModelWarm(modelID string, rewriteVersion int) {
	if modelID == "" {
		return
	}
	if e.warmPrefix == nil {
		e.warmPrefix = make(map[string]int, 2)
	}
	e.warmPrefix[modelID] = rewriteVersion
}

// modelWarmThisSession reports whether this model has already been sent the
// conversation AS IT STANDS NOW, so the provider may still hold its prefix.
//
// The revision comparison is what keeps this honest. Compaction, a pressure
// trim, and a completed-task fold all rewrite earlier messages in place; after
// any of them, every model's cached prefix describes a conversation that no
// longer exists. Warmth has to retire with it, or the advisor will price a full
// cold start as free.
//
// A resumed session starts with an empty ledger and therefore treats every
// model as cold. That is the conservative direction: it makes switching look
// expensive and keeps the session where it is, which is what a resume wants.
func (e *Engine) modelWarmThisSession(modelID string) bool {
	if modelID == "" {
		return false
	}
	current := e.history.RewriteVersion()
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	version, seen := e.warmPrefix[modelID]
	return seen && version == current
}

// switchCost measures what moving this task to candidate would cost in
// uncached input, and whether that is affordable.
func (e *Engine) switchCost(candidate string) switchEconomics {
	cost := switchEconomics{ColdStartTokens: e.inUseContextTokens(), Warm: e.modelWarmThisSession(candidate)}
	cost.Affordable = cost.Warm || cost.ColdStartTokens <= coldStartCapTokens
	return cost
}

// inUseContextTokens is the conversation size a model switch would re-read. It
// prefers the provider's reported prompt size — the only honest number — and
// falls back to the local estimate before the first response lands.
func (e *Engine) inUseContextTokens() int {
	estimate := e.history.EstimatedTokens()
	if e.latestPromptAvailable && e.latestPromptTokens > estimate {
		return e.latestPromptTokens
	}
	return estimate
}

// historyFitsModel reports whether the conversation carried into this task fits
// the candidate model's window with room to answer. Distinct from switchCost:
// this one is about whether the switch is POSSIBLE without silently dropping
// the oldest messages at assembly time; switchCost is about whether it is worth
// paying for.
func (e *Engine) historyFitsModel(modelID string) bool {
	limit := 0
	for _, model := range e.settings.Provider.Models {
		if model.ID == modelID {
			limit = model.ContextLimit
			break
		}
	}
	profile := e.catalogModelProfile(modelID)
	if limit <= 0 {
		limit = profile.DefaultContextWindow
	}
	if limit <= 0 {
		return false // an unknown window is not one to gamble a conversation on
	}
	// 30% headroom for this task's growth, plus tool schemas and the output the
	// candidate must be able to produce. Keep this equation aligned with request
	// preflight: subtracting a second fixed reserve here used to double-charge
	// output room and reject models that actually fit.
	e.taskMu.Lock()
	toolChars := e.assemblyToolDefChars
	e.taskMu.Unlock()
	needed := e.inUseContextTokens()*13/10 + e.history.TokensForChars(toolChars) + profile.ContextOutputReserve(e.catalogModelMaxOutput(modelID)) + 512
	return needed <= limit
}

// routedWindowFloorTokens is where a conversation stops being safe to route
// through a load-balancing layer.
//
// A model slug on a routing layer is not one machine with one window. The nine
// upstreams serving MiniMax M3 range from 256k to 1M context, while our catalog
// carries the single largest number — so a conversation the catalog says fits
// comfortably will 400 outright the moment it lands on a smaller peer. The
// upstream pin makes that rare, not impossible: it is a preference, and a
// fallback during an outage can put a 600k conversation on a 524k machine.
//
// 500k is chosen against the real floor of the endpoints that matter (524,288),
// leaving headroom for the answer. Below it every listed upstream can serve us;
// above it the conversation's survival depends on which peer answers.
const routedWindowFloorTokens = 500_000

// routedWindowRisk reports whether the conversation has outgrown what every
// peer behind the routing layer can hold. It is deliberately not a gate: the
// request may well succeed, and refusing work over a risk the user can resolve
// in one command would be worse than telling them about it.
func (e *Engine) routedWindowRisk() bool {
	if e.upstreamPin() == "" {
		return false // no routing layer in the path; the catalog window is the truth
	}
	return e.inUseContextTokens() > routedWindowFloorTokens
}

// warmModelsThisSession lists the models holding a cache of the conversation as
// it stands, most recently used first. The advisor is shown this list so it can
// prefer returning to a warm model over lighting up a cold one, and the
// /context card shows it for the same reason.
func (e *Engine) warmModelsThisSession() []string {
	current := e.history.RewriteVersion()
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	seen := map[string]bool{}
	var models []string
	for i := len(e.usageRecords) - 1; i >= 0; i-- {
		record := e.usageRecords[i]
		if record.Stream != "" && record.Stream != contract.UsageStreamMain {
			continue // aux traffic never warms the session's prefix
		}
		if record.Model == "" || seen[record.Model] {
			continue
		}
		seen[record.Model] = true
		if version, ok := e.warmPrefix[record.Model]; ok && version == current {
			models = append(models, record.Model)
		}
	}
	return models
}
