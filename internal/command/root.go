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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	appcore "github.com/muhiya/muhiyacode/internal/app"
	"github.com/muhiya/muhiyacode/internal/buildinfo"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/mcpclient"
	"github.com/muhiya/muhiyacode/internal/state"
	"github.com/muhiya/muhiyacode/internal/tui"
	"github.com/muhiya/muhiyacode/internal/updatecheck"
	"github.com/spf13/cobra"
)

func Execute(ctx context.Context, args []string) error {
	command := NewRootCommand()
	command.SetArgs(args)
	return command.ExecuteContext(ctx)
}

func optionalSeed(value int) *int {
	if value < 0 {
		return nil
	}
	return &value
}

func NewRootCommand() *cobra.Command {
	var printPrompt, cwd string
	var seed int
	var simple, fresh, noMCP, unsafeFullAccess, jsonl bool
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
				return runOneShot(cmd, cwd, printPrompt, fresh, noMCP, unsafeFullAccess, jsonl, optionalSeed(seed))
			}
			if jsonl {
				return errors.New("--jsonl requires a one-shot prompt supplied with --print")
			}
			return runInteractive(cmd, interactiveOptions{Workspace: cwd, Prompt: prompt, Fresh: fresh, NoMCP: noMCP, Simple: simple, Seed: optionalSeed(seed)})
		},
	}
	// Commit/Date are set by the release build's ldflags (.goreleaser.yaml);
	// source builds show "unknown" for both, same as buildinfo.Version's default
	// (feature 010 T037: these were ldflags-injected but had no reader anywhere —
	// wiring them into --version is the fix, since the alternative was deleting
	// working release metadata for no reason).
	root.SetVersionTemplate(fmt.Sprintf("MuhiyaCode {{.Version}} (commit %s, built %s)\n", buildinfo.Commit, buildinfo.Date))
	root.PersistentFlags().StringVarP(&cwd, "cwd", "C", "", "workspace directory")
	root.PersistentFlags().IntVar(&seed, "seed", -1, "provider seed for reproducible runs (when supported)")
	root.Flags().StringVarP(&printPrompt, "print", "p", "", "run one-shot prompt and exit")
	root.Flags().BoolVar(&simple, "simple", false, "use the line interface instead of the full TUI")
	root.Flags().BoolVar(&fresh, "new", false, "start a new session instead of reopening the latest workspace session")
	root.Flags().BoolVar(&noMCP, "no-mcp", false, "start without connecting MCP servers")
	root.Flags().BoolVar(&unsafeFullAccess, "unsafe-full-access", false, "allow unrestricted unattended file and shell tools in one-shot mode")
	root.Flags().BoolVar(&jsonl, "jsonl", false, "emit versioned JSON Lines events in one-shot mode")
	root.AddCommand(newLoginCommand(), newLogoutCommand(), newResumeCommand(), newSessionsCommand(), newCheckpointCommand(), newProcessCommand(), newModelCommand(), newConfigCommand(), newMCPCommand(), newDoctorCommand(), newBenchmarkCommand(), newDocsCommand())
	return root
}

type interactiveOptions struct {
	Workspace, SessionID, Prompt string
	Fresh, NoMCP, Simple         bool
	Seed                         *int
}

func runInteractive(cmd *cobra.Command, options interactiveOptions) error {
	bridge := tui.NewBridge()
	// T021: open the fast core (config/DB/session) now so the composer can appear
	// immediately; build the runtime in the background via the Hydrate closure.
	app, err := openApplicationCore(ApplicationOptions{
		Context: cmd.Context(), Workspace: options.Workspace, SessionID: options.SessionID,
		NewSession: options.Fresh, Title: promptTitle(options.Prompt), Callbacks: bridge.Callbacks(), DisableMCP: options.NoMCP,
		Seed: options.Seed,
	})
	if err != nil {
		return err
	}
	defer app.Close()
	// UMI-06: Auto Accept is no longer reset at interactive startup. The
	// session runtime record is the permission authority: a resumed session
	// restores its saved mode, and a new session inherits the global default
	// (Normal unless the user explicitly configured a trusted default).
	startupNotice := ""
	// Ask the registry whether a newer version exists, on its own goroutine so
	// startup never waits on it. Cached for a day, silent on failure, and the
	// result is read at hydration — by then it has either landed or it has not,
	// and either way the session proceeds.
	updateCh := make(chan string, 1)
	go func() {
		updateCh <- updatecheck.Refresh(cmd.Context(), filepath.Join(app.paths.CacheDir, "update_check.json"))
	}()
	hydrate := func(ctx context.Context) (tui.HydratedRuntime, error) {
		if err := app.Hydrate(ctx); err != nil {
			return tui.HydratedRuntime{}, err
		}
		// One-shot startup notices are read from the now-live engine: config gaps,
		notice := configurationNotice(*app.Settings(), app.secrets)
		if recovery := strings.TrimSpace(app.Runtime().RecoveryNotice); recovery != "" {
			if notice != "" {
				notice += " "
			}
			notice += recovery
		}
		latest := ""
		select {
		case latest = <-updateCh:
		default: // still in flight — the header simply shows nothing this run
		}
		return tui.HydratedRuntime{
			Runtime: app.Runtime(), Actions: app.Actions(), Recent: app.Recent(), Notice: notice, LatestVersion: latest,
		}, nil
	}
	return tui.Run(tui.Options{
		// A partial runtime (settings + session, no engine) lets the loading shell
		// render the real header/title immediately; Hydrate fills in the engine.
		Runtime: tui.Runtime{Settings: app.settings, Session: app.session},
		Bridge:  bridge, Version: buildinfo.Version,
		InitialPrompt: options.Prompt, Context: cmd.Context(), Simple: options.Simple,
		Hydrate: hydrate, Notice: startupNotice,
	})
}

func runOneShot(cmd *cobra.Command, cwd, prompt string, fresh, noMCP, unsafeFullAccess, jsonl bool, seed *int) error {
	var emitter *jsonlEmitter
	callbacks := newConsoleCallbacks(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	if jsonl {
		emitter = newJSONLEmitter(cmd.OutOrStdout())
		callbacks = emitter.callbacks()
	}
	app, err := OpenApplication(ApplicationOptions{Context: cmd.Context(), Workspace: cwd, NewSession: fresh, Title: promptTitle(prompt), Callbacks: callbacks, DisableMCP: noMCP, Seed: seed})
	if err != nil {
		return err
	}
	defer app.Close()
	if emitter != nil {
		secretValues := mcpSecretValues(app.paths)
		emitter.SetRedactor(func(value string) string {
			return state.Redact(value, app.secrets, secretValues...)
		})
	}
	if notice := configurationNotice(*app.Settings(), app.secrets); notice != "" {
		return errors.New(notice)
	}
	if app.Settings().PermissionMode == contract.PermissionAutoAccept && !unsafeFullAccess {
		return errors.New("saved auto-accept/full-access mode requires explicit --unsafe-full-access for a non-interactive run")
	}
	if unsafeFullAccess {
		app.Runtime().Engine.SetPermissionMode(contract.PermissionAutoAccept)
		if app.guard != nil {
			if err := app.guard.SetMode(contract.PermissionAutoAccept); err != nil {
				return err
			}
		}
	}
	answer, stats, err := app.Runtime().Engine.Run(cmd.Context(), appcore.AssemblePrompt(cmd.Context(), prompt, nil, nil, nil))
	if emitter != nil {
		emitter.terminal(answer, stats, err)
		if emitErr := emitter.Err(); emitErr != nil {
			return emitErr
		}
	}
	if err != nil {
		return err
	}
	if emitter == nil {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSpace(answer)); err != nil {
			return err
		}
	}
	if stats.Status != contract.TaskStatusSucceeded {
		reason := stats.TerminatedReason
		if reason == "" {
			reason = stats.StopCause
		}
		return &TaskExitError{Status: stats.Status, Reason: reason}
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
			if err := state.SaveModelCatalog(state.ModelCatalogCache{
				Version: 2, RefreshedAt: time.Now().UTC(), Models: models,
			}, paths); err != nil {
				return err
			}
			stranded := addDiscoveredModels(&settings, models)
			if err := state.SaveSettings(settings, paths); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Discovered %d model(s). Using %s.\n", len(models), settings.Provider.ActiveModelID)
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

func loadConfig() (state.Paths, contract.Settings, contract.Secrets, error) {
	paths, err := state.EnsurePaths()
	if err != nil {
		return state.Paths{}, contract.Settings{}, contract.Secrets{}, err
	}
	settings, err := state.LoadSettings(paths)
	if err != nil {
		return paths, contract.Settings{}, contract.Secrets{}, err
	}
	catalog, err := state.LoadModelCatalog(paths)
	if err != nil {
		return paths, contract.Settings{}, contract.Secrets{}, err
	}
	if len(catalog.Models) > 0 {
		addDiscoveredModels(&settings, append([]contract.Model(nil), catalog.Models...))
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
		collectSecretStrings(server, &values)
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

// collectSecretStrings walks an OAuth entry (nested maps/slices) and appends every
// string leaf long enough to be a credential. The real secrets — access/refresh
// tokens under "tokens", client_secret under "clientInformation" — are NESTED, so
// a top-level-only scan collected only harmless URLs and left a bare opaque token
// (e.g. ya29.a0Af…) to be written verbatim to memory/transcripts/events (I-4).
func collectSecretStrings(value any, out *[]string) {
	switch v := value.(type) {
	case string:
		if len(v) >= 8 {
			*out = append(*out, v)
		}
	case map[string]any:
		for _, item := range v {
			collectSecretStrings(item, out)
		}
	case []any:
		for _, item := range v {
			collectSecretStrings(item, out)
		}
	}
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
		Ask: func(ctx context.Context, questions []contract.Question) ([]contract.Answer, error) {
			if !readerIsTerminal(input) {
				return nil, errors.New("user input is required, but stdin is non-interactive")
			}
			mu.Lock()
			defer mu.Unlock()
			answers := make([]contract.Answer, 0, len(questions))
			for _, question := range questions {
				index := 0
				for i, choice := range question.Choices {
					if choice.Recommended {
						index = i
						break
					}
				}
				if len(question.Choices) == 0 {
					return nil, errors.New("question has no choices")
				}
				fmt.Fprintln(errorsOut, question.Question)
				for i, choice := range question.Choices {
					recommended := ""
					if choice.Recommended {
						recommended = " (recommended)"
					}
					fmt.Fprintf(errorsOut, "  %d. %s%s — %s\n", i+1, choice.Label, recommended, choice.Description)
				}
				fmt.Fprintf(errorsOut, "Choose [%d]: ", index+1)
				line, err := reader.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return nil, err
				}
				if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
					return nil, errors.New("question input ended without a selection")
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}
				if value := strings.TrimSpace(line); value != "" {
					selected, parseErr := strconv.Atoi(value)
					if parseErr != nil || selected < 1 || selected > len(question.Choices) {
						return nil, fmt.Errorf("invalid choice %q; expected 1-%d", value, len(question.Choices))
					}
					index = selected - 1
				}
				answers = append(answers, contract.Answer{Question: question.Question, Choice: question.Choices[index], Index: index})
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
