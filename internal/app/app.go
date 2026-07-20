// Package app is the frontend-neutral core seam. It holds the types a
// frontend needs to drive a MuhiyaCode session — the runtime handle, the
// backend action set, and the view models they exchange — plus the prompt
// assembly every frontend must perform identically.
//
// It exists so a second frontend (the planned desktop app) can be written
// against the same core the terminal UI uses, without importing the terminal
// UI. Before this package those types were declared inside internal/tui and
// implemented by internal/command, which meant the composition root depended
// on the terminal renderer just to name its own return values.
//
// Layering: app sits above orchestrator and below the frontends. It must never
// import internal/tui.
package app

import (
	"context"
	"sort"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
	"golang.org/x/text/unicode/norm"
)

// Runtime is one live session: the engine plus the session and settings it was
// built for.
type Runtime struct {
	Engine   *orchestrator.Engine
	Session  contract.Session
	Settings *contract.Settings
}

// Skill is a discovered skill a frontend can list and queue for the next turn.
type Skill struct {
	Name, Description, Instructions, Path string
}

// UsageData is the account usage shown by /usage (003 US5), mapped from the
// gateway GET /v1/usage response. Credits are in credits (1 credit = $0.01);
// window/spend figures are USD.
type UsageData struct {
	PlanName       string
	Windows        []UsageWindow
	ExtraTotal     float64
	ExtraRemaining float64
	SpendTodayUSD  float64
}

type UsageWindow struct {
	Name            string
	BudgetUSD       float64
	CurrentSpentUSD float64
	ResetTime       string
	DurationSeconds int
}

// MCPServerInfo is the view model for one MCP server: its configuration plus
// the manager's live connection status.
type MCPServerInfo struct {
	Name      string
	Transport string // "stdio" | "http"
	Target    string // command+args, or url
	Enabled   bool
	OAuth     bool
	State     string // "connected" | "connecting" | "error" | "auth_required" | "disabled" | "authorized" | "not connected"
	Status    string // human-readable message
	ToolCount int
}

// MCPAddSpec describes a server to register.
type MCPAddSpec struct {
	Name      string
	Transport string
	Command   string
	Args      []string
	URL       string
	OAuth     bool
}

// MCPActions is the typed backend the MCP management UI drives. Each call is
// safe to invoke from a frontend's event goroutine via its own command wrapper.
type MCPActions struct {
	List       func(context.Context) ([]MCPServerInfo, error)
	Add        func(context.Context, MCPAddSpec) error
	Remove     func(context.Context, string) error
	SetEnabled func(context.Context, string, bool) error
	Authorize  func(context.Context, string) error
	Test       func(context.Context, string) ([]MCPServerInfo, error)
	// Refresh (D2) kicks a background, non-blocking manager refresh so opening
	// the management UI begins connecting lazy servers. Optional (may be nil).
	Refresh func(context.Context)
}

// Actions is the backend surface a frontend calls. internal/command implements
// it; frontends only consume it.
type Actions struct {
	SaveSettings  func(context.Context, *contract.Settings) error
	SetPermission func(context.Context, contract.PermissionMode) error
	Rewind        func(context.Context) (string, error)
	NewSession    func(context.Context) (Runtime, []contract.Event, error)
	ListSessions  func(context.Context) ([]contract.Session, error)
	Resume        func(context.Context, string) (Runtime, []contract.Event, error)
	SetAPIKey     func(context.Context, string) error
	Logout        func(context.Context) error
	// LoginViaBrowser runs the browser sign-in (loopback + PKCE) and returns the
	// signed-in email. It powers bare `/login` when a key is not pasted directly.
	LoginViaBrowser func(context.Context) (string, error)
	MCP             MCPActions
	ListSkills      func(context.Context) ([]Skill, error)
	// LoadSkill loads one skill's instruction body on demand (003 T036), so the
	// /skills modal opens without reading every skill file.
	LoadSkill func(context.Context, string) (string, error)
	// FetchUsage retrieves account usage from the gateway with the stored key
	// (003 US5). IsLoggedIn reports whether an API key is present, driving the
	// /login / /usage / /logout command visibility.
	FetchUsage func(context.Context) (*UsageData, error)
	IsLoggedIn func() bool
	// DiscoverModels re-queries the gateway's model catalog, merges it into
	// settings, and returns the models now available. No restart needed.
	DiscoverModels func(context.Context) ([]contract.Model, error)
	// TranscriptPage (005 US1 T020) loads a keyset page of the durable transcript
	// from SQLite for on-demand scroll-back. Nil ⇒ paging is disabled and the
	// frontend shows only the recent window it was given.
	TranscriptPage func(context.Context, contract.TranscriptPageRequest) (contract.TranscriptPage, error)
}

// HydratedRuntime is the result of the deferred runtime build: everything a
// frontend needs to switch from its loading shell to the live session.
type HydratedRuntime struct {
	Runtime Runtime
	Actions Actions
	Recent  []contract.Event
	Notice  string
	// LatestVersion is the newest published version, if the background update
	// check finished in time. Empty means unknown — show nothing.
	LatestVersion string
}

// AssemblePrompt turns what the user typed into the exact bytes the engine
// receives. Every frontend must call it: before it existed, the terminal UI
// did skill wrapping, paste expansion, and NFC normalization inline in its
// submit path while line mode did only NFC and one-shot did none, so the same
// input produced different prompts depending on how you launched.
//
// expandPastes reconstructs stashed paste blocks from their compact
// placeholders (nil when the frontend has no paste stash). loadSkill fetches a
// skill body on demand and may be nil.
func AssemblePrompt(ctx context.Context, typed string, skills []Skill, expandPastes func(string) string, loadSkill func(context.Context, string) (string, error)) string {
	prompt := typed
	if expandPastes != nil {
		prompt = expandPastes(prompt)
	}
	// The model receives normalized logical Unicode (006 T030/FR-013). NFC is
	// idempotent and meaning-preserving — it never introduces presentation forms
	// or reordering — and it rides the user message, so the cached prefix is
	// untouched. Digit systems are preserved as written.
	prompt = norm.NFC.String(prompt)
	if len(skills) == 0 {
		return prompt
	}
	ordered := append([]Skill(nil), skills...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	sections := make([]string, 0, len(ordered))
	for _, skill := range ordered {
		body := strings.TrimSpace(skill.Instructions)
		if body == "" && skill.Path != "" && loadSkill != nil {
			if loaded, err := loadSkill(ctx, skill.Path); err == nil {
				body = strings.TrimSpace(loaded)
			}
		}
		if body == "" {
			body = skill.Description
		}
		sections = append(sections, "<skill name=\""+skill.Name+"\">\n"+body+"\n</skill>")
	}
	return "Follow the selected skill instructions when relevant to this turn.\n\n" +
		strings.Join(sections, "\n\n") + "\n\nUser prompt:\n" + prompt
}
