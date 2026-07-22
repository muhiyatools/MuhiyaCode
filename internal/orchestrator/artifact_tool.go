package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/evidence"
)

const maxArtifactFetchBytes int64 = 64 * 1024

type ArtifactFetcher struct {
	store       *evidence.Store
	sessionID   string
	workspaceID string
}

func NewArtifactFetcher(store *evidence.Store, sessionID, workspaceID string) (*ArtifactFetcher, error) {
	if store == nil || sessionID == "" || workspaceID == "" {
		return nil, errors.New("artifact fetcher requires store, session, and workspace identity")
	}
	return &ArtifactFetcher{store: store, sessionID: sessionID, workspaceID: workspaceID}, nil
}

func (fetcher *ArtifactFetcher) Definition() contract.ToolDefinition {
	return contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{
		Name: "fetch_artifact", Description: "Fetch one exact bounded byte range from a previously stored tool-result artifact.",
		Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"handle": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxArtifactFetchBytes},
		}, "required": []string{"handle", "offset", "limit"}},
	}}
}

func (fetcher *ArtifactFetcher) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Handle string `json:"handle"`
		Offset int64  `json:"offset"`
		Limit  int64  `json:"limit"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return "", fmt.Errorf("invalid fetch_artifact arguments: %w", err)
	}
	if input.Limit > maxArtifactFetchBytes {
		return "", fmt.Errorf("artifact fetch limit exceeds %d bytes", maxArtifactFetchBytes)
	}
	content, metadata, err := fetcher.store.Fetch(input.Handle, fetcher.sessionID, fetcher.workspaceID, input.Offset, input.Limit)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Artifact %s bytes %d-%d of %d (complete=%t):\n%s", metadata.Handle, input.Offset, input.Offset+int64(len(content)), metadata.SizeBytes, metadata.Complete, content), nil
}
