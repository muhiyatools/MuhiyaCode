package orchestrator

import (
	"encoding/json"
	"strings"

	"github.com/muhiya/muhiyacode/internal/evidence"
)

func (e *Engine) virtualizeToolOutcome(outcome toolOutcome) toolOutcome {
	if e.artifactStore == nil || strings.TrimSpace(outcome.Output) == "" {
		return outcome
	}
	status := "success"
	if outcome.Failed {
		status = "failure"
	}
	metadata, err := e.artifactStore.Put(evidence.PutInput{
		SessionID: e.session.ID, WorkspaceID: e.workspaceID, Source: outcome.Call.ToolName(),
		Status: status, Complete: true, Retention: evidence.RetentionSession, ReducerVersion: "014-reducer-v1",
		Content: []byte(outcome.Output),
	})
	if err == nil && e.inspection != nil && readonlyTools[outcome.Call.ToolName()] {
		e.inspection.RecordArtifact(outcome.Call, metadata.Handle, metadata.ContentHash)
	}
	if err != nil || (e.settings.TokenEconomyMode != "balanced" && e.settings.TokenEconomyMode != "aggressive") {
		return outcome
	}
	threshold := 6_000
	if outcome.Failed {
		threshold = 12_000
	}
	if len(outcome.Output) <= threshold {
		return outcome
	}
	name := outcome.Call.ToolName()
	var card evidence.ObservationCard
	switch {
	case name == "run_shell":
		card = evidence.ReduceProcess(evidence.ProcessReductionInput{Adapter: "shell", Status: status, ExitCode: boolExit(outcome.Failed), Complete: true, Artifact: metadata.Handle, Text: outcome.Output})
	case name == "edit_file" || name == "multi_edit" || name == "write_file" || name == "apply_patch" || name == "git_diff":
		card = evidence.ReduceDiff(evidence.DiffReductionInput{Status: status, Complete: true, Artifact: metadata.Handle, Text: outcome.Output})
	case strings.HasPrefix(name, "mcp__") || json.Valid([]byte(outcome.Output)):
		card = evidence.ReduceJSON(evidence.JSONReductionInput{Kind: name, Status: status, Complete: true, Artifact: metadata.Handle, Raw: []byte(outcome.Output)})
	default:
		card = evidence.ReduceFile(evidence.FileReductionInput{Kind: name, Source: toolSource(outcome), Status: status, Complete: true, Artifact: metadata.Handle, Text: outcome.Output})
	}
	outcome.Output = card.Render()
	return outcome
}

func boolExit(failed bool) int {
	if failed {
		return 1
	}
	return 0
}

func toolSource(outcome toolOutcome) string {
	if path := pathArgument(outcome.Call); path != "" {
		return path
	}
	return outcome.Call.ToolName()
}
