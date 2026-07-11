package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const (
	PrefixReasonSystem  = "system"
	PrefixReasonTools   = "tools"
	PrefixReasonHistory = "history"
	PrefixReasonModel   = "model"
)

// PrefixShape is a compact diagnostic identity for all stable request regions.
type PrefixShape struct {
	SystemHash     string `json:"system_hash"`
	ToolsHash      string `json:"tools_hash"`
	RewriteVersion int    `json:"rewrite_version"`
	ModelID        string `json:"model_id"`
}

// NewPrefixShape hashes exactly the serialized system message and canonical
// tools bytes that will be sent. json.Marshal is deterministic for Go maps and
// preserves the caller-controlled tool slice order. The caller must therefore
// pass the tools in their actual wire order; sorting here would mask a real
// prefix change.
func NewPrefixShape(system string, tools []contract.ToolDefinition, rewriteVersion int, modelID string) (PrefixShape, error) {
	systemBytes, err := json.Marshal(contract.Message{Role: contract.RoleSystem, Content: system})
	if err != nil {
		return PrefixShape{}, err
	}
	toolBytes, err := json.Marshal(tools)
	if err != nil {
		return PrefixShape{}, err
	}
	return PrefixShape{
		SystemHash:     hashBytes(systemBytes),
		ToolsHash:      hashBytes(toolBytes),
		RewriteVersion: rewriteVersion,
		ModelID:        modelID,
	}, nil
}

// CompareShape reports every changed stable region in deterministic order.
func CompareShape(previous, current PrefixShape) []string {
	reasons := make([]string, 0, 4)
	if previous.SystemHash != current.SystemHash {
		reasons = append(reasons, PrefixReasonSystem)
	}
	if previous.ToolsHash != current.ToolsHash {
		reasons = append(reasons, PrefixReasonTools)
	}
	if previous.RewriteVersion != current.RewriteVersion {
		reasons = append(reasons, PrefixReasonHistory)
	}
	if previous.ModelID != current.ModelID {
		reasons = append(reasons, PrefixReasonModel)
	}
	return reasons
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
