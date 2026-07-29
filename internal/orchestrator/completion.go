package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func (e *Engine) handleTruncatedTextResponse(ctx context.Context, response contract.ChatResponse, retries *int) (retry, terminate bool) {
	if response.FinishReason != "length" || len(response.ToolCalls) != 0 {
		return false, false
	}
	if *retries >= 2 {
		return false, true
	}
	*retries = *retries + 1
	if strings.TrimSpace(response.Content) != "" {
		_ = e.persistAssistant(ctx, response.Content)
	}
	e.history.Append(contract.Message{
		Role:    contract.RoleUser,
		Content: "[continue] Your previous response was cut off by the output limit. Continue from where it ended and finish with a concise, factual result. Do not repeat the completed portion.",
	})
	return true, false
}

func requestBuildError(built RequestBuild, contextLimit, reserve int) error {
	if !built.OverBudget {
		return nil
	}
	return fmt.Errorf(
		"request exceeds the model context window after pruning: %s estimate %d tokens from %d serialized message bytes, prompt budget %d tokens (context %d, output and tool reserve %d)",
		built.EstimateSource,
		built.EstimatedPromptTokens,
		built.SerializedMessageBytes,
		built.PromptBudgetTokens,
		contextLimit,
		reserve,
	)
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
