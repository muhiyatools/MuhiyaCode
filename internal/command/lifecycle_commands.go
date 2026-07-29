package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
	"github.com/muhiya/muhiyacode/internal/workspace"
	"github.com/spf13/cobra"
)

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
	command.AddCommand(
		newSessionForkCommand(),
		newSessionArchiveCommand(false),
		newSessionArchiveCommand(true),
		newSessionDeleteCommand(),
		newSessionExportCommand("export", false),
		newSessionExportCommand("sanitize", true),
		newSessionImportCommand(),
		newSessionSearchCommand(),
		newSessionStatsCommand(),
	)
	return command
}

func newProcessCommand() *cobra.Command {
	var sessionID string
	command := &cobra.Command{Use: "process", Short: "List or stop session-owned background processes"}
	resolveRegistry := func(cmd *cobra.Command) (string, *state.DB, error) {
		paths, db, _, err := openSessionState(cmd.Context())
		if err != nil {
			return "", nil, err
		}
		if sessionID == "" {
			root := inheritedWorkspace(cmd)
			if root == "" {
				root, err = os.Getwd()
			}
			if err == nil {
				root, err = workspace.CanonicalPath(root)
			}
			var found bool
			if err == nil {
				var session contract.Session
				session, found, err = db.LatestSession(cmd.Context(), root)
				sessionID = session.ID
			}
			if err != nil || !found {
				db.Close()
				return "", nil, errors.New("no session found for workspace")
			}
		}
		dir, err := state.SessionDir(paths, sessionID)
		if err != nil {
			db.Close()
			return "", nil, err
		}
		return filepath.Join(dir, "processes.json"), db, nil
	}
	list := &cobra.Command{
		Use: "list", Short: "List background processes owned by a session", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			registry, db, err := resolveRegistry(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			processes, err := workspace.ListProcessRegistry(registry)
			if err != nil {
				return err
			}
			if len(processes) == 0 {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "No owned background processes.")
				return err
			}
			for _, process := range processes {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  pid=%d  %s  %s\n",
					process.ID, process.PID, process.StartedAt.Format(time.RFC3339), process.Label)
			}
			return nil
		},
	}
	stop := &cobra.Command{
		Use: "stop <process-id>", Short: "Stop an owned background process tree", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, db, err := resolveRegistry(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			return workspace.StopRegisteredProcess(registry, args[0])
		},
	}
	command.PersistentFlags().StringVar(&sessionID, "session", "", "session ID (defaults to latest workspace session)")
	command.AddCommand(list, stop)
	return command
}

func newModelCommand() *cobra.Command {
	command := &cobra.Command{Use: "model", Short: "Inspect or select configured models"}
	list := &cobra.Command{
		Use: "list", Short: "List configured model capabilities", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, settings, _, err := loadConfig()
			if err != nil {
				return err
			}
			for _, model := range settings.Provider.Models {
				active := ""
				if model.ID == settings.Provider.ActiveModelID {
					active = "*"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s  tier=%d context=%d output=%d health=%s provider=%s\n",
					active, model.ID, model.CodingTier, model.ContextLimit, model.MaxOutput, model.Health, model.Provider)
			}
			return nil
		},
	}
	use := &cobra.Command{
		Use: "use <model-id>", Short: "Select and pin a model", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, settings, secrets, err := loadConfig()
			if err != nil {
				return err
			}
			if err := state.SetConfig("model", args[0], &settings, &secrets); err != nil {
				return err
			}
			return state.SaveSettings(settings, paths)
		},
	}
	command.AddCommand(list, use)
	return command
}
