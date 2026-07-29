package orchestrator

import (
	"fmt"
	"regexp"
	"strings"

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
	Class                   TaskClass
	Risky                   bool
	Reason                  string
	ScopeGuard              bool
	SingleArtifactExtension string
}

type Budget struct {
	Class                   TaskClass
	Risky                   bool
	ToolCalls               int
	MaxTurns                int
	MaxChecks               int
	MaxTaskTokens           int
	MaxOutputTokens         int
	Reasoning               contract.ReasoningTier
	Verification            string
	SingleArtifactExtension string
	Brief                   string
}

var (
	greetingRE       = regexp.MustCompile(`(?i)^(hi+|hey+|hello+|yo|sup|hola|salam|salaam|marhaba|ahlan|thanks?|thank you|thx|ty|ok(ay)?|cool|nice|great|good (morning|afternoon|evening|night)|how are you\??|test(ing)?)[\s!.?]*$`)
	questionRE       = regexp.MustCompile(`(?i)^(what|who|when|where|why|how|is|are|was|were|does|do|did|should i|tell me|explain|define|compare|convert|calculate|translate|summari[sz]e)\b`)
	continuationRE   = regexp.MustCompile(`(?i)^(continue|go (on|ahead)|proceed|keep going|resume|carry on|next( step)?|do it|yes|finish( it)?|and then)[\s!.?]*$`)
	codeRE           = regexp.MustCompile("(?m)```|=>|;\\s*$|\\b(function|class|import|const|def|struct|interface|func|package)\\b")
	pathRE           = regexp.MustCompile(`(?i)(^|[\s"'` + "`" + `(])([\w.-]+[/\\])*[\w.-]+\.(ts|tsx|js|jsx|json|go|py|rb|rs|java|kt|cs|cpp|c|h|css|html|vue|svelte|md|yml|yaml|toml|sql|sh|ps1|env)\b|[\w.-]+[/\\][\w.-]+[/\\][\w/\\.-]+`)
	repoRE           = regexp.MustCompile(`(?i)\b(repo|repository|codebase|project|app|file|files|folder|directory|module|component|function|class|method|test|tests|bug|error|build|compile|lint|typecheck|api|database|schema|diff|package|dependency|ui|page|screen|button|form)\b`)
	changeRE         = regexp.MustCompile(`(?i)\b(add|fix|change|update|refactor|implement|create|build|make|remove|delete|rename|move|write|convert|improve|optimi[sz]e|integrate|configure|debug|migrate|upgrade|redesign|rewrite|extend|support|polish|enhance)\b`)
	breadthRE        = regexp.MustCompile(`(?i)\b(entire|whole (codebase|project|app|repo)|all (files|pages|components|modules|tests|routes)|across the|every (file|page|component|module)|rewrite|redesign|overhaul|re-?architect|migration|migrate|audit|from scratch|end[- ]to[- ]end)\b`)
	tinyRE           = regexp.MustCompile(`(?i)\b(typo|rename|bump|comment|one[- ]?line|quick|tiny|trivial|small tweak|label|placeholder|tooltip|colou?r|padding|margin|font|title|wording|version number)\b`)
	featureRE        = regexp.MustCompile(`(?i)\b(management|dashboard|admin panel|page|screen|auth(entication)?|login flow|integration|system|workflow|onboarding|settings|profile|notifications?|crud|api for)\b`)
	riskRE           = regexp.MustCompile(`(?i)\b(delete|drop|truncate|wipe|migration|migrate|auth|login|session|password|token|secret|credential|payment|billing|checkout|production|deploy|release|security|encrypt|permission)\b`)
	qualityRE        = regexp.MustCompile(`(?i)\b(polish(ed)?|perfect(ly)?|flawless|bullet-?proof|production[- ]?(grade|ready)|100\s*%|make sure everything|fully working)\b`)
	simpleArtifactRE = regexp.MustCompile(`(?i)\b(simple|basic|minimal|small|quick)\b.*\b(game|page|site|app|demo)\b`)
	singleArtifactRE = regexp.MustCompile(`(?i)\b(?:single|one|standalone|self[- ]contained)\s+(?:self[- ]contained\s+)?(html|javascript|js|css|python|py|go)\s+file\b`)
	bulletRE         = regexp.MustCompile(`^\s*([-*]|[0-9]+[.)])\s`)
	// planRequestRE (P3b): the user is asking the agent to CREATE a plan (not execute
	// one). Matches "create/make/write/draft a plan", "plan out/first/before", "plan
	// how to". Only used to keep such a request out of the chat class.
	planRequestRE = regexp.MustCompile(`(?i)\b(create|make|write|draft|prepare|design|outline|come up with|need|want|give me)\b[^.!?\n]{0,30}\bplan\b|\bplan\b\s+(this\s+|the\s+|it\s+)?(out|first|before|how)\b|^\s*plan\s+(out|how|the|this)\b`)
	// planDocRE (P3a): the user is asking to EXECUTE/USE an existing plan document.
	// Requires a co-occurring .md path (planDocPathRE) so a plain "run the plan" does
	// not steal a plain continuation; execution of a named file beats plan-creation.
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
	singleArtifactExtension := requestedSingleArtifactExtension(text)
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
	// file paths used to force the heaviest class — and with it an unconditional
	// review — on prompts that were otherwise ordinary. Corroboration keeps the
	// heaviest class for genuinely broad work; the review gate is the authoritative
	// backstop either way.
	largeSignals := 0
	for _, signal := range []bool{len(text) > 1200, breadth > 0, bullets >= 8, paths >= 4} {
		if signal {
			largeSignals++
		}
	}
	largeThreshold := 2
	assessment := Assessment{Class: ClassStandard, Risky: risky, Reason: "typical multi-step task"}
	switch {
	case len(text) > 2500 || breadth >= 2 || (breadth > 0 && len(text) > 1200):
		assessment.Class, assessment.Reason = ClassEpic, "architecture-scale request"
	case largeSignals >= largeThreshold:
		assessment.Class, assessment.Reason = ClassLarge, "broad multi-part request"
	case changeRE.MatchString(text) && featureRE.MatchString(text):
		assessment.Class, assessment.Reason = ClassStandard, "feature-scope request"
	case singleArtifactExtension != "" && simpleArtifactRE.MatchString(text) && len(text) <= 400 && bullets <= 2:
		assessment.Class, assessment.Reason = ClassTiny, "simple single-file artifact"
	case len(text) <= 200 && bullets == 0 && paths <= 1 && tinyRE.MatchString(text):
		assessment.Class, assessment.Reason = ClassTiny, "single trivial change"
	case len(text) <= 400 && bullets <= 2 && paths <= 2:
		assessment.Class, assessment.Reason = ClassSmall, "single focused change"
	}
	assessment.SingleArtifactExtension = singleArtifactExtension
	assessment.ScopeGuard = qualityRE.MatchString(text) && assessment.Class != ClassChat
	// Asking for a plan (or pointing at a plan document) is real work, never
	// chat/tiny — the model decides how to plan; the classifier only sizes it.
	if (planRequest || planDoc) && (assessment.Class == ClassChat || assessment.Class == ClassTiny) {
		assessment.Class = ClassSmall
		assessment.Reason = "planning request"
	}
	return assessment
}

var classToolCaps = map[TaskClass]int{ClassChat: 6, ClassTiny: 5, ClassSmall: 10, ClassStandard: 20, ClassLarge: 40, ClassEpic: 64}
var classTurns = map[TaskClass]int{ClassChat: 6, ClassTiny: 5, ClassSmall: 10, ClassStandard: 18, ClassLarge: 32, ClassEpic: 48}
var classCheckCaps = map[TaskClass]int{ClassChat: 1, ClassTiny: 1, ClassSmall: 2, ClassStandard: 3, ClassLarge: 5, ClassEpic: 8}
var classTokenCaps = map[TaskClass]int{ClassChat: 0, ClassTiny: 0, ClassSmall: 0, ClassStandard: 0, ClassLarge: 0, ClassEpic: 0}
var classOutputCaps = map[TaskClass]int{ClassChat: 32_000, ClassTiny: 32_000, ClassSmall: 32_000, ClassStandard: 32_000, ClassLarge: 32_000, ClassEpic: 32_000}
var classVerify = map[TaskClass]string{ClassChat: "none", ClassTiny: "one-cheap-check", ClassSmall: "targeted", ClassStandard: "standard", ClassLarge: "thorough", ClassEpic: "thorough"}

func BudgetFor(a Assessment, effort EffortProfile) Budget {
	verification := classVerify[a.Class]
	if a.Risky {
		verification = bumpVerification(verification)
	}
	// Reasoning effort is the user's chosen level (sent raw to the gateway);
	// the brief reports it so the model knows the depth it is being given.
	b := Budget{
		Class: a.Class, Risky: a.Risky,
		ToolCalls:               classToolCaps[a.Class],
		MaxTurns:                min(effort.MaxTurns, classTurns[a.Class]),
		MaxChecks:               classCheckCaps[a.Class],
		MaxTaskTokens:           classTokenCaps[a.Class],
		MaxOutputTokens:         classOutputCaps[a.Class],
		Reasoning:               ReasoningForTask(a.Class, effort.Level),
		Verification:            verification,
		SingleArtifactExtension: a.SingleArtifactExtension,
	}
	b.Brief = buildBrief(b, a)
	return b
}

// buildBrief renders the per-task tail brief from a computed budget.
func buildBrief(b Budget, a Assessment) string {
	guard := ""
	if a.ScopeGuard {
		guard = " scope=only the named target; quality words do not widen scope;"
	}
	// The brief rides every user-message tail under a ~50-token budget (SC-004,
	// request-assembly.md §4). It carries no agent count: delegation scale is the
	// model's judgment, not a number to spend down. It deliberately avoids a
	// mandatory DONE declaration or tasks.md file: those are useful only when
	// the task itself needs them.
	scope := ""
	if b.SingleArtifactExtension != "" {
		scope = " single=" + b.SingleArtifactExtension + ",no-scratch;"
	}
	tokenStr := "unlimited"
	if b.MaxTaskTokens > 0 {
		tokenStr = fmt.Sprintf("%d", b.MaxTaskTokens)
	}
	return fmt.Sprintf("[task: %s; hard tools<=%d checks<=%d turns<=%d tokens<=%s; reasoning=%s; verify=%s;%s%s finish]",
		b.Class, b.ToolCalls, b.MaxChecks, b.MaxTurns, tokenStr, b.Reasoning, b.Verification, scope, guard)
}

func requestedSingleArtifactExtension(text string) string {
	match := singleArtifactRE.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	switch strings.ToLower(match[1]) {
	case "javascript", "js":
		return ".js"
	case "python", "py":
		return ".py"
	default:
		return "." + strings.ToLower(match[1])
	}
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
