package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/buildinfo"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
	"github.com/muhiya/muhiyacode/internal/state"
	"github.com/muhiya/muhiyacode/internal/tui"
	"github.com/muhiya/muhiyacode/internal/workspace"
	"github.com/spf13/cobra"
)

type sessionReconciliation struct {
	mutationID        string
	mutationCertainty string
	approvalID        string
	approvalDecision  string
}

func (request sessionReconciliation) requested() bool {
	return request.mutationID != "" || request.mutationCertainty != "" ||
		request.approvalID != "" || request.approvalDecision != ""
}

func doctorRepairSession(cmd *cobra.Command, reconciliation sessionReconciliation) error {
	paths, _, secrets, err := loadConfig()
	if err != nil {
		return err
	}
	request := sessionRepairRequest{
		paths: paths, secrets: secrets, workspace: inheritedWorkspace(cmd),
		reconciliation: reconciliation,
	}
	return repairSessionHistory(cmd.Context(), cmd.OutOrStdout(), request)
}

type sessionRepairRequest struct {
	paths          state.Paths
	secrets        contract.Secrets
	workspace      string
	reconciliation sessionReconciliation
}

func repairSessionHistory(ctx context.Context, out io.Writer, request sessionRepairRequest) error {
	target, err := openSessionRepairTarget(ctx, request)
	if err != nil {
		return err
	}
	defer target.db.Close()
	if err := applySessionReconciliation(ctx, out, target, request.reconciliation); err != nil {
		return err
	}
	messageCount, err := repairHistoryProjection(ctx, target)
	if err != nil {
		return err
	}
	usageCount, err := repairUsageProjection(ctx, target)
	if err != nil {
		return err
	}
	fmt.Fprintf(
		out,
		"repaired session %s projections from the execution journal (%d messages, %d usage records)\n",
		target.session.ID,
		messageCount,
		usageCount,
	)
	return nil
}

func applySessionReconciliation(
	ctx context.Context,
	out io.Writer,
	target sessionRepairTarget,
	request sessionReconciliation,
) error {
	if request.mutationID != "" {
		certainty, err := parseMutationCertainty(request.mutationCertainty)
		if err != nil {
			return err
		}
		if err := target.db.ReconcileMutation(ctx, target.session.ID, request.mutationID, certainty); err != nil {
			return err
		}
		fmt.Fprintf(out, "reconciled mutation %s as %s\n", request.mutationID, certainty)
	} else if request.mutationCertainty != "" {
		return fmt.Errorf("--mutation-certainty requires --ack-mutation")
	}
	if request.approvalID != "" {
		approved, err := parseApprovalDecision(request.approvalDecision)
		if err != nil {
			return err
		}
		if err := target.db.ReconcileApproval(ctx, target.session.ID, request.approvalID, approved); err != nil {
			return err
		}
		fmt.Fprintf(out, "reconciled approval %s as %s\n", request.approvalID, request.approvalDecision)
	} else if request.approvalDecision != "" {
		return fmt.Errorf("--approval-decision requires --ack-approval")
	}
	return nil
}

func parseMutationCertainty(raw string) (contract.MutationCertainty, error) {
	certainty := contract.MutationCertainty(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(raw)), "-", "_"))
	switch certainty {
	case contract.MutationCommitted, contract.MutationNotStarted, contract.MutationIndeterminate:
		return certainty, nil
	default:
		return "", fmt.Errorf("--mutation-certainty must be committed, not_started, or indeterminate")
	}
}

func parseApprovalDecision(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "approved":
		return true, nil
	case "denied":
		return false, nil
	default:
		return false, fmt.Errorf("--approval-decision must be approved or denied")
	}
}

func repairHistoryProjection(ctx context.Context, target sessionRepairTarget) (int, error) {
	var legacy orchestrator.HistorySnapshot
	legacyErr := target.sessions.ReadJSON(target.session.ID, "history.json", orchestrator.HistorySnapshot{Version: 1}, &legacy)
	app := &Application{db: target.db, sessions: target.sessions}
	recovered, err := app.loadJournalHistory(ctx, target.session, legacy, legacyErr)
	if err != nil {
		return 0, err
	}
	if err := target.sessions.WriteJSON(target.session.ID, "history.json", recovered); err != nil {
		return 0, fmt.Errorf("write repaired history projection: %w", err)
	}
	return len(recovered.Messages), nil
}

func repairUsageProjection(ctx context.Context, target sessionRepairTarget) (int, error) {
	legacy, legacyErr := target.sessions.UsageRecords(target.session.ID)
	app := &Application{db: target.db, sessions: target.sessions}
	recovered, err := app.loadJournalUsage(ctx, target.session, legacy, legacyErr)
	if err != nil {
		return 0, err
	}
	if err := target.sessions.WriteUsageRecords(target.session.ID, recovered); err != nil {
		return 0, fmt.Errorf("write repaired usage projection: %w", err)
	}
	return len(recovered), nil
}

type sessionRepairTarget struct {
	db       *state.DB
	sessions *state.Sessions
	session  contract.Session
}

func openSessionRepairTarget(ctx context.Context, request sessionRepairRequest) (sessionRepairTarget, error) {
	root, err := workspace.CanonicalPath(request.workspace)
	if err != nil {
		return sessionRepairTarget{}, err
	}
	db, err := state.Open(ctx, request.paths)
	if err != nil {
		return sessionRepairTarget{}, err
	}
	session, exists, err := db.LatestSession(ctx, root)
	if err != nil {
		db.Close()
		return sessionRepairTarget{}, err
	}
	if !exists {
		db.Close()
		return sessionRepairTarget{}, fmt.Errorf("no session exists for workspace %s", root)
	}
	sessions := &state.Sessions{DB: db, Secrets: request.secrets}
	return sessionRepairTarget{db: db, sessions: sessions, session: session}, nil
}

// This file adds the Stability Overhaul T005 diagnostics to `muhiyacode doctor`
// (defect D3): a self-identifying build line, launch-shadowing detection (the
// "same error keeps showing" root cause — the npm command runs a vendored exe a
// plain `go build` never touches), a masked API-key check, and workspace
// writability/git checks. The pure diagnoseShadow is unit-tested; the gatherers
// are thin OS I/O wrappers around it.

// printBuildDiagnostic prints the running binary's stamped identity and warns
// when it is an unstamped (plain `go build`) build.
func printBuildDiagnostic(out io.Writer) {
	stamped := buildinfo.Commit != "unknown" && buildinfo.Date != "unknown"
	status := "ok  "
	if !stamped {
		status = "warn"
	}
	fmt.Fprintf(out, "%s build: MuhiyaCode %s (commit %s, built %s)\n", status, buildinfo.Version, buildinfo.Commit, buildinfo.Date)
	if !stamped {
		fmt.Fprintln(out, "     unstamped build — official builds inject a commit/date via scripts/build.ps1")
	}
}

func printPendingMutationDiagnostic(ctx context.Context, out io.Writer, paths state.Paths, requestedWorkspace string) {
	if strings.TrimSpace(requestedWorkspace) == "" {
		requestedWorkspace, _ = os.Getwd()
	}
	root, err := workspace.CanonicalPath(requestedWorkspace)
	if err != nil {
		fmt.Fprintln(out, "warn execution journal:", err)
		return
	}
	db, err := state.Open(ctx, paths)
	if err != nil {
		fmt.Fprintln(out, "warn execution journal:", err)
		return
	}
	defer db.Close()
	session, exists, err := db.LatestSession(ctx, root)
	if err != nil {
		fmt.Fprintln(out, "warn execution journal:", err)
		return
	}
	if !exists {
		fmt.Fprintln(out, "ok  execution journal: no workspace session")
		return
	}
	health, err := db.ExecutionJournalHealth(ctx, session.ID)
	if err != nil {
		fmt.Fprintln(out, "warn execution journal:", err)
		return
	}
	if len(health.PendingMutations) == 0 && len(health.IncompleteTasks) == 0 &&
		len(health.PendingApprovals) == 0 && len(health.IncompleteTools) == 0 {
		fmt.Fprintln(out, "ok  execution journal: no incomplete tasks, tools, approvals, or mutation intents")
		return
	}
	if len(health.IncompleteTasks) > 0 {
		fmt.Fprintf(out, "warn execution journal: %d task(s) started without a durable terminal record\n", len(health.IncompleteTasks))
		for _, taskID := range health.IncompleteTasks {
			fmt.Fprintf(out, "     task %s\n", taskID)
		}
	}
	if len(health.IncompleteTools) > 0 {
		fmt.Fprintf(out, "warn execution journal: %d tool dispatch(es) have no durable terminal record\n", len(health.IncompleteTools))
		for _, started := range health.IncompleteTools {
			fmt.Fprintf(out, "     %s %s (execution %s)\n", started.ToolName, started.Target, started.ExecutionID)
		}
	}
	if len(health.PendingMutations) > 0 {
		fmt.Fprintf(out, "warn execution journal: %d mutation intent(s) have no durable outcome; inspect targets before retrying\n", len(health.PendingMutations))
		for _, intent := range health.PendingMutations {
			fmt.Fprintf(out, "     %s %s (intent %s)\n", intent.ToolName, intent.Target, intent.IntentID)
		}
	}
	if len(health.PendingApprovals) > 0 {
		fmt.Fprintf(out, "warn execution journal: %d approval request(s) have no durable resolution\n", len(health.PendingApprovals))
		for _, request := range health.PendingApprovals {
			fmt.Fprintf(out, "     %s %s (approval %s)\n", request.Action, request.Target, request.RequestID)
		}
	}
}

// shadowInputs is the pure snapshot diagnoseShadow reasons over. Every field is
// gathered by gatherShadowInputs from the real environment; tests construct it
// directly so no OS calls are needed to test the logic.
type shadowInputs struct {
	resolved      string // where `muhiyacode` resolves on PATH ("" if not found)
	isNpmShim     bool   // the resolved command is the npm launcher shim
	vendorExe     string // vendored exe path the npm shim runs ("" if n/a)
	vendorExists  bool
	vendorSize    int64
	vendorMod     string // formatted vendor exe mtime
	envBinary     string // MUHIYACODE_BINARY override
	thisCommit    string // buildinfo.Commit of the running process
	vendorVersion string // first line of `<vendorExe> --version` ("" if not run)
}

// diagnoseShadow turns a shadowInputs snapshot into ok/warn diagnostic lines. Its
// crux: does the binary the `muhiyacode` command would actually run match THIS
// build? A mismatch is the stale-vendored-exe defect (D3).
func diagnoseShadow(in shadowInputs) []string {
	var lines []string
	if in.envBinary != "" {
		lines = append(lines, "ok   launch: MUHIYACODE_BINARY override set → "+in.envBinary)
	}
	if in.resolved == "" {
		lines = append(lines, "warn launch: `muhiyacode` is not on PATH (you are running this binary directly)")
		return lines
	}
	lines = append(lines, "ok   launch: `muhiyacode` resolves to "+in.resolved)
	if in.isNpmShim {
		if in.vendorExists {
			lines = append(lines, fmt.Sprintf("ok   launch: npm shim runs vendored exe (%d bytes, %s)", in.vendorSize, in.vendorMod))
		} else {
			lines = append(lines, "warn launch: npm shim present but its vendored exe is missing — reinstall: npm install -g muhiyacode")
			return lines
		}
	}
	if in.vendorVersion != "" && in.thisCommit != "" && in.thisCommit != "unknown" {
		if strings.Contains(in.vendorVersion, in.thisCommit) {
			lines = append(lines, "ok   launch: the `muhiyacode` command matches THIS build ("+in.thisCommit+")")
		} else {
			lines = append(lines, "warn launch: the `muhiyacode` command runs a DIFFERENT build than this one —")
			lines = append(lines, "     this build:  commit "+in.thisCommit)
			lines = append(lines, "     muhiyacode:  "+in.vendorVersion)
			lines = append(lines, "     → run scripts/swap-global.ps1, then restart your terminal")
		}
	}
	return lines
}

// printShadowDiagnostic gathers real environment data and prints the lines.
func printShadowDiagnostic(out io.Writer) {
	for _, line := range diagnoseShadow(gatherShadowInputs()) {
		fmt.Fprintln(out, line)
	}
}

func gatherShadowInputs() shadowInputs {
	in := shadowInputs{thisCommit: buildinfo.Commit, envBinary: strings.TrimSpace(os.Getenv("MUHIYACODE_BINARY"))}
	if resolved, err := exec.LookPath("muhiyacode"); err == nil {
		in.resolved = resolved
	}
	lower := strings.ToLower(in.resolved)
	if strings.Contains(lower, "npm") || strings.Contains(lower, "node_modules") {
		in.isNpmShim = true
		if appData := os.Getenv("APPDATA"); appData != "" {
			in.vendorExe = filepath.Join(appData, "npm", "node_modules", "muhiyacode", "vendor", "muhiyacode.exe")
			if info, err := os.Stat(in.vendorExe); err == nil {
				in.vendorExists = true
				in.vendorSize = info.Size()
				in.vendorMod = info.ModTime().Format("2006-01-02 15:04")
			}
		}
	}
	// The parity check runs the VENDORED exe directly (clean .exe spawn) rather
	// than the shim (which would trampoline through node) — it is the binary the
	// command ultimately executes.
	if in.vendorExists {
		in.vendorVersion = firstVersionLine(in.vendorExe)
	}
	return in
}

// firstVersionLine runs `<exe> --version` with a short timeout and returns its
// first output line, or "" on any failure.
func firstVersionLine(exe string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0])
}

// maskAPIKey renders a key as ····<last4>, never revealing the whole value.
func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "(none — run /login or muhiyacode config set apiKey <key>)"
	}
	if len(key) <= 4 {
		return "····"
	}
	return "····" + key[len(key)-4:]
}

// workspaceDiagnostics reports whether the working directory is writable and
// whether it (or an ancestor) is a git repository. dir "" means the current cwd.
func workspaceDiagnostics(dir string) []string {
	var lines []string
	if strings.TrimSpace(dir) == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	probe := filepath.Join(dir, ".muhiyacode-doctor-write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		lines = append(lines, "warn workspace: NOT writable — "+dir)
	} else {
		_ = os.Remove(probe)
		lines = append(lines, "ok   workspace: writable — "+dir)
	}
	if hasGitRepo(dir) {
		lines = append(lines, "ok   workspace: inside a git repository (edits are also /undo-able in-session)")
	} else {
		lines = append(lines, "ok   workspace: not a git repository (edits are still /undo-able in-session)")
	}
	return lines
}

// hasGitRepo reports whether dir or any ancestor holds a .git entry.
func hasGitRepo(dir string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func newDoctorCommand() *cobra.Command {
	var offline bool
	var reconciliation sessionReconciliation
	command := &cobra.Command{
		Use:   "doctor [rtl|repair-session]",
		Short: "Validate local setup and endpoint access",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repairRequested := len(args) == 1 && strings.EqualFold(args[0], "repair-session")
			if reconciliation.requested() && !repairRequested {
				return errors.New("reconciliation flags require `doctor repair-session`")
			}
			if len(args) == 1 {
				switch {
				case strings.EqualFold(args[0], "rtl"):
					return doctorRTL(cmd)
				case strings.EqualFold(args[0], "repair-session"):
					return doctorRepairSession(cmd, reconciliation)
				}
			}
			return doctor(cmd, offline)
		},
	}
	command.Flags().BoolVar(&offline, "offline", false, "skip endpoint and tool-call diagnostics")
	command.Flags().StringVar(&reconciliation.mutationID, "ack-mutation", "", "acknowledge a pending mutation intent by ID")
	command.Flags().StringVar(&reconciliation.mutationCertainty, "mutation-certainty", "", "committed, not_started, or indeterminate")
	command.Flags().StringVar(&reconciliation.approvalID, "ack-approval", "", "acknowledge a pending approval request by ID")
	command.Flags().StringVar(&reconciliation.approvalDecision, "approval-decision", "", "approved or denied")
	return command
}

func doctor(cmd *cobra.Command, offline bool) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "MuhiyaCode diagnostics")
	printBuildDiagnostic(out)
	printShadowDiagnostic(out)
	for _, line := range workspaceDiagnostics(inheritedWorkspace(cmd)) {
		fmt.Fprintln(out, line)
	}
	paths, settings, secrets, err := loadConfig()
	if err != nil {
		fmt.Fprintln(out, "fail config:", err)
		return err
	}
	fmt.Fprintln(out, "ok  home:", paths.Home)
	fmt.Fprintln(out, "ok  apiKey:", maskAPIKey(secrets.ProviderAPIKey))
	printPendingMutationDiagnostic(cmd.Context(), out, paths, inheritedWorkspace(cmd))
	shell, shellErr := workspace.ChooseShell(settings.Shell.Preferred)
	if shellErr != nil {
		fmt.Fprintln(out, "fail shell:", shellErr)
	} else {
		fmt.Fprintln(out, "ok  shell:", shell)
	}
	active, activeOK := state.ActiveModel(settings)
	if secrets.ProviderAPIKey == "" || !activeOK || active.ContextLimit <= 0 || settings.Provider.BaseURL == "" {
		message := "provider config incomplete; set baseUrl, apiKey, model, and contextLimit"
		fmt.Fprintln(out, "fail provider:", message)
		if shellErr != nil {
			return errors.Join(shellErr, errors.New(message))
		}
		return errors.New(message)
	}
	fmt.Fprintf(out, "ok  provider: %s (%s, %d context)\n", settings.Provider.BaseURL, active.ID, active.ContextLimit)
	if offline {
		return shellErr
	}
	provider := gateway.NewOpenAICompatible(gateway.Config{Settings: settings, APIKey: secrets.ProviderAPIKey, MaxRetries: 1, RequestLifetime: 45 * time.Second})
	response, endpointErr := provider.Chat(cmd.Context(), contract.ChatRequest{
		ModelID: active.ID, Reasoning: contract.ReasoningLow, MaxTokens: 300,
		Messages: []contract.Message{{Role: contract.RoleSystem, Content: "Call diagnostics_ping with value ok."}, {Role: contract.RoleUser, Content: "Run the diagnostic tool now."}},
		Tools:    []contract.ToolDefinition{{Type: "function", Function: contract.FunctionDefinition{Name: "diagnostics_ping", Description: "Return a diagnostic ping.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []string{"value"}, "additionalProperties": false}}}},
	})
	if endpointErr != nil {
		fmt.Fprintln(out, "fail endpoint:", endpointErr)
		return errors.Join(shellErr, endpointErr)
	}
	for _, call := range response.ToolCalls {
		if call.ToolName() == "diagnostics_ping" {
			fmt.Fprintln(out, "ok  endpoint: auth, streaming, and structured tool calls work")
			return shellErr
		}
	}
	fmt.Fprintln(out, "fail endpoint: response streamed but omitted diagnostics_ping")
	return errors.Join(shellErr, errors.New("endpoint did not return a structured tool call"))
}

func doctorRTL(cmd *cobra.Command) error {
	_, settings, _, err := loadConfig()
	if err != nil {
		return err
	}
	sample := "مرحباً، هل تستطيع تنفيذ مشاريعي وطلباتي البرمجية؟"
	mixed := `راجع workspace: F:\MuhiyaCode Agent\dist ثم نفّذ الاختبارات.`
	response := "نعم، أستطيع مساعدتك في تنفيذ المشاريع البرمجية المتاحة."
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "MuhiyaCode RTL diagnostics\nmode: %s  align: %s\n", settings.RTL.Mode, settings.RTL.Align)
	fmt.Fprintf(out, "terminal BiDi control emitted: %q\n\n", tui.TerminalBiDiControl(settings.RTL.Mode))
	fmt.Fprintln(out, "Native Arabic (logical order — what the model receives):")
	fmt.Fprintln(out, sample)
	fmt.Fprintln(out, response)
	fmt.Fprintln(out, "\nMixed Arabic and path:")
	fmt.Fprintln(out, tui.RenderRTL(sample, "visual"))
	fmt.Fprintln(out, tui.RenderRTL(response, "visual"))
	fmt.Fprintln(out, "\nMixed Arabic and path:")
	fmt.Fprintln(out, tui.RenderRTL(mixed, settings.RTL.Mode))
	if _, ok := tui.CopyRoundTrip(sample, "visual"); ok {
		fmt.Fprintln(out, "\ncopy round-trip: OK (selecting the shaped text yields the original logical text)")
	} else {
		fmt.Fprintln(out, "\ncopy round-trip: DEGRADED (mixed-direction line; whole-message copy is still exact)")
	}
	return nil
}
