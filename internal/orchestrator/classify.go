package orchestrator

import (
	"fmt"
	"math"
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
	Class      TaskClass
	Risky      bool
	Reason     string
	ScopeGuard bool
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
	codeRE         = regexp.MustCompile("(?m)```|=>|;\\s*$|\\b(function|class|import|const|def|struct|interface|func|package)\\b")
	pathRE         = regexp.MustCompile(`(?i)(^|[\s"'` + "`" + `(])([\w.-]+[/\\])*[\w.-]+\.(ts|tsx|js|jsx|json|go|py|rb|rs|java|kt|cs|cpp|c|h|css|html|vue|svelte|md|yml|yaml|toml|sql|sh|ps1|env)\b|[\w.-]+[/\\][\w.-]+[/\\][\w/\\.-]+`)
	repoRE         = regexp.MustCompile(`(?i)\b(repo|repository|codebase|project|app|file|files|folder|directory|module|component|function|class|method|test|tests|bug|error|build|compile|lint|typecheck|api|database|schema|diff|package|dependency|ui|page|screen|button|form)\b`)
	changeRE       = regexp.MustCompile(`(?i)\b(add|fix|change|update|refactor|implement|create|build|make|remove|delete|rename|move|write|convert|improve|optimi[sz]e|integrate|configure|debug|migrate|upgrade|redesign|rewrite|extend|support|polish|enhance)\b`)
	breadthRE      = regexp.MustCompile(`(?i)\b(entire|whole (codebase|project|app|repo)|all (files|pages|components|modules|tests|routes)|across the|every (file|page|component|module)|rewrite|redesign|overhaul|re-?architect|migration|migrate|audit|from scratch|end[- ]to[- ]end)\b`)
	tinyRE         = regexp.MustCompile(`(?i)\b(typo|rename|bump|comment|one[- ]?line|quick|tiny|trivial|small tweak|label|placeholder|tooltip|colou?r|padding|margin|font|title|wording|version number)\b`)
	featureRE      = regexp.MustCompile(`(?i)\b(management|dashboard|admin panel|page|screen|auth(entication)?|login flow|integration|system|workflow|onboarding|settings|profile|notifications?|crud|api for)\b`)
	riskRE         = regexp.MustCompile(`(?i)\b(delete|drop|truncate|wipe|migration|migrate|auth|login|session|password|token|secret|credential|payment|billing|checkout|production|deploy|release|security|encrypt|permission)\b`)
	agentRE        = regexp.MustCompile(`(?i)\b(sub-?agents?|delegate|parallel agents?)\b`)
	qualityRE      = regexp.MustCompile(`(?i)\b(polish(ed)?|perfect(ly)?|flawless|bullet-?proof|production[- ]?(grade|ready)|100\s*%|make sure everything|fully working)\b`)
	bulletRE       = regexp.MustCompile(`^\s*([-*]|[0-9]+[.)])\s`)
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
	paths := len(pathRE.FindAllStringIndex(text, 21))
	hasWorkspace := paths > 0 || codeRE.MatchString(text) || repoRE.MatchString(text)
	if !hasWorkspace && !changeRE.MatchString(text) && len(text) < 400 && (questionRE.MatchString(text) || strings.HasSuffix(text, "?")) {
		return Assessment{Class: ClassChat, Reason: "general question, no workspace involvement"}
	}
	if !hasWorkspace && !changeRE.MatchString(text) && len(text) < 80 {
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
	assessment := Assessment{Class: ClassStandard, Risky: risky, Reason: "typical multi-step task"}
	switch {
	case len(text) > 2500 || breadth >= 2 || (breadth > 0 && len(text) > 1200):
		assessment.Class, assessment.Reason = ClassEpic, "architecture-scale request"
	case len(text) > 1200 || breadth > 0 || bullets >= 8 || paths >= 4:
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
	guard := ""
	if a.ScopeGuard {
		guard = " scope=only the named target; quality words do not widen scope;"
	}
	done := " declare DONE before editing;"
	if a.Class == ClassChat {
		done = ""
	}
	b.Brief = fmt.Sprintf("[task-brief: class=%s; tools~%d; turns<=%d; agents<=%d; reasoning=%s; verify=%s;%s%s finish all plan steps]", b.Class, b.ToolCalls, b.MaxTurns, b.MaxAgentRuns, b.Reasoning, b.Verification, guard, done)
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
