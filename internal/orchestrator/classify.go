package orchestrator

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type TaskClass string

const (
	ClassChat     TaskClass = "chat"
	ClassTiny     TaskClass = "tiny"
	ClassSmall    TaskClass = "small"
	ClassStandard TaskClass = "standard"
	ClassLarge    TaskClass = "large"
	ClassEpic     TaskClass = "epic"
)

type Assessment struct {
	Class      TaskClass
	Risky      bool
	Reason     string
	ScopeGuard bool
	// PlanRequest (P3b): the user explicitly asked the agent to CREATE a plan, so it
	// enters the planning pipeline (proposes a plan, pauses for proceed-now/later)
	// regardless of size — the natural-language replacement for the removed /plan.
	PlanRequest bool
	// PlanDoc (P3a): the user asked to EXECUTE an existing plan document (…the plan in
	// X.md). The agent skips research/planning and mirrors that file into the to-dos.
	PlanDoc     bool
	PlanDocPath string // the referenced markdown path (best-effort extraction)
}

// PlanNeedVerdict is the explainable, user-visible routing decision for the
// enforced orchestration pipeline. Task size selects depth; effort continues
// to select only the unchanged per-phase agent allowance.
type PlanNeedVerdict struct {
	NeedsPlan bool
	Depth     string
	Reason    string
}

const (
	PipelineDepthLight = "light"
	PipelineDepthFull  = "full"
)

// NeedsPlan is deliberately a pure predicate over the existing classifier.
// Feature 014 (cheap-by-default, user directive): conversational, tiny,
// small, AND standard work all stay on the direct path — a "fix two bugs"
// request gets fixed, not ceremonied through research/plan/approval. Only
// corroborated large/epic work (or an explicit plan request of any size)
// enters the pipeline.
func NeedsPlan(assessment Assessment) PlanNeedVerdict {
	verdict := PlanNeedVerdict{Reason: assessment.Reason}
	// P3(a): executing an existing plan document never enters the research/planning
	// pipeline — the plan already exists; the agent mirrors it into the to-dos and runs.
	if assessment.PlanDoc {
		return verdict
	}
	// P3(b): an explicit "create a plan" request always enters the pipeline (proposes
	// a plan, pauses for proceed-now/later) regardless of size — the natural-language
	// replacement for the removed /plan command.
	if assessment.PlanRequest {
		verdict.NeedsPlan = true
		verdict.Depth = PipelineDepthLight
		if assessment.Class == ClassLarge || assessment.Class == ClassEpic {
			verdict.Depth = PipelineDepthFull
		}
		return verdict
	}
	if assessment.Class == ClassLarge || assessment.Class == ClassEpic {
		verdict.NeedsPlan = true
		verdict.Depth = PipelineDepthFull
	}
	return verdict
}

type Budget struct {
	Class        TaskClass
	Risky        bool
	ToolCalls    int
	MaxTurns     int
	MaxAgentRuns int
	Reasoning    contract.ReasoningTier
	Verification string
	Brief        string
}

var (
	greetingRE     = regexp.MustCompile(`(?i)^(hi+|hey+|hello+|yo|sup|hola|salam|salaam|marhaba|ahlan|thanks?|thank you|thx|ty|ok(ay)?|cool|nice|great|good (morning|afternoon|evening|night)|how are you\??|test(ing)?)[\s!.?]*$`)
	questionRE     = regexp.MustCompile(`(?i)^(what|who|when|where|why|how|is|are|was|were|does|do|did|should i|tell me|explain|define|compare|convert|calculate|translate|summari[sz]e)\b`)
	continuationRE = regexp.MustCompile(`(?i)^(continue|go (on|ahead)|proceed|keep going|resume|carry on|next( step)?|do it|yes|finish( it)?|and then)[\s!.?]*$`)
	// planProceedRE (004 US2, T7) widens continuation detection for a saved plan
	// beyond the bare tokens above: an affirmative execution verb at the START of
	// the message, optionally followed by "the plan"/"it"/"now". Anchored at ^ so
	// "the plan is wrong" and "don't proceed" do not match; the caller further
	// guards it to fire only while a plan is pending/interrupted, and step-progress
	// detection (T8) is the phrasing-independent backstop.
	planProceedRE = regexp.MustCompile(`(?i)^(proceed|go ahead|continue|resume|carry on|execute|run|start|begin|do)\b.{0,30}\b(plan|it|now)\b[\s!.?]*$`)
	// planDiscardRE (P2): a natural-language discard of a pending/interrupted plan —
	// the replacement for /plan clear. End-anchored so "discard the plan and do X"
	// falls through to normal handling; the caller guards it to fire only while a
	// plan invites a proceed.
	planDiscardRE = regexp.MustCompile(`(?i)^\s*(discard|drop|forget|abandon|scrap|throw away|get rid of|cancel)\b.{0,20}\b(plan|it)\b[\s!.?]*$`)
	codeRE        = regexp.MustCompile("(?m)```|=>|;\\s*$|\\b(function|class|import|const|def|struct|interface|func|package)\\b")
	pathRE        = regexp.MustCompile(`(?i)(^|[\s"'` + "`" + `(])([\w.-]+[/\\])*[\w.-]+\.(ts|tsx|js|jsx|json|go|py|rb|rs|java|kt|cs|cpp|c|h|css|html|vue|svelte|md|yml|yaml|toml|sql|sh|ps1|env)\b|[\w.-]+[/\\][\w.-]+[/\\][\w/\\.-]+`)
	repoRE        = regexp.MustCompile(`(?i)\b(repo|repository|codebase|project|app|file|files|folder|directory|module|component|function|class|method|test|tests|bug|error|build|compile|lint|typecheck|api|database|schema|diff|package|dependency|ui|page|screen|button|form)\b`)
	changeRE      = regexp.MustCompile(`(?i)\b(add|fix|change|update|refactor|implement|create|build|make|remove|delete|rename|move|write|convert|improve|optimi[sz]e|integrate|configure|debug|migrate|upgrade|redesign|rewrite|extend|support|polish|enhance)\b`)
	breadthRE     = regexp.MustCompile(`(?i)\b(entire|whole (codebase|project|app|repo)|all (files|pages|components|modules|tests|routes)|across the|every (file|page|component|module)|rewrite|redesign|overhaul|re-?architect|migration|migrate|audit|from scratch|end[- ]to[- ]end)\b`)
	tinyRE        = regexp.MustCompile(`(?i)\b(typo|rename|bump|comment|one[- ]?line|quick|tiny|trivial|small tweak|label|placeholder|tooltip|colou?r|padding|margin|font|title|wording|version number)\b`)
	featureRE     = regexp.MustCompile(`(?i)\b(management|dashboard|admin panel|page|screen|auth(entication)?|login flow|integration|system|workflow|onboarding|settings|profile|notifications?|crud|api for)\b`)
	riskRE        = regexp.MustCompile(`(?i)\b(delete|drop|truncate|wipe|migration|migrate|auth|login|session|password|token|secret|credential|payment|billing|checkout|production|deploy|release|security|encrypt|permission)\b`)
	agentRE       = regexp.MustCompile(`(?i)\b(sub-?agents?|delegate|parallel agents?)\b`)
	qualityRE     = regexp.MustCompile(`(?i)\b(polish(ed)?|perfect(ly)?|flawless|bullet-?proof|production[- ]?(grade|ready)|100\s*%|make sure everything|fully working)\b`)
	bulletRE      = regexp.MustCompile(`^\s*([-*]|[0-9]+[.)])\s`)
	// planRequestRE (P3b): the user is asking the agent to CREATE a plan (not execute
	// one). Matches "create/make/write/draft a plan", "plan out/first/before", "plan
	// how to". Routes to the pipeline so the agent proposes a plan and pauses.
	planRequestRE = regexp.MustCompile(`(?i)\b(create|make|write|draft|prepare|design|outline|come up with|need|want|give me)\b[^.!?\n]{0,30}\bplan\b|\bplan\b\s+(this\s+|the\s+|it\s+)?(out|first|before|how)\b|^\s*plan\s+(out|how|the|this)\b`)
	// planDocRE (P3a): the user is asking to EXECUTE/USE an existing plan document.
	// Requires a co-occurring .md path (planDocPathRE) so a plain "run the plan" does
	// not steal a pipeline "proceed"; execution of a named file beats plan-creation.
	planDocRE     = regexp.MustCompile(`(?i)\b(execute|run|follow|implement|apply|use|do|start|continue)\b[^.!?\n]{0,70}\bplan\b|\bplan\b[^.!?\n]{0,40}\.md\b`)
	planDocPathRE = regexp.MustCompile(`(?i)([\w./\\-]+\.md)\b`)
)

func Classify(raw string, previous TaskClass) Assessment {
	text := strings.TrimSpace(raw)
	risky := riskRE.MatchString(text)
	if greetingRE.MatchString(text) {
		return Assessment{Class: ClassChat, Reason: "greeting/smalltalk"}
	}
	if continuationRE.MatchString(text) {
		if previous == "" {
			previous = ClassStandard
		}
		return Assessment{Class: previous, Risky: risky, Reason: "continuation of previous task"}
	}
	// P3: detect plan-document execution vs plan-creation intent BEFORE the
	// conversational shortcuts, so a short "plan the auth flow" or "run PLAN.md" is
	// never misrouted to chat. A named .md file plus an execute verb means "run this
	// existing plan"; that beats plan-creation intent.
	mdPath := ""
	if match := planDocPathRE.FindStringSubmatch(text); len(match) > 1 {
		mdPath = match[1]
	}
	planDoc := mdPath != "" && planDocRE.MatchString(text)
	// A planning QUESTION ("how do I write a plan?") is not a request to create one.
	planRequest := !planDoc && planRequestRE.MatchString(text) && !questionRE.MatchString(text)
	paths := len(pathRE.FindAllStringIndex(text, 21))
	hasWorkspace := paths > 0 || codeRE.MatchString(text) || repoRE.MatchString(text)
	if !planRequest && !planDoc && !hasWorkspace && !changeRE.MatchString(text) && len(text) < 400 && (questionRE.MatchString(text) || strings.HasSuffix(text, "?")) {
		return Assessment{Class: ClassChat, Reason: "general question, no workspace involvement"}
	}
	if !planRequest && !planDoc && !hasWorkspace && !changeRE.MatchString(text) && len(text) < 80 {
		return Assessment{Class: ClassChat, Reason: "conversational message"}
	}
	lines := strings.Split(text, "\n")
	bullets := 0
	for _, line := range lines {
		if bulletRE.MatchString(line) {
			bullets++
		}
	}
	breadth := len(breadthRE.FindAllStringIndex(text, 21))
	// Large escalation requires TWO independent size signals (feature 011 D2/F2):
	// a single breadth word ("audit", "migrate", "all") or merely naming four
	// file paths used to force the full pipeline — and with it an unconditional
	// review — on prompts that were otherwise ordinary. Corroboration keeps the
	// full pipeline for genuinely broad work; the review gate is the authoritative
	// backstop either way.
	largeSignals := 0
	for _, signal := range []bool{len(text) > 1200, breadth > 0, bullets >= 8, paths >= 4} {
		if signal {
			largeSignals++
		}
	}
	largeThreshold := 2
	if reviewLegacyMode {
		// Baseline mode (MUHIYA_BENCH_LEGACY_REVIEW=1): pre-011 single-signal
		// escalation, so baseline runs route tasks exactly as the old build did.
		largeThreshold = 1
	}
	assessment := Assessment{Class: ClassStandard, Risky: risky, Reason: "typical multi-step task"}
	switch {
	case len(text) > 2500 || breadth >= 2 || (breadth > 0 && len(text) > 1200):
		assessment.Class, assessment.Reason = ClassEpic, "architecture-scale request"
	case largeSignals >= largeThreshold:
		assessment.Class, assessment.Reason = ClassLarge, "broad multi-part request"
	case changeRE.MatchString(text) && featureRE.MatchString(text):
		assessment.Class, assessment.Reason = ClassStandard, "feature-scope request"
	case len(text) <= 200 && bullets == 0 && paths <= 1 && tinyRE.MatchString(text):
		assessment.Class, assessment.Reason = ClassTiny, "single trivial change"
	case len(text) <= 400 && bullets <= 2 && paths <= 2:
		assessment.Class, assessment.Reason = ClassSmall, "single focused change"
	}
	assessment.ScopeGuard = qualityRE.MatchString(text) && assessment.Class != ClassChat
	if agentRE.MatchString(text) && (assessment.Class == ClassChat || assessment.Class == ClassTiny || assessment.Class == ClassSmall) {
		assessment.Class = ClassStandard
		assessment.Reason += "; subagents explicitly requested"
	}
	// P3: attach the plan-intent flags. A plan request/doc is real work, never chat/tiny.
	assessment.PlanRequest, assessment.PlanDoc, assessment.PlanDocPath = planRequest, planDoc, mdPath
	if (planRequest || planDoc) && (assessment.Class == ClassChat || assessment.Class == ClassTiny) {
		assessment.Class = ClassSmall
	}
	if planDoc {
		assessment.Reason = "execute an existing plan document"
	} else if planRequest {
		assessment.Reason = "create a plan, then pause for approval"
	}
	return assessment
}

var classToolBase = map[TaskClass]int{ClassChat: 3, ClassTiny: 8, ClassSmall: 15, ClassStandard: 30, ClassLarge: 60, ClassEpic: 100}
var classTurns = map[TaskClass]int{ClassChat: 6, ClassTiny: 10, ClassSmall: 16, ClassStandard: 26, ClassLarge: 44, ClassEpic: 64}
var classAgents = map[TaskClass]int{ClassChat: 0, ClassTiny: 0, ClassSmall: 0, ClassStandard: 2, ClassLarge: 5, ClassEpic: 8}
var classVerify = map[TaskClass]string{ClassChat: "none", ClassTiny: "targeted", ClassSmall: "targeted", ClassStandard: "standard", ClassLarge: "thorough", ClassEpic: "thorough"}
var effortScale = map[contract.EffortLevel]float64{contract.EffortLow: .75, contract.EffortMedium: 1, contract.EffortHigh: 1.3, contract.EffortMax: 1.8}

func BudgetFor(a Assessment, effort EffortProfile) Budget {
	verification := classVerify[a.Class]
	if a.Risky {
		verification = bumpVerification(verification)
	}
	// Reasoning effort is the user's chosen level (sent raw to the gateway);
	// the brief reports it so the model knows the depth it is being given.
	b := Budget{
		Class: a.Class, Risky: a.Risky,
		ToolCalls:    int(math.Max(2, math.Round(float64(classToolBase[a.Class])*effortScale[effort.Level]))),
		MaxTurns:     min(effort.MaxTurns, classTurns[a.Class]),
		MaxAgentRuns: min(effort.MaxAgentRuns, classAgents[a.Class]),
		Reasoning:    ReasoningForEffort(effort.Level),
		Verification: verification,
	}
	b.Brief = buildBrief(b, a)
	return b
}

// buildBrief renders the per-task tail brief from a computed budget. Kept as
// its own function so WithAgentFloor can regenerate the brief after adjusting
// the agent cap — the advertised budget must always match the enforced one.
func buildBrief(b Budget, a Assessment) string {
	guard := ""
	if a.ScopeGuard {
		guard = " scope=only the named target; quality words do not widen scope;"
	}
	done := " declare DONE before editing;"
	if a.Class == ClassChat {
		done = ""
	}
	// A zero budget is stated as an explicit prohibition, not a bare number:
	// "agents<=0" reads like an estimate, and models kept calling run_subagent
	// against it and burning turns on guaranteed failures. Kept terse — the
	// brief rides every user-message tail under a ~50-token budget (SC-004,
	// request-assembly.md §4), so the small-class brief must stay ≤181 chars.
	agents := fmt.Sprintf("agents<=%d", b.MaxAgentRuns)
	if b.MaxAgentRuns <= 0 {
		agents = "agents=0 (no run_subagent)"
	}
	return fmt.Sprintf("[task-brief: date=%s; class=%s; tools~%d; turns<=%d; %s; reasoning=%s; verify=%s;%s%s finish all plan steps]", time.Now().Format("2006-01-02"), b.Class, b.ToolCalls, b.MaxTurns, agents, b.Reasoning, b.Verification, guard, done)
}

// WithAgentFloor returns the budget with MaxAgentRuns raised to at least
// floor and the brief regenerated to match. Used by plan mode, whose plan
// block advertises delegated investigation: the advertised capability and the
// enforced cap must never contradict each other.
func (b Budget) WithAgentFloor(floor int, a Assessment) Budget {
	if b.MaxAgentRuns >= floor {
		return b
	}
	b.MaxAgentRuns = floor
	b.Brief = buildBrief(b, a)
	return b
}

func EscalateClass(value TaskClass) TaskClass {
	order := []TaskClass{ClassChat, ClassTiny, ClassSmall, ClassStandard, ClassLarge, ClassEpic}
	for i, class := range order {
		if class == value && i < len(order)-1 {
			return order[i+1]
		}
	}
	return ClassEpic
}

func bumpVerification(value string) string {
	switch value {
	case "none":
		return "targeted"
	case "targeted":
		return "standard"
	default:
		return "thorough"
	}
}
