package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// buildContextManifest inventories the exact already-assembled logical bytes;
// it never rewrites the messages, prompt, or tool definitions sent on the wire.
func buildContextManifest(requestSeq uint64, encodedDefinitions []byte, promptText string, promptContext PromptContext, messages []contract.Message, taskTailContent string) (ContextManifest, error) {
	manifest := ContextManifest{RequestSeq: requestSeq, MeasurementKind: contract.MeasurementCalibratedEstimate}
	cursor := 0
	add := func(segment ContextSegment) error {
		start, end := cursor, cursor+segment.Bytes
		segment.ByteStart, segment.ByteEnd = &start, &end
		cursor = end
		return manifest.Add(segment)
	}
	if err := add(NewContextSegment("core-tools", SegmentCoreTools, "session-definitions", StabilitySession, encodedDefinitions, EstimateTokens(string(encodedDefinitions)), true)); err != nil {
		return ContextManifest{}, err
	}

	projectBlock := strings.TrimRight(promptContext.ProjectContextBlock, "\n")
	systemCore := promptText
	projectPayload := ""
	if strings.TrimSpace(projectBlock) != "" {
		suffix := "\n\n" + projectBlock
		if !strings.HasSuffix(promptText, suffix) {
			return ContextManifest{}, errors.New("rendered project context is not the system-prompt suffix")
		}
		systemCore = strings.TrimSuffix(promptText, suffix)
		projectPayload = suffix
	}
	if err := add(NewContextSegment("system", SegmentSystem, "universal-system", StabilitySession, []byte(systemCore), EstimateTokens(systemCore), true)); err != nil {
		return ContextManifest{}, err
	}
	if projectPayload != "" {
		if err := add(NewContextSegment("project-context", SegmentProjectRoot, "project-context", StabilitySession, []byte(projectPayload), EstimateTokens(projectPayload), true)); err != nil {
			return ContextManifest{}, err
		}
	}

	messageBytes := make([]byte, 0, len(messages)*64)
	for index, message := range messages {
		encoded, err := json.Marshal(message)
		if err != nil {
			return ContextManifest{}, fmt.Errorf("encode message %d: %w", index, err)
		}
		messageBytes = append(messageBytes, encoded...)
		kind := SegmentUser
		source := fmt.Sprintf("history:%d:%s", index, message.Role)
		stability := StabilityEpoch
		switch {
		case message.Role == contract.RoleTool:
			kind = SegmentObservation
		case message.Role == contract.RoleAssistant && (message.ReasoningContent != nil || len(message.ReasoningDetails) > 0):
			kind = SegmentAssistantReasoning
		case message.Role == contract.RoleUser && message.Content == taskTailContent:
			kind = SegmentCurrentGoal
			source = "current-task-tail"
			stability = StabilityRequest
		}
		if err := add(NewContextSegment(fmt.Sprintf("message-%06d", index), kind, source, stability, encoded, EstimateTokens(string(encoded)), true)); err != nil {
			return ContextManifest{}, err
		}
	}
	stableBytes := make([]byte, 0, len(encodedDefinitions)+len(promptText)+1)
	stableBytes = append(stableBytes, encodedDefinitions...)
	stableBytes = append(stableBytes, 0)
	stableBytes = append(stableBytes, promptText...)
	manifest.StablePrefixHash = fingerprintBytes(stableBytes)
	manifest.MessagePrefixHash = fingerprintBytes(messageBytes)
	return manifest, nil
}

func (e *Engine) recordContextManifest(manifest ContextManifest) {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	for index := len(e.contextManifests) - 1; index >= 0; index-- {
		if e.contextManifests[index].RequestSeq == manifest.RequestSeq {
			e.contextManifests[index] = manifest.Clone()
			return
		}
	}
	e.contextManifests = append(e.contextManifests, manifest.Clone())
	const retainedManifests = 128
	if len(e.contextManifests) > retainedManifests {
		e.contextManifests = append([]ContextManifest(nil), e.contextManifests[len(e.contextManifests)-retainedManifests:]...)
	}
}

func (e *Engine) ContextManifests() []ContextManifest {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	result := make([]ContextManifest, len(e.contextManifests))
	for index := range e.contextManifests {
		result[index] = e.contextManifests[index].Clone()

	}
	return result
}

func assistantReplayMessage(response contract.ChatResponse, content string, calls []contract.ToolCall) contract.Message {
	message := contract.Message{Role: contract.RoleAssistant, Content: content, ToolCalls: calls}
	if strings.TrimSpace(response.Reasoning) != "" {
		reasoning := response.Reasoning
		message.ReasoningContent = &reasoning
	}
	if len(response.ReasoningDetails) > 0 {
		message.ReasoningDetails = append(json.RawMessage(nil), response.ReasoningDetails...)
	}
	return message
}
