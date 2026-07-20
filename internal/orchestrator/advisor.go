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

// The session advisor (NATIVE_AGENT_PLAN §6 E2). Caching is the priority, so
// the session's models are FROZEN once work begins: a mid-session switch
// cold-starts the main prefix AND breaks the execution chain's continuation,
// which is the opposite of what this architecture exists to protect.
//
// The advisor therefore runs at most ONCE, on the first prompt of a session,
// before the first main request — the only moment when a switch is free
// because nothing is cached yet. Its expected answer is "keep": the configured
// pairing (MiniMax M3 planning, DeepSeek V4 Pro executing) covers almost
// everything. It exists for the case where a session's first task obviously
// outgrows the configured main model's context or capability.
//
// A different workload arriving MID-session does not switch anything: it gets
// the fresh-session advisory instead (see freshSessionAdvice).

// advisorMaxTokens keeps the aux call trivially cheap; the answer is one small
// JSON object.
const advisorMaxTokens = 200

// advisorDecision is the utility model's answer.
type advisorDecision struct {
	Keep bool   `json:"keep"`
	Main string `json:"main"`
	Sub  string `json:"sub"`
	Why  string `json:"why"`
}

// utilityModelID resolves the cheap "instructing" model used for aux calls
// (the advisor, onboarding). It prefers a Flash-class model in the catalog and
// falls back to the configured subagent model, so a catalog without one still
// works rather than failing.
func (e *Engine) utilityModelID() string {
	for _, model := range e.settings.Provider.Models {
		name := strings.ToLower(model.ID + " " + model.Name)
		if strings.Contains(name, "flash") {
			return model.ID
		}
	}
	return e.settings.Provider.SubagentModelID
}

// shouldRunAdvisor reports whether this task is the session's first, with the
// advisor enabled and the roles not user-pinned. Everything else — every later
// task in the session — is a hard no: that is what makes "models never change
// mid-session" a mechanical invariant rather than a convention.
func (e *Engine) shouldRunAdvisor() bool {
	if strings.EqualFold(strings.TrimSpace(e.settings.Provider.Advisor), "off") {
		return false
	}
	if e.settings.Provider.RolesPinned {
		return false
	}
	if e.UsageAggregate().Requests > 0 {
		return false
	}
	// Nothing to choose between.
	return len(e.settings.Provider.Models) > 1
}

// advisorCatalog renders the model universe for the prompt: id, window, family,
// and whether the family supports continuation (which is what makes a model a
// good executor). Sorted for determinism.
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
		lines = append(lines, fmt.Sprintf("- %s — window %s, family %s, continuation %s", model.ID, contract.HumanTokens(window), profile.Family, continuation))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// runSessionAdvisor consults the utility model once and applies its choice.
// Every failure mode — disabled, no catalog, timeout, malformed answer,
// unknown id, apply error — keeps the configured models silently. The advisor
// must never be a point of failure; a session that starts is worth more than a
// marginally better model.
func (e *Engine) runSessionAdvisor(ctx context.Context, prompt string, workspaceSignal string) {
	if !e.shouldRunAdvisor() {
		return
	}
	modelID := e.utilityModelID()
	if strings.TrimSpace(modelID) == "" {
		return
	}
	configuredMain, configuredSub := e.settings.Provider.ActiveModelID, e.settings.Provider.SubagentModelID
	user := fmt.Sprintf("CONFIGURED\nmain %s\nsub %s\n\nAVAILABLE MODELS\n%s\n\nWORKSPACE\n%s\n\nTASK\n%s",
		configuredMain, configuredSub, advisorCatalog(e.settings.Provider.Models), workspaceSignal, contract.TruncateEllipsis(prompt, 2000))

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
		e.recordHarnessEvent(ctx, contract.HarnessRecovery, "advisor-unavailable", "keeping the configured models")
		return
	}
	decision, ok := parseAdvisorDecision(response.Content)
	if !ok || decision.Keep {
		return
	}
	mainID := e.resolveCatalogModel(decision.Main)
	subID := e.resolveCatalogModel(decision.Sub)
	if mainID == "" && subID == "" {
		return
	}
	applied := false
	if subID != "" && subID != configuredSub {
		if e.applyModelSwitch(ctx, "subagent", subID, e.catalogModelName(subID), "") == nil {
			applied = true
		}
	}
	if mainID != "" && mainID != configuredMain {
		profile := gateway.ResolveModelProfile(mainID + " " + e.catalogModelName(mainID))
		if e.applyModelSwitch(ctx, "main", mainID, e.catalogModelName(mainID), profile.PromptAddendum) == nil {
			applied = true
		}
	}
	if applied {
		reason := strings.TrimSpace(decision.Why)
		if reason == "" {
			reason = "better fit for this session"
		}
		e.callbacks.EmitNotice("Models for this session: " + e.settings.Provider.ActiveModelID + " planning, " + e.settings.Provider.SubagentModelID + " executing — " + reason)
	}
}

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
