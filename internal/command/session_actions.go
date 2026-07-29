package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/muhiya/muhiyacode/internal/state"
)

func openSessionState(ctx context.Context) (state.Paths, *state.DB, *state.Sessions, error) {
	paths, err := state.EnsurePaths()
	if err != nil {
		return state.Paths{}, nil, nil, err
	}
	db, err := state.Open(ctx, paths)
	if err != nil {
		return state.Paths{}, nil, nil, err
	}
	return paths, db, &state.Sessions{DB: db}, nil
}

func newSessionForkCommand() *cobra.Command {
	var title string
	command := &cobra.Command{
		Use: "fork <session-id>", Short: "Fork a session at its latest durable event", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, db, sessions, err := openSessionState(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			cursor, err := db.LatestExecutionCursor(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			forked, err := sessions.ForkAt(cmd.Context(), args[0], title, cursor, time.Now().UTC())
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), forked.ID)
			return err
		},
	}
	command.Flags().StringVar(&title, "title", "", "optional title for the forked session")
	return command
}

func newSessionArchiveCommand(unarchive bool) *cobra.Command {
	name := "archive"
	short := "Archive a session"
	if unarchive {
		name = "unarchive"
		short = "Restore an archived session"
	}
	return &cobra.Command{
		Use: fmt.Sprintf("%s <session-id>", name), Short: short, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, db, _, err := openSessionState(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			return db.ArchiveSession(cmd.Context(), args[0], !unarchive)
		},
	}
}

func newSessionDeleteCommand() *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use: "delete <session-id>", Short: "Guarded deletion of a session and its sidecars", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				return errors.New("deleting a session is permanent — rerun with --force to confirm")
			}
			_, db, sessions, err := openSessionState(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			return sessions.Delete(cmd.Context(), args[0])
		},
	}
	command.Flags().BoolVar(&force, "force", false, "confirm permanent session deletion")
	return command
}

func newSessionExportCommand(use string, forceSanitize bool) *cobra.Command {
	var output string
	var sanitize bool
	command := &cobra.Command{
		Use: fmt.Sprintf("%s <session-id>", use), Short: "Export a versioned session bundle", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, db, _, err := openSessionState(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			bundle, err := db.ExportSession(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if sanitize || forceSanitize {
				secrets, _ := state.LoadSecrets(paths)
				bundle = state.SanitizeSessionBundle(bundle, secrets.ProviderAPIKey)
			}
			payload, err := json.MarshalIndent(bundle, "", "  ")
			if err != nil {
				return err
			}
			payload = append(payload, '\n')
			if output == "" || output == "-" {
				_, err = cmd.OutOrStdout().Write(payload)
				return err
			}
			file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			if _, err = file.Write(payload); err == nil {
				err = file.Sync()
			}
			return errors.Join(err, file.Close())
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "-", "output file, or - for stdout (refuses overwrite)")
	if !forceSanitize {
		command.Flags().BoolVar(&sanitize, "sanitize", false, "redact secret-like values before export")
	}
	return command
}

func newSessionImportCommand() *cobra.Command {
	var targetWorkspace string
	command := &cobra.Command{
		Use: "import <bundle.json>", Short: "Import a versioned session bundle", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := os.Stat(args[0])
			if err != nil {
				return err
			}
			if info.Size() > 64<<20 {
				return errors.New("session bundle exceeds 64 MiB")
			}
			file, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer file.Close()
			decoder := json.NewDecoder(io.LimitReader(file, (64<<20)+1))
			decoder.DisallowUnknownFields()
			var bundle state.SessionBundle
			if err := decoder.Decode(&bundle); err != nil {
				return err
			}
			var trailing any
			if err := decoder.Decode(&trailing); err != io.EOF {
				return errors.New("session bundle contains trailing JSON")
			}
			_, db, sessions, err := openSessionState(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			imported, err := sessions.Import(cmd.Context(), bundle, targetWorkspace)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), imported.ID)
			return err
		},
	}
	command.Flags().StringVar(&targetWorkspace, "workspace", "", "override the imported workspace path")
	return command
}

func newSessionSearchCommand() *cobra.Command {
	return &cobra.Command{
		Use: "search <query>", Short: "Search session titles and workspace paths", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, db, _, err := openSessionState(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			sessions, err := db.ListSessions(cmd.Context(), "", 1_000)
			if err != nil {
				return err
			}
			query := strings.ToLower(args[0])
			for _, session := range sessions {
				if strings.Contains(strings.ToLower(session.Title+"\n"+session.WorkspacePath), query) {
					fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", session.ID, session.Title, session.WorkspacePath)
				}
			}
			return nil
		},
	}
}

func newSessionStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use: "stats <session-id>", Short: "Show durable session event statistics", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, db, _, err := openSessionState(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			bundle, err := db.ExportSession(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(),
				"session=%s parent=%s events=%d execution_events=%d created=%s updated=%s\n",
				bundle.Session.ID, bundle.Session.ParentID, len(bundle.Events), len(bundle.ExecutionEvents),
				bundle.Session.CreatedAt.Format(time.RFC3339), bundle.Session.UpdatedAt.Format(time.RFC3339),
			)
			return err
		},
	}
}
