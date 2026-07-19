// Package command defines MuhiyaCode's CLI surface and process composition.
package command

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/buildinfo"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/mcpclient"
	"github.com/muhiya/muhiyacode/internal/state"
	"github.com/muhiya/muhiyacode/internal/tui"
	"github.com/muhiya/muhiyacode/internal/workspace"
	"github.com/spf13/cobra"
)

func Execute(ctx context.Context, args []string) error {
	command := NewRootCommand()
	command.SetArgs(args)
	return command.ExecuteContext(ctx)
}

func NewRootCommand() *cobra.Command {
	var printPrompt, cwd string
	var simple, fresh, noMCP bool
	root := &cobra.Command{
		Use:           "muhiyacode [prompt...]",
		Short:         "MuhiyaCode terminal coding agent",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		Version:       buildinfo.Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.TrimSpace(strings.Join(args, " "))
			if printPrompt != "" {
				return runOneShot(cmd, cwd, printPrompt, fresh, noMCP)
			}
			return runInteractive(cmd, interactiveOptions{Workspace: cwd, Prompt: prompt, Fresh: fresh, NoMCP: noMCP, Simple: simple})
		},
	}
	// Commit/Date are set by the release build's ldflags (.goreleaser.yaml);
	// source builds show "unknown" for both, same as buildinfo.Version's default
	// (feature 010 T037: these were ldflags-injected but had no reader anywhere —
	// wiring them into --version is the fix, since the alternative was deleting
	// working release metadata for no reason).
	root.SetVersionTemplate(fmt.Sprintf("MuhiyaCode {{.Version}} (commit %s, built %s)\n", buildinfo.Commit, buildinfo.Date))
	root.PersistentFlags().StringVarP(&cwd, "cwd", "C", "", "workspace directory")
	root.Flags().StringVarP(&printPrompt, "print", "p", "", "run one-shot prompt and exit")
	root.Flags().BoolVar(&simple, "simple", false, "use the line interface instead of the full TUI")
	root.Flags().BoolVar(&fresh, "new", false, "start a new session instead of reopening the latest workspace session")
	root.Flags().BoolVar(&noMCP, "no-mcp", false, "start without connecting MCP servers")
	root.AddCommand(newLoginCommand(), newLogoutCommand(), newResumeCommand(), newSessionsCommand(), newConfigCommand(), newMCPCommand(), newDoctorCommand())
	return root
}

type interactiveOptions struct {
	Workspace, SessionID, Prompt string
	Fresh, NoMCP, Simple         bool
}

func runInteractive(cmd *cobra.Command, options interactiveOptions) error {
	bridge := tui.NewBridge()
	// T021: open the fast core (config/DB/session) now so the composer can appear
	// immediately; build the runtime in the background via the Hydrate closure.
	app, err := openApplicationCore(ApplicationOptions{
		Context: cmd.Context(), Workspace: options.Workspace, SessionID: options.SessionID,
		NewSession: options.Fresh, Title: promptTitle(options.Prompt), Callbacks: bridge.Callbacks(), DisableMCP: options.NoMCP,
	})
	if err != nil {
		return err
	}
	defer app.Close()
	hydrate := func(ctx context.Context) (tui.HydratedRuntime, error) {
		if err := app.Hydrate(ctx); err != nil {
			return tui.HydratedRuntime{}, err
		}
		// One-shot startup notices are read from the now-live engine: config gaps,
		// plus any restored goal/plan-state resurrected from the sidecars (G4/P2).
		notice := configurationNotice(*app.Settings(), app.secrets)
		if restored := app.Runtime().Engine.RestoredGoalNotice(); restored != "" {
			notice = joinNotice(notice, restored)
		}
		if restored := app.Runtime().Engine.RestoredPlanNotice(); restored != "" {
			notice = joinNotice(notice, restored)
		}
		return tui.HydratedRuntime{
			Runtime: app.Runtime(), Actions: app.Actions(), Recent: app.Recent(), Notice: notice,
		}, nil
	}
	return tui.Run(tui.Options{
		// A partial runtime (settings + session, no engine) lets the loading shell
		// render the real header/title immediately; Hydrate fills in the engine.
		Runtime: tui.Runtime{Settings: app.settings, Session: app.session},
		Bridge:  bridge, Version: buildinfo.Version,
		InitialPrompt: options.Prompt, Context: cmd.Context(), Simple: options.Simple,
		Hydrate: hydrate,
	})
}

// joinNotice appends add to base on its own line, tolerating an empty base.
func joinNotice(base, add string) string {
	if base == "" {
		return add
	}
	return base + "\n" + add
}

func runOneShot(cmd *cobra.Command, cwd, prompt string, fresh, noMCP bool) error {
	callbacks := newConsoleCallbacks(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	app, err := OpenApplication(ApplicationOptions{Context: cmd.Context(), Workspace: cwd, NewSession: fresh, Title: promptTitle(prompt), Callbacks: callbacks, DisableMCP: noMCP})
	if err != nil {
		return err
	}
	defer app.Close()
	if notice := configurationNotice(*app.Settings(), app.secrets); notice != "" {
		return errors.New(notice)
	}
	answer, stats, err := app.Runtime().Engine.Run(cmd.Context(), prompt)
	if err != nil {
		// Feature 011 T004a: the benchmark runner needs a summary even for a
		// failed run (recorded as completed:false), before the error propagates.
		if os.Getenv("MUHIYA_BENCH_JSON") == "1" {
			emitBenchSummary(cmd.OutOrStdout(), stats, err)
		}
		return err
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSpace(answer)); err != nil {
		return err
	}
	if os.Getenv("MUHIYA_BENCH_JSON") == "1" {
		emitBenchSummary(cmd.OutOrStdout(), stats, nil)
	}
	return nil
}

func newResumeCommand() *cobra.Command {
	var simple, noMCP bool
	command := &cobra.Command{
		Use:   "resume <session-id>",
		Short: "Resume a stored session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInteractive(cmd, interactiveOptions{SessionID: args[0], NoMCP: noMCP, Simple: simple})
		},
	}
	command.Flags().BoolVar(&simple, "simple", false, "use the line interface")
	command.Flags().BoolVar(&noMCP, "no-mcp", false, "do not connect MCP servers")
	return command
}

func newSessionsCommand() *cobra.Command {
	var all bool
	command := &cobra.Command{
		Use:   "sessions",
		Short: "List stored sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := state.EnsurePaths()
			if err != nil {
				return err
			}
			db, err := state.Open(cmd.Context(), paths)
			if err != nil {
				return err
			}
			defer db.Close()
			filter := ""
			if !all {
				filter = inheritedWorkspace(cmd)
				if filter != "" {
					filter, err = workspace.CanonicalPath(filter)
				} else {
					filter, err = os.Getwd()
					if err == nil {
						filter, err = workspace.CanonicalPath(filter)
					}
				}
				if err != nil {
					return err
				}
			}
			sessions, err := db.ListSessions(cmd.Context(), filter, 100)
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "No sessions found.")
				return err
			}
			for _, session := range sessions {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  %s\n", session.ID, session.UpdatedAt.Local().Format(time.RFC3339), session.Title, session.WorkspacePath)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&all, "all", false, "list sessions from every workspace")
	return command
}

func newConfigCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Show or set configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, settings, secrets, err := loadConfig()
			if err != nil {
				return err
			}
			payload, _ := json.MarshalIndent(state.RedactedSettings(settings, secrets), "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(payload))
			fmt.Fprintf(cmd.OutOrStdout(), "settings: %s\nsecrets:  %s\n", paths.SettingsFile, paths.SecretsFile)
			return nil
		},
	}
	set := &cobra.Command{
		Use:   "set <key> <value> [model]",
		Short: "Set a configuration value",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, settings, secrets, err := loadConfig()
			if err != nil {
				return err
			}
			label := args[0]
			if strings.HasPrefix(strings.ToLower(args[0]), "http://") || strings.HasPrefix(strings.ToLower(args[0]), "https://") {
				if err := state.SetConfig("baseUrl", args[0], &settings, &secrets); err != nil {
					return err
				}
				if err := state.SetConfig("apiKey", args[1], &settings, &secrets); err != nil {
					return err
				}
				label = "baseUrl and apiKey"
				if len(args) == 3 {
					if err := state.SetConfig("model", args[2], &settings, &secrets); err != nil {
						return err
					}
					label += " and model"
				}
			} else if err := state.SetConfig(args[0], args[1], &settings, &secrets); err != nil {
				return err
			}
			if err := state.SaveSettings(settings, paths); err != nil {
				return err
			}
			if err := state.SaveSecrets(secrets, paths); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Saved "+label+".")
			return nil
		},
	}
	discover := &cobra.Command{
		Use:   "discover",
		Short: "Discover models from the configured endpoint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, settings, secrets, err := loadConfig()
			if err != nil {
				return err
			}
			provider := gateway.NewOpenAICompatible(gateway.Config{Settings: settings, APIKey: secrets.ProviderAPIKey})
			models, err := provider.ListModels(cmd.Context())
			if err != nil {
				return err
			}
			stranded := addDiscoveredModels(&settings, models)
			if err := state.SaveSettings(settings, paths); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Discovered %d model(s). Main: %s; subagent: %s.\n", len(models), settings.Provider.ActiveModelID, settings.Provider.SubagentModelID)
			if len(stranded) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No longer offered (still selected): %s. Pick a new one with `muhiyacode config set model <id>`.\n", strings.Join(stranded, ", "))
			}
			return nil
		},
	}
	path := &cobra.Command{Use: "path", Short: "Print the settings path", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		paths, err := state.EnsurePaths()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), paths.SettingsFile)
		return err
	}}
	command.AddCommand(set, discover, path)
	return command
}

func newMCPCommand() *cobra.Command {
	command := &cobra.Command{Use: "mcp", Short: "Manage MCP servers", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		paths, err := state.EnsurePaths()
		if err != nil {
			return err
		}
		value, err := formatMCPList(paths)
		if err == nil {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), value)
		}
		return err
	}}
	list := &cobra.Command{Use: "list", Short: "List registered servers", Args: cobra.NoArgs, RunE: command.RunE}
	var stdioCWD string
	var stdioEnv []string
	addStdio := &cobra.Command{
		Use:   "add-stdio <name> <command> [args...]",
		Short: "Register a stdio MCP server",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := state.EnsurePaths()
			if err != nil {
				return err
			}
			name := state.SanitizeMCPName(args[0])
			if name == "" {
				return errors.New("MCP server name is invalid")
			}
			if err := state.UpsertMCPServer(state.MCPServer{Name: name, Enabled: true, TimeoutMS: 30_000, Transport: "stdio", Command: args[1], Args: args[2:], CWD: stdioCWD}, paths); err != nil {
				return err
			}
			values, err := parseKeyValues(stdioEnv)
			if err != nil {
				return err
			}
			if len(values) > 0 {
				if err := state.SetMCPEnv(name, values, paths); err != nil {
					return err
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Registered MCP stdio server "+name+".")
			return nil
		},
	}
	addStdio.Flags().StringVar(&stdioCWD, "cwd", "", "working directory for the server")
	addStdio.Flags().StringArrayVar(&stdioEnv, "env", nil, "secret environment value KEY=VALUE (repeatable)")
	var oauth bool
	var scope string
	var port, httpTimeout int
	addHTTP := &cobra.Command{
		Use:   "add-http <name> <url>",
		Short: "Register a Streamable HTTP MCP server",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if port < 1024 || port > 65535 {
				return fmt.Errorf("--port must be between 1024 and 65535")
			}
			if httpTimeout <= 0 || httpTimeout > 600_000 {
				return fmt.Errorf("--timeout-ms must be between 1 and 600000")
			}
			paths, err := state.EnsurePaths()
			if err != nil {
				return err
			}
			name := state.SanitizeMCPName(args[0])
			if name == "" {
				return errors.New("MCP server name is invalid")
			}
			server := state.MCPServer{Name: name, Enabled: true, TimeoutMS: httpTimeout, Transport: "http", URL: args[1], OAuth: &state.MCPOAuth{Enabled: oauth, Scope: scope, RedirectPort: port}}
			if err := state.UpsertMCPServer(server, paths); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Registered MCP HTTP server "+name+".")
			return nil
		},
	}
	addHTTP.Flags().BoolVar(&oauth, "oauth", false, "enable OAuth authorization")
	addHTTP.Flags().StringVar(&scope, "scope", "", "OAuth scope")
	addHTTP.Flags().IntVar(&port, "port", 35698, "local OAuth callback port")
	addHTTP.Flags().IntVar(&httpTimeout, "timeout-ms", 30_000, "server operation timeout")
	remove := &cobra.Command{Use: "remove <name>", Short: "Remove a registered server", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		paths, err := state.EnsurePaths()
		if err != nil {
			return err
		}
		removed, err := state.RemoveMCPServer(args[0], paths)
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("MCP server not found: %s", args[0])
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Removed MCP server "+args[0]+".")
		return nil
	}}
	auth := &cobra.Command{Use: "auth <name>", Short: "Authorize an OAuth-enabled HTTP server", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		paths, err := state.EnsurePaths()
		if err != nil {
			return err
		}
		manager := mcpclient.New(paths, nil, nil, nil)
		defer manager.Close()
		return manager.Authorize(cmd.Context(), args[0], mcpclient.AuthorizeOptions{OnURL: func(url string) {
			fmt.Fprintln(cmd.OutOrStdout(), "Open this URL to authorize MCP:\n"+url)
		}})
	}}
	command.AddCommand(list, addStdio, addHTTP, remove, auth)
	return command
}

func newDoctorCommand() *cobra.Command {
	var offline bool
	command := &cobra.Command{
		Use:   "doctor [rtl]",
		Short: "Validate local setup and endpoint access",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && strings.EqualFold(args[0], "rtl") {
				return doctorRTL(cmd)
			}
			return doctor(cmd, offline)
		},
	}
	command.Flags().BoolVar(&offline, "offline", false, "skip endpoint and tool-call diagnostics")
	return command
}

func doctor(cmd *cobra.Command, offline bool) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "MuhiyaCode diagnostics")
	// T005: the always-useful checks print FIRST, before any config load can fail
	// out — a misconfigured user is exactly who runs `doctor`, and they still need
	// the build identity, launch-shadow warning, and workspace state.
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
	fmt.Fprintln(out, "\nVisual fallback (shaped + reordered — what the app draws):")
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

func loadConfig() (state.Paths, contract.Settings, contract.Secrets, error) {
	paths, err := state.EnsurePaths()
	if err != nil {
		return state.Paths{}, contract.Settings{}, contract.Secrets{}, err
	}
	settings, err := state.LoadSettings(paths)
	if err != nil {
		return paths, contract.Settings{}, contract.Secrets{}, err
	}
	secrets, err := state.LoadSecrets(paths)
	return paths, settings, secrets, err
}

func inheritedWorkspace(cmd *cobra.Command) string {
	value, _ := cmd.Flags().GetString("cwd")
	return value
}

// mcpSecretValues flattens configured MCP OAuth/env secret values so both the
// engine's memory-candidate screening and the CLI `memory remember` path redact
// against the same full secret set (005 US3 security).
func mcpSecretValues(paths state.Paths) []string {
	secrets, err := state.LoadMCPSecrets(paths)
	if err != nil {
		return nil
	}
	var values []string
	for _, server := range secrets.OAuth {
		for _, value := range server {
			if text, ok := value.(string); ok && text != "" {
				values = append(values, text)
			}
		}
	}
	for _, server := range secrets.Env {
		for _, value := range server {
			if value != "" {
				values = append(values, value)
			}
		}
	}
	return values
}

func configurationNotice(settings contract.Settings, secrets contract.Secrets) string {
	if strings.TrimSpace(secrets.ProviderAPIKey) == "" {
		return "Setup required: run `muhiyacode login` (browser sign-in) or `muhiyacode config set apiKey <key>`."
	}
	model, ok := state.ActiveModel(settings)
	if !ok {
		return "Setup required: run `muhiyacode config discover` or `muhiyacode config set model <id>`."
	}
	if model.ContextLimit <= 0 {
		return "Setup required: set the active model context with `muhiyacode config set contextLimit <tokens>`."
	}
	return ""
}

func promptTitle(prompt string) string {
	prompt = strings.Join(strings.Fields(prompt), " ")
	if prompt == "" {
		return "Workspace session"
	}
	runes := []rune(prompt)
	if len(runes) > 80 {
		return string(runes[:79]) + "…"
	}
	return prompt
}

func parseKeyValues(values []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, value := range values {
		key, entry, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, "\x00\r\n") {
			return nil, fmt.Errorf("invalid environment value %q; expected KEY=VALUE", value)
		}
		result[key] = entry
	}
	return result, nil
}

func newConsoleCallbacks(input io.Reader, _ io.Writer, errorsOut io.Writer) contract.Callbacks {
	reader := bufio.NewReader(input)
	var mu sync.Mutex
	return contract.Callbacks{
		Status: func(value string) {
			if strings.TrimSpace(value) != "" {
				fmt.Fprintln(errorsOut, "·", value)
			}
		},
		ToolStart: func(name string, _ json.RawMessage) { fmt.Fprintln(errorsOut, "→", name) },
		ToolEnd: func(name, output string) {
			line := strings.SplitN(strings.TrimSpace(output), "\n", 2)[0]
			if len(line) > 240 {
				line = line[:239] + "…"
			}
			fmt.Fprintf(errorsOut, "✓ %s  %s\n", name, line)
		},
		MCPStatus: func(value string) { fmt.Fprintln(errorsOut, "· MCP", value) },
		Confirm: func(ctx context.Context, message string) (bool, error) {
			if !readerIsTerminal(input) {
				return false, nil
			}
			mu.Lock()
			defer mu.Unlock()
			fmt.Fprintf(errorsOut, "%s\nAllow? [y/N] ", message)
			answer, err := reader.ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return false, err
			}
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			default:
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			return answer == "y" || answer == "yes", nil
		},
		Ask: func(_ context.Context, questions []contract.Question) ([]contract.Answer, error) {
			answers := make([]contract.Answer, 0, len(questions))
			for _, question := range questions {
				index := 0
				for i, choice := range question.Choices {
					if choice.Recommended {
						index = i
						break
					}
				}
				if len(question.Choices) > 0 {
					answers = append(answers, contract.Answer{Question: question.Question, Choice: question.Choices[index], Index: index})
				}
			}
			return answers, nil
		},
	}
}

func readerIsTerminal(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
