package orchestrator

import (
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const exactTokenPreflightAttempts = 3

// requestTokenCounter is an optional provider capability. Implementations must
// count the exact normalized wire request for the active model/tokenizer; an
// approximate implementation must not advertise this interface.
type requestTokenCounter interface {
	CountRequestTokens(contract.ChatRequest) (int, error)
}

type tokenPreflightInput struct {
	System      string
	Built       RequestBuild
	Definitions []contract.ToolDefinition
	SessionPin  string
	Reserve     int
}

type tokenPreflightState struct {
	input        tokenPreflightInput
	maxPrompt    int
	dropped      bool
	droppedUnits int
}

func (e *Engine) mainChatRequest(messages []contract.Message, definitions []contract.ToolDefinition, sessionPin string) contract.ChatRequest {
	reasoning, maxTokens := e.taskRequestPolicy()
	return contract.ChatRequest{
		SessionID:        sessionPin,
		Purpose:          contract.RequestPurposeMain,
		CacheEpoch:       e.CacheEpoch(),
		Messages:         messages,
		Tools:            definitions,
		ModelID:          e.settings.Provider.ActiveModelID,
		ToolChoice:       "auto",
		Reasoning:        reasoning,
		MaxTokens:        maxTokens,
		OnToken:          e.callbacks.Token,
		OnReasoningToken: e.callbacks.ReasoningToken,
		OnStreamReset:    e.callbacks.StreamReset,
		Seed:             e.seed,
	}
}

func (e *Engine) exactTokenPreflight(input tokenPreflightInput) (RequestBuild, int, error) {
	counter, ok := e.provider.(requestTokenCounter)
	if !ok {
		return input.Built, input.Reserve, nil
	}
	state, err := e.newTokenPreflightState(input)
	if err != nil {
		return input.Built, input.Reserve, err
	}
	for attempt := 0; attempt < exactTokenPreflightAttempts; attempt++ {
		exactTokens, countErr := e.countRequestTokens(counter, state.input)
		if countErr != nil {
			return state.input.Built, state.input.Reserve, countErr
		}
		if state.accepts(exactTokens) {
			return state.input.Built, state.input.Reserve, nil
		}
		if attempt == exactTokenPreflightAttempts-1 {
			return state.input.Built, state.input.Reserve, state.overflowError(exactTokens)
		}
		state.prune(e, exactTokens)
	}
	return state.input.Built, state.input.Reserve, fmt.Errorf("exact request token preflight exhausted without a terminal decision")
}

func (e *Engine) newTokenPreflightState(input tokenPreflightInput) (*tokenPreflightState, error) {
	modelID := e.settings.Provider.ActiveModelID
	profile := e.catalogModelProfile(modelID)
	maxPrompt := e.contextLimit() - profile.ContextOutputReserve(e.catalogModelMaxOutput(modelID)) - 512
	if maxPrompt <= 0 {
		return nil, fmt.Errorf("model context leaves no safe prompt capacity after output reserve")
	}
	return &tokenPreflightState{input: input, maxPrompt: maxPrompt, dropped: input.Built.WindowDropped, droppedUnits: input.Built.DroppedUnits}, nil
}

func (e *Engine) countRequestTokens(counter requestTokenCounter, input tokenPreflightInput) (int, error) {
	request := e.mainChatRequest(input.Built.Messages, input.Definitions, input.SessionPin)
	exactTokens, err := counter.CountRequestTokens(request)
	if err != nil {
		return 0, fmt.Errorf("exact request token count: %w", err)
	}
	if exactTokens <= 0 {
		return 0, fmt.Errorf("exact request token count returned invalid value %d", exactTokens)
	}
	return exactTokens, nil
}

func (s *tokenPreflightState) accepts(exactTokens int) bool {
	s.input.Built.EstimatedWireTokens = exactTokens
	s.input.Built.EstimateSource = "provider-tokenizer"
	if exactTokens > s.maxPrompt {
		return false
	}
	s.input.Built.OverBudget = false
	s.input.Built.WindowDropped = s.dropped
	s.input.Built.DroppedUnits = s.droppedUnits
	return true
}

func (s *tokenPreflightState) prune(e *Engine, exactTokens int) {
	s.input.Reserve += exactTokens - s.maxPrompt + 64
	next := e.history.BuildRequestWithMetadata(s.input.System, e.contextLimit(), s.input.Reserve)
	s.dropped = s.dropped || next.WindowDropped
	s.droppedUnits += next.DroppedUnits
	s.input.Built = next
}

func (s *tokenPreflightState) overflowError(exactTokens int) error {
	return fmt.Errorf(
		"exact request token count %d exceeds safe prompt window %d after %d pruning attempts",
		exactTokens, s.maxPrompt, exactTokenPreflightAttempts,
	)
}

func (e *Engine) buildMainRequest(system string, definitions []contract.ToolDefinition, sessionPin string, reserve requestBudgetReserve) (RequestBuild, int, error) {
	built := e.history.BuildRequestWithMetadata(system, e.contextLimit(), reserve.Total)
	built, effectiveReserve, err := e.exactTokenPreflight(tokenPreflightInput{System: system, Built: built, Definitions: definitions, SessionPin: sessionPin, Reserve: reserve.Total})
	if err != nil {
		return RequestBuild{}, effectiveReserve, err
	}
	built.EstimatedToolTokens = reserve.ToolTokens
	built.EstimatedCoreToolTokens = reserve.CoreToolTokens
	built.EstimatedDeferredToolTokens = reserve.DeferredToolTokens
	if built.EstimateSource != "provider-tokenizer" {
		built.EstimatedWireTokens = built.EstimatedPromptTokens + reserve.ToolTokens
	}
	built.SerializedToolBytes = reserve.ToolBytes
	built.SerializedCoreToolBytes = reserve.CoreToolBytes
	built.SerializedDeferredToolBytes = reserve.DeferredToolBytes
	return built, effectiveReserve, nil
}
