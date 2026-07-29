package command

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/tui"
	"github.com/spf13/cobra"
)

type CheckpointScope string

const (
	CheckpointCode         CheckpointScope = "code"
	CheckpointConversation CheckpointScope = "conversation"
	CheckpointBoth         CheckpointScope = "both"
)

func ParseCheckpointScope(value string) (CheckpointScope, error) {
	scope := CheckpointScope(strings.ToLower(strings.TrimSpace(value)))
	switch scope {
	case CheckpointCode, CheckpointConversation, CheckpointBoth:
		return scope, nil
	default:
		return "", fmt.Errorf("checkpoint scope must be code, conversation, or both")
	}
}

func (a *Application) Checkpoints(ctx context.Context) ([]contract.CheckpointInfo, error) {
	a.mu.Lock()
	checkpoint := a.activeCheckpoint
	a.mu.Unlock()
	if checkpoint == nil {
		return nil, nil
	}
	return checkpoint.List(ctx, 100)
}

func (a *Application) CreateCheckpoint(ctx context.Context, description string, files []string) (string, error) {
	a.mu.Lock()
	checkpoint := a.activeCheckpoint
	a.mu.Unlock()
	if checkpoint == nil {
		return "", fmt.Errorf("checkpoint service is unavailable")
	}
	for index, file := range files {
		if !filepath.IsAbs(file) {
			files[index] = filepath.Join(checkpoint.Workspace, file)
		}
	}
	return checkpoint.Create(ctx, description, files)
}

func (a *Application) DeleteCheckpoint(ctx context.Context, id string) error {
	a.mu.Lock()
	checkpoint := a.activeCheckpoint
	a.mu.Unlock()
	if checkpoint == nil {
		return fmt.Errorf("checkpoint service is unavailable")
	}
	return checkpoint.Delete(ctx, id)
}

func (a *Application) RestoreCheckpoint(
	ctx context.Context,
	id string,
	scope CheckpointScope,
) (tui.Runtime, []contract.Event, string, error) {
	switch scope {
	case CheckpointCode, CheckpointConversation, CheckpointBoth:
	default:
		return tui.Runtime{}, nil, "", fmt.Errorf("invalid checkpoint scope %q", scope)
	}
	a.mu.Lock()
	checkpoint := a.activeCheckpoint
	session := a.runtime.Session
	a.mu.Unlock()
	if checkpoint == nil {
		return tui.Runtime{}, nil, "", fmt.Errorf("checkpoint service is unavailable")
	}
	info, err := checkpoint.Info(ctx, id)
	if err != nil {
		return tui.Runtime{}, nil, "", err
	}

	var forked contract.Session
	if scope == CheckpointConversation || scope == CheckpointBoth {
		forked, err = a.sessions.ForkAt(ctx, session.ID, session.Title+" (rewind)", info.EventCursor, info.CreatedAt)
		if err != nil {
			return tui.Runtime{}, nil, "", err
		}
	}
	message := ""
	if scope == CheckpointCode || scope == CheckpointBoth {
		message, _, err = checkpoint.RestoreCode(ctx, id)
		if err != nil {
			if forked.ID != "" {
				_ = a.sessions.Delete(ctx, forked.ID)
			}
			return tui.Runtime{}, nil, "", err
		}
	}
	if forked.ID == "" {
		return tui.Runtime{}, nil, message, nil
	}
	runtime, recent, err := a.switchSession(ctx, forked)
	if err != nil {
		return tui.Runtime{}, nil, "", err
	}
	if message != "" {
		message += " "
	}
	message += "Conversation restored into forked session " + forked.ID + "."
	return runtime, recent, message, nil
}

func newCheckpointCommand() *cobra.Command {
	var sessionID string
	command := &cobra.Command{Use: "checkpoint", Short: "List or restore session checkpoints"}
	list := &cobra.Command{
		Use: "list", Short: "List selectable checkpoints", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := OpenApplication(ApplicationOptions{
				Context: cmd.Context(), Workspace: inheritedWorkspace(cmd),
				SessionID: sessionID, DisableMCP: true,
			})
			if err != nil {
				return err
			}
			defer app.Close()
			checkpoints, err := app.Checkpoints(cmd.Context())
			if err != nil {
				return err
			}
			if len(checkpoints) == 0 {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "No checkpoints found.")
				return err
			}
			for _, checkpoint := range checkpoints {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  cursor=%d  %s\n",
					checkpoint.ID,
					checkpoint.CreatedAt.Local().Format(time.RFC3339),
					checkpoint.EventCursor,
					checkpoint.Description,
				)
			}
			return nil
		},
	}
	var scopeValue string
	restore := &cobra.Command{
		Use: "restore <checkpoint-id>", Short: "Restore code, conversation, or both", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := ParseCheckpointScope(scopeValue)
			if err != nil {
				return err
			}
			app, err := OpenApplication(ApplicationOptions{
				Context: cmd.Context(), Workspace: inheritedWorkspace(cmd),
				SessionID: sessionID, DisableMCP: true,
			})
			if err != nil {
				return err
			}
			defer app.Close()
			_, _, message, err := app.RestoreCheckpoint(cmd.Context(), args[0], scope)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), message)
			return err
		},
	}
	command.PersistentFlags().StringVar(&sessionID, "session", "", "session ID (defaults to the latest workspace session)")
	restore.Flags().StringVar(&scopeValue, "scope", string(CheckpointBoth), "restore scope: code, conversation, or both")
	var description string
	create := &cobra.Command{
		Use: "create <file> [file...]", Short: "Create a manual code checkpoint", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := OpenApplication(ApplicationOptions{
				Context: cmd.Context(), Workspace: inheritedWorkspace(cmd),
				SessionID: sessionID, DisableMCP: true,
			})
			if err != nil {
				return err
			}
			defer app.Close()
			id, err := app.CreateCheckpoint(cmd.Context(), description, args)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), id)
			return err
		},
	}
	create.Flags().StringVar(&description, "description", "manual checkpoint", "checkpoint description")
	deleteCommand := &cobra.Command{
		Use: "delete <checkpoint-id>", Short: "Delete a checkpoint snapshot", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := OpenApplication(ApplicationOptions{
				Context: cmd.Context(), Workspace: inheritedWorkspace(cmd),
				SessionID: sessionID, DisableMCP: true,
			})
			if err != nil {
				return err
			}
			defer app.Close()
			return app.DeleteCheckpoint(cmd.Context(), args[0])
		},
	}
	command.AddCommand(list, restore, create, deleteCommand)
	return command
}
