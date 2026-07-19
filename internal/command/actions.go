package command

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/state"
	"github.com/muhiya/muhiyacode/internal/tui"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

func (a *Application) Actions() tui.Actions {
	return tui.Actions{
		SaveSettings: func(_ context.Context, settings *contract.Settings) error {
			if settings == nil {
				return errors.New("settings are nil")
			}
			if err := state.SaveSettings(*settings, a.paths); err != nil {
				return err
			}
			*a.settings = *settings
			a.provider.UpdateConfig(*a.settings, a.secrets.ProviderAPIKey)
			return nil
		},
		SetModel: func(ctx context.Context, role, id string) error { return a.setModel(ctx, role, id) },
		SetPermission: func(_ context.Context, mode contract.PermissionMode) error {
			a.mu.Lock()
			active := a.activeWorkspace
			a.mu.Unlock()
			if active != nil {
				if err := active.SetPermissionMode(mode); err != nil {
					return err
				}
			}
			return state.SaveSettings(*a.settings, a.paths)
		},
		Rewind: func(ctx context.Context) (string, error) {
			a.mu.Lock()
			checkpoint := a.activeCheckpoint
			a.mu.Unlock()
			if checkpoint == nil {
				return "No checkpoint available.", nil
			}
			return checkpoint.RestoreLatest(ctx)
		},
		NewSession: func(ctx context.Context) (tui.Runtime, []contract.Event, error) {
			a.mu.Lock()
			root := a.runtime.Session.WorkspacePath
			a.mu.Unlock()
			session, err := a.sessions.New(ctx, root, "Workspace session")
			if err != nil {
				return tui.Runtime{}, nil, err
			}
			return a.switchSession(ctx, session)
		},
		ListSessions: func(ctx context.Context) ([]contract.Session, error) {
			a.mu.Lock()
			root := a.runtime.Session.WorkspacePath
			a.mu.Unlock()
			return a.db.ListSessions(ctx, root, 100)
		},
		Resume: func(ctx context.Context, id string) (tui.Runtime, []contract.Event, error) {
			session, ok, err := a.db.Session(ctx, id)
			if err != nil {
				return tui.Runtime{}, nil, err
			}
			if !ok {
				return tui.Runtime{}, nil, fmt.Errorf("session not found: %s", id)
			}
			return a.switchSession(ctx, session)
		},
		SetAPIKey: func(ctx context.Context, key string) error { return a.setAPIKey(ctx, key) },
		Logout:    func(_ context.Context) error { return a.setAPIKey(context.Background(), "") },
		LoginViaBrowser: func(ctx context.Context) (string, error) {
			// Surface the authorization URL in the TUI transcript as a fallback for
			// when the browser cannot be opened automatically (headless/SSH), so the
			// user is not left staring at a silent 5-minute wait.
			onURL := func(u string) {
				if a.callbacks.Notice != nil {
					a.callbacks.Notice("If your browser did not open, visit:\n" + u)
				}
			}
			email, token, err := performBrowserLogin(ctx, "", onURL)
			if err != nil {
				return "", err
			}
			// Sync the freshly minted key into the live app state (in-memory
			// secret, provider client, model discovery) and persist it.
			if err := a.setAPIKey(ctx, token); err != nil {
				return "", err
			}
			return email, nil
		},
		MCP:        a.mcpActions(),
		ListSkills: func(_ context.Context) ([]tui.Skill, error) { return a.listSkills() },
		LoadSkill:  func(_ context.Context, path string) (string, error) { return a.loadSkillBody(path) },
		FetchUsage: func(ctx context.Context) (*tui.UsageData, error) { return a.fetchUsage(ctx) },
		IsLoggedIn: func() bool {
			a.mu.Lock()
			defer a.mu.Unlock()
			return strings.TrimSpace(a.secrets.ProviderAPIKey) != ""
		},
		DiscoverModels: func(ctx context.Context) ([]contract.Model, error) {
			discoveryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			models, err := a.provider.ListModels(discoveryCtx)
			if err != nil {
				return nil, err
			}
			stranded := addDiscoveredModels(a.settings, models)
			if err := state.SaveSettings(*a.settings, a.paths); err != nil {
				return nil, err
			}
			a.provider.UpdateConfig(*a.settings, a.secrets.ProviderAPIKey)
			if len(stranded) > 0 && a.callbacks.Notice != nil {
				a.callbacks.Notice("No longer offered by the gateway (still selected — pick a new one with /model): " + strings.Join(stranded, ", "))
			}
			return a.settings.Provider.Models, nil
		},
		// TranscriptPage (005 US1 T020) serves keyset pages of the durable transcript
		// for scroll-back. The session id is forced from the active session so a page
		// request can never read another session's transcript.
		TranscriptPage: func(ctx context.Context, req contract.TranscriptPageRequest) (contract.TranscriptPage, error) {
			req.SessionID = a.currentSessionID()
			return a.db.TranscriptPage(ctx, req)
		},
	}
}

// fetchUsage retrieves account usage from the gateway with the stored key and
// maps it into the TUI's UsageData (003 US5). Errors carry a friendly message.
func (a *Application) fetchUsage(ctx context.Context) (*tui.UsageData, error) {
	a.mu.Lock()
	settings := *a.settings
	key := a.secrets.ProviderAPIKey
	a.mu.Unlock()
	resp, err := gateway.FetchUsage(ctx, settings, key)
	if err != nil {
		return nil, err
	}
	data := &tui.UsageData{
		PlanName:       resp.Plan.Name,
		ExtraTotal:     resp.Credits.ExtraTotal,
		ExtraRemaining: resp.Credits.ExtraRemaining,
		SpendTodayUSD:  resp.Spend.TodayUSD,
	}
	for _, w := range resp.Plan.Windows {
		data.Windows = append(data.Windows, tui.UsageWindow{
			Name: w.Name, BudgetUSD: w.BudgetUSD, CurrentSpentUSD: w.CurrentSpentUSD,
			ResetTime: w.ResetTime, DurationSeconds: w.DurationSeconds,
		})
	}
	return data, nil
}

func approvalMessage(request workspace.ApprovalRequest) string {
	if request.TrustWorkspace {
		return "Trust this workspace for mutations?\n" + request.Workspace
	}
	if request.Command != "" {
		return "Allow shell command in " + request.Workspace + "?\n\n" + request.Command
	}
	label := strings.Title(string(request.Action))
	if request.OutsideWorkspace {
		label += " outside the workspace"
	}
	return fmt.Sprintf("%s?\n%s", label, request.Path)
}

var planLine = regexp.MustCompile(`^- \[([ xX])\] (.+) \((pending|in_progress|completed)\)$`)

func parsePlan(content string) contract.Plan {
	var plan contract.Plan
	var notes []string
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		match := planLine.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) == 4 {
			plan.Steps = append(plan.Steps, contract.PlanStep{Title: match[2], Status: contract.PlanStatus(match[3])})
		} else if strings.TrimSpace(line) != "" && line != "No task has been planned yet." {
			notes = append(notes, line)
		}
	}
	if len(plan.Steps) > 0 {
		plan.Note = strings.Join(notes, "\n")
		plan.UpdatedAt = time.Now().UTC()
	}
	return plan
}

// addDiscoveredModels merges a fresh gateway model list into settings: it upserts
// every returned model, prunes previously endpoint-discovered models the gateway
// no longer offers, and (re)assigns the default main/subagent roles. It returns
// the ids of any still-selected models that vanished from the catalog so the
// caller can advise switching.
func addDiscoveredModels(settings *contract.Settings, models []contract.Model) []string {
	sort.SliceStable(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	for _, model := range models {
		if model.ContextLimit <= 0 {
			model.ContextLimit = gateway.ResolveModelProfile(model.ID + " " + model.Name).DefaultContextWindow
		}
		state.UpsertModel(settings, model, false)
	}
	stranded := pruneStaleDiscoveredModels(settings, models)
	if !state.AssignFreshDefaultModels(settings) {
		state.AutoAssignModels(settings)
	}
	return stranded
}

// pruneStaleDiscoveredModels removes previously endpoint-discovered models the
// gateway no longer offers (e.g. after an operator unchecks "MuhiyaCode
// Discoverable"), so a hidden model does not linger in the picker forever. The
// currently-selected main and subagent models are always kept even when newly
// absent, so a refresh can never strand the running session; their ids are
// returned so the caller can advise switching. Manually-added models
// (Source != "endpoint") are never pruned. It is a no-op when fresh is empty: an
// empty successful fetch is treated as "unknown", never as "unpublish everything".
func pruneStaleDiscoveredModels(settings *contract.Settings, fresh []contract.Model) []string {
	if settings == nil || len(fresh) == 0 {
		return nil
	}
	freshIDs := make(map[string]bool, len(fresh))
	for _, model := range fresh {
		freshIDs[model.ID] = true
	}
	active := settings.Provider.ActiveModelID
	subagent := settings.Provider.SubagentModelID
	kept := settings.Provider.Models[:0]
	var stranded []string
	for _, model := range settings.Provider.Models {
		stale := model.Source == "endpoint" && !freshIDs[model.ID]
		if stale && model.ID != active && model.ID != subagent {
			continue // drop a model the gateway no longer lists
		}
		if stale {
			stranded = append(stranded, model.ID)
		}
		kept = append(kept, model)
	}
	settings.Provider.Models = kept
	return stranded
}
