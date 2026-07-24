package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// The task advisor. When a prompt arrives, a cheap utility call decides which
// model should run THIS task; that model is then fixed until the task ends.
// Every request inside a task therefore goes to one model, which is what makes
// the provider's implicit prefix cache work at all.
//
// Its expected answer is "keep". A switch is a real cost — the new model has
// never seen this conversation, so the whole prefix is re-billed as uncached
// input on its first request — and the advisor is told that cost in tokens
// before it answers (see switchcost.go). Two hard gates back that up: the
// conversation must FIT the candidate's window, and the cold start must be
// affordable. Keeping a merely-adequate warm model beats a marginally better
// cold one on nearly every task.

// advisorMaxTokens keeps the aux call trivially cheap; the answer is one small
// JSON object.
const advisorMaxTokens = 200

// advisorDecision is the utility model's answer: keep the current model, or
// name the one this task should run on.
type advisorDecision struct {
	Keep  bool   `json:"keep"`
	Model string `json:"model"`
	Why   string `json:"why"`
}

// utilityModelID resolves the cheap model used for auxiliary calls (the task
// advisor, onboarding questions). These are one-shot, low-token, and off the
// session's cached stream, so the cheapest capable model is the right one.
//
// Preference order: a Flash-class model, then the smallest-window catalog entry
// (a proxy for cheapest), then the active model. The last fallback matters: it
// guarantees a usable id even for a single-model catalog, where the old
// fallback (the configured subagent model, a field the unified session no
// longer maintains) could be empty and silently break the aux call.
func (e *Engine) utilityModelID() string {
	for _, model := range e.settings.Provider.Models {
		name := strings.ToLower(model.ID + " " + model.Name)
		if strings.Contains(name, "flash") {
			return model.ID
		}
	}
	cheapest, window := "", 0
	for _, model := range e.settings.Provider.Models {
		if model.ContextLimit > 0 && (window == 0 || model.ContextLimit < window) {
			cheapest, window = model.ID, model.ContextLimit
		}
	}
	if cheapest != "" {
		return cheapest
	}
	return e.settings.Provider.ActiveModelID
}

// shouldRunAdvisor reports whether the advisor may choose a model for the task
// about to start. It runs at EVERY task boundary — the only moment a switch is
// safe, because no request has been made yet — and the model it picks is then
// fixed for that whole task.
//
// It used to run only on a session's first task, freezing the pairing forever,
// because a mid-session switch cold-started the prefix AND broke the execution
// chain's continuation. The execution chain is gone with the subagents, and the
// remaining cost (a one-time cold start on the new model) is a price the
// advisor is explicitly told to weigh rather than a reason to forbid the choice.
func (e *Engine) shouldRunAdvisor() bool {
	mode := strings.ToLower(strings.TrimSpace(e.settings.Provider.Advisor))
	if mode == "off" || mode == "pinned" {
		return false
	}
	// A user-pinned model is never overridden unless explicitly routed.
	if e.settings.Provider.RolesPinned && mode != "routed" {
		return false
	}
	// Nothing to choose between.
	return len(e.settings.Provider.Models) > 1
}

// advisorCatalog renders the model universe for the prompt: id, window, family,
// and whether the family supports continuation — which tells the advisor how
// well a long conversation will keep hitting cache there. Sorted for
// determinism, so the advisor prompt is byte-stable across turns.
func advisorCatalog(models []contract.Model) string {
	lines := make([]string, 0, len(models))
	for _, model := range models {
		profile := gateway.ResolveModelProfile(model.ID + " " + model.Name)
		continuation := "no"
		if profile.ContinuationLinking == gateway.ContinuationSupported {
			continuation = "yes"
		}
		window := model.ContextLimit
		if window <= 0 {
			window = profile.DefaultContextWindow
		}
		lines = append(lines, fmt.Sprintf("- %s — window %s, family %s, continuation %s", model.ID, contract.FullTokens(window), profile.Family, continuation))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// reconcileCatalog checks, once before the session's first request, that the
// models this session is configured to use actually exist in the live catalog.
//
// The gateway's model rows are managed directly in its production database, so
// a row can be renamed, deactivated, or (because the visibility migration
// defaults new rows to hidden) simply not returned to this client. Any of those
// used to surface as a 404 on the FIRST REAL REQUEST — mid-task, after the user
// had already described what they wanted.
//
// Substituting happens through the same applyModelSwitch the advisor uses, so
// it runs before RolesPinned and before any cached bytes exist. Silent when the
// catalog is fine, which is the overwhelmingly common case.
func (e *Engine) reconcileCatalog() error {
	if len(e.settings.Provider.Models) == 0 || e.UsageAggregate().Requests > 0 {
		return nil // nothing to check against, or too late to change anything
	}
	known := make(map[string]bool, len(e.settings.Provider.Models))
	for _, model := range e.settings.Provider.Models {
		known[model.ID] = true
	}
	configured := e.settings.Provider.ActiveModelID
	if configured == "" || known[configured] {
		return nil
	}
	
	if strings.ToLower(strings.TrimSpace(e.settings.Provider.Advisor)) == "pinned" || e.settings.Provider.RolesPinned {
		return fmt.Errorf("configured model %q is missing from the catalog and substitution is blocked", configured)
	}

	replacement := e.substituteFor()
	if replacement.ID == "" {
		e.callbacks.EmitNotice(fmt.Sprintf("The configured model %q is not available on this gateway and no substitute was found — requests will fail until the catalog or your config is corrected.", configured))
		return nil
	}
	addendum := gateway.ResolveModelProfile(replacement.ID + " " + replacement.Name).PromptAddendum
	if err := e.applyModelSwitch(context.Background(), "main", replacement.ID, replacement.Name, addendum); err != nil {
		return nil
	}
	e.callbacks.EmitNotice(fmt.Sprintf("The configured model %q is not available on this gateway; using %s instead.", configured, replacement.Name))
	return nil
}

// substituteFor picks the best available stand-in when the configured model is
// missing from the catalog: the largest window, which is the safest default for
// a session whose conversation has to fit. Ties break on ID so a broken catalog
// produces the same choice every run.
//
// The per-role variant (a cheap executor kept distinct from an expensive
// planner, preferring a continuation-capable family) went with the roles.
func (e *Engine) substituteFor() contract.Model {
	models := append([]contract.Model(nil), e.settings.Provider.Models...)
	sort.Slice(models, func(i, j int) bool {
		left, right := models[i], models[j]
		if left.ContextLimit != right.ContextLimit {
			return left.ContextLimit > right.ContextLimit
		}
		return left.ID < right.ID
	})
	if len(models) == 0 {
		return contract.Model{}
	}
	return models[0]
}

// runTaskAdvisor consults the utility model at a task boundary and applies its
// choice for that task. Every failure mode — disabled, pinned, no catalog,
// timeout, malformed answer, unknown id, a window that will not fit, an apply
// error — keeps the current model silently. The advisor must never be a point
// of failure: a task that runs is worth more than a marginally better model.
func (e *Engine) runTaskAdvisor(ctx context.Context, prompt string, workspaceSignal string) {
	if !e.shouldRunAdvisor() {
		return
	}
	modelID := e.utilityModelID()
	if strings.TrimSpace(modelID) == "" {
		return
	}
	current := e.settings.Provider.ActiveModelID
	// Tell the advisor what a switch would COST, not just what is available.
	// Without this it optimizes capability in a vacuum and proposes moves that
	// re-send an entire conversation uncached to win a marginally better model.
	coldStart := e.inUseContextTokens()
	warm := "none yet"
	if models := e.warmModelsThisSession(); len(models) > 0 {
		warm = strings.Join(models, ", ")
	}
	user := fmt.Sprintf("CURRENT\n%s\n\nAVAILABLE MODELS\n%s\n\nSWITCH COST\nMoving to a model this session has not used re-sends about %s tokens of conversation uncached. Already warm this session: %s.\n\nWORKSPACE\n%s\n\nTASK\n%s",
		current, advisorCatalog(e.settings.Provider.Models), contract.FullTokens(coldStart), warm, workspaceSignal, contract.TruncateEllipsis(prompt, 2000))

	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	started := time.Now()
	// A deliberately cold, one-shot isolated stream on its own pin — the same
	// shape as the onboarding call, so it coexists with the prefix-shape guard.
	response, err := e.provider.Chat(callCtx, contract.ChatRequest{
		SessionID: e.session.ID + ":sub:advisor", ModelID: modelID,
		Reasoning: contract.ReasoningLow, MaxTokens: advisorMaxTokens,
		Messages: []contract.Message{
			{Role: contract.RoleSystem, Content: instructions.AdvisorSystemBody},
			{Role: contract.RoleUser, Content: user},
		},
	})
	cancel()
	_ = e.recordUsageAndEmit(func() error {
		return e.recordAuxUsage(ctx, modelID, ":sub:advisor", response.Usage, elapsedMS(started))
	})
	if err != nil {
		e.recordHarnessEvent(ctx, contract.HarnessRecovery, "advisor-unavailable", "keeping the current model")
		return
	}
	decision, ok := parseAdvisorDecision(response.Content)
	if !ok || decision.Keep {
		return
	}
	chosen := e.resolveCatalogModel(decision.Model)
	if chosen == "" || chosen == current {
		return
	}
	// Two hard gates the advisor cannot talk its way past.
	//
	// FIT: the conversation this task inherits must fit the new model's window.
	// Switching into a smaller window would silently drop the oldest messages at
	// assembly time — context destruction disguised as a model upgrade.
	if !e.historyFitsModel(chosen) {
		e.recordHarnessEvent(ctx, contract.HarnessRecovery, "model-switch-declined", "conversation does not fit "+chosen)
		return
	}
	// COST: a model this session has never used holds no cache for this
	// conversation, so it re-reads every token at full price. That is affordable
	// while the conversation is small, and affordable at any size when returning
	// to a model already warm here. Otherwise the switch costs more than it can
	// plausibly win, and the current model — whose cache IS warm — keeps the task.
	if cost := e.switchCost(chosen); !cost.Affordable {
		e.recordHarnessEvent(ctx, contract.HarnessRecovery, "model-switch-declined",
			fmt.Sprintf("cold start of %s on %s costs more than the switch can win", contract.FullTokens(cost.ColdStartTokens), chosen))
		return
	}
	profile := gateway.ResolveModelProfile(chosen + " " + e.catalogModelName(chosen))
	if e.applyModelSwitch(ctx, "main", chosen, e.catalogModelName(chosen), profile.PromptAddendum) != nil {
		return
	}
	reason := strings.TrimSpace(decision.Why)
	if reason == "" {
		reason = "better fit for this task"
	}
	e.callbacks.EmitNotice("Switched to " + e.catalogModelName(chosen) + " for this task — " + reason)
}

// historyFitsModel and the switch-cost helpers live in switchcost.go.

// parseAdvisorDecision tolerates a fenced code block around the JSON, which
// small models often add despite instructions.
func parseAdvisorDecision(content string) (advisorDecision, bool) {
	text := strings.TrimSpace(content)
	if strings.HasPrefix(text, "```") && strings.HasSuffix(text, "```") {
		if lines := strings.Split(text, "\n"); len(lines) >= 3 {
			text = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	var decision advisorDecision
	if json.Unmarshal([]byte(text), &decision) != nil {
		return advisorDecision{}, false
	}
	return decision, true
}

// resolveCatalogModel maps an advisor-named id onto a real catalog entry,
// returning "" when it named something that does not exist.
func (e *Engine) resolveCatalogModel(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	for _, model := range e.settings.Provider.Models {
		if strings.EqualFold(model.ID, id) || strings.EqualFold(model.Name, id) {
			return model.ID
		}
	}
	return ""
}

func (e *Engine) catalogModelName(id string) string {
	for _, model := range e.settings.Provider.Models {
		if model.ID == id {
			if strings.TrimSpace(model.Name) != "" {
				return model.Name
			}
			return model.ID
		}
	}
	return id
}

// maybeAdviseFreshSession is the mid-session half of the caching directive:
// when a NEW task in an ongoing session is large AND shares nothing with the
// work this session has done, a fresh session would serve it better — a clean
// model choice and a cache that is not carrying unrelated context. It emits
// ONE notice per session, never blocks, and costs no model call.
func (e *Engine) maybeAdviseFreshSession(assessment Assessment, prompt string) {
	if e.freshSessionAdvised || e.UsageAggregate().Requests == 0 {
		return
	}
	if assessment.Class != ClassLarge && assessment.Class != ClassEpic {
		return
	}
	if e.knowledge == nil || e.knowledge.RelatedToSession(prompt) {
		return
	}
	e.freshSessionAdvised = true
	e.callbacks.EmitNotice("This looks like different work than the rest of this session — /new would give it a clean start and a fresh cache.")
}

// workspaceSignal is the one-line project description the advisor sees: how
// large the workspace is. Reuses the review gate's bounded source-file probe,
// so it costs no extra traversal budget.
func (e *Engine) workspaceSignal() string {
	root := strings.TrimSpace(e.session.WorkspacePath)
	if root == "" {
		return "no workspace"
	}
	count := countWorkspaceSourceFiles(root, researchLensProbeCeiling)
	return fmt.Sprintf("%s repository (~%d source files)", RepoBucketForFileCount(count), count)
}
