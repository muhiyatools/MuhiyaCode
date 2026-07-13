package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

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
	HistoryHash    string `json:"history_hash"`
	SettledHash    string `json:"settled_hash"`
	MessageCount   int    `json:"message_count"`
	RewriteVersion int    `json:"rewrite_version"`
	ModelID        string `json:"model_id"`
}

// NewPrefixShape hashes exactly the serialized system message and canonical
// tools bytes that will be sent. json.Marshal is deterministic for Go maps and
// preserves the caller-controlled tool slice order. The caller must therefore
// pass the tools in their actual wire order; sorting here would mask a real
// prefix change.
func NewPrefixShape(system string, tools []contract.ToolDefinition, rewriteVersion int, modelID string) (PrefixShape, error) {
	return NewWirePrefixShape(contract.ChatRequest{
		Messages: []contract.Message{{Role: contract.RoleSystem, Content: system}},
		Tools:    tools, ToolChoice: "auto", ModelID: modelID,
	}, 1, rewriteVersion)
}

// NewWirePrefixShape hashes the stable regions as they are represented on the
// request wire. settledCount is the number of messages transmitted by the
// preceding request; hashing that slice separately makes an in-place rewrite
// visible while allowing an append-only fresh tail.
func NewWirePrefixShape(request contract.ChatRequest, settledCount, rewriteVersion int) (PrefixShape, error) {
	if len(request.Messages) == 0 {
		return PrefixShape{}, fmt.Errorf("request prefix shape requires at least one message")
	}
	systemBytes, err := json.Marshal(request.Messages[0])
	if err != nil {
		return PrefixShape{}, err
	}
	choice := request.ToolChoice
	if choice == "" && len(request.Tools) > 0 {
		choice = "auto"
	}
	toolBytes, err := json.Marshal(struct {
		Tools      []contract.ToolDefinition `json:"tools"`
		ToolChoice string                    `json:"tool_choice"`
	}{request.Tools, choice})
	if err != nil {
		return PrefixShape{}, err
	}
	history := request.Messages[1:]
	settledHistoryCount := max(0, min(len(history), settledCount-1))
	historyBytes, err := marshalMessageSequence(history)
	if err != nil {
		return PrefixShape{}, err
	}
	settledBytes, err := marshalMessageSequence(history[:settledHistoryCount])
	if err != nil {
		return PrefixShape{}, err
	}
	return PrefixShape{
		SystemHash:     hashBytes(systemBytes),
		ToolsHash:      hashBytes(toolBytes),
		HistoryHash:    hashBytes(historyBytes),
		SettledHash:    hashBytes(settledBytes),
		MessageCount:   len(request.Messages),
		RewriteVersion: rewriteVersion,
		ModelID:        request.ModelID,
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
	if previous.HistoryHash != current.SettledHash || current.MessageCount < previous.MessageCount {
		reasons = append(reasons, PrefixReasonHistory)
	}
	if previous.ModelID != current.ModelID {
		reasons = append(reasons, PrefixReasonModel)
	}
	return reasons
}

func marshalMessageSequence(messages []contract.Message) ([]byte, error) {
	var result []byte
	for _, message := range messages {
		encoded, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		result = append(result, encoded...)
		result = append(result, '\n')
	}
	return result, nil
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
