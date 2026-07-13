package tui

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

type statusMsg string
type noticeMsg string
type streamMsg struct{ text, reasoning string }
type mcpRefreshTickMsg struct{}
type toolStartMsg struct {
	name  string
	input json.RawMessage
}
type toolEndMsg struct{ name, output string }
type toolOutputMsg struct{ name, output string }
type planMsg contract.Plan
type usageMsg contract.Usage
type contextMsg contract.ContextInfo
type agentMsg contract.AgentEvent
type statsMsg contract.TaskStats
type mcpStatusMsg string

type modalRequest struct {
	title, message string
	choices        []contract.QuestionChoice
	reply          chan int
}

// Bridge translates concurrent runtime callbacks into Bubble Tea messages.
// Token streams are coalesced to avoid one full render per token.
type Bridge struct {
	mu        sync.Mutex
	program   *tea.Program
	pending   string
	reasoning string
	// US2 T031: tool/shell output is coalesced per tool behind the same single-
	// outstanding flush as assistant tokens, so a shell streaming many chunks
	// produces one frame per flush window instead of one per chunk. toolOrder
	// preserves first-seen order for deterministic delivery.
	toolOrder []string
	toolBuf   map[string]string
	scheduled bool
	fallback  func(modalRequest) int
}

func (b *Bridge) SetFallback(handler func(modalRequest) int) {
	b.mu.Lock()
	b.fallback = handler
	b.mu.Unlock()
}

func NewBridge() *Bridge { return &Bridge{} }

func (b *Bridge) Attach(program *tea.Program) {
	b.mu.Lock()
	b.program = program
	b.mu.Unlock()
}

func (b *Bridge) send(message tea.Msg) {
	b.mu.Lock()
	program := b.program
	b.mu.Unlock()
	if program != nil {
		program.Send(message)
	}
}

func (b *Bridge) Callbacks() contract.Callbacks {
	return contract.Callbacks{
		Status:         func(value string) { b.send(statusMsg(value)) },
		Notice:         func(value string) { b.send(noticeMsg(value)) },
		Token:          func(value string) { b.queueStream(value, "") },
		ReasoningToken: func(value string) { b.queueStream("", value) },
		ToolStart: func(name string, input json.RawMessage) {
			b.flush()
			b.send(toolStartMsg{name: name, input: append(json.RawMessage(nil), input...)})
		},
		ToolOutput:   func(name, output string) { b.queueToolOutput(name, output) },
		ToolEnd:      func(name, output string) { b.flush(); b.send(toolEndMsg{name: name, output: output}) },
		PlanUpdate:   func(value contract.Plan) { b.send(planMsg(value)) },
		Usage:        func(value contract.Usage) { b.send(usageMsg(value)) },
		Context:      func(value contract.ContextInfo) { b.send(contextMsg(value)) },
		Agent:        func(value contract.AgentEvent) { b.send(agentMsg(value)) },
		MCPStatus:    func(value string) { b.send(mcpStatusMsg(value)) },
		TaskComplete: func(value contract.TaskStats) { b.flush(); b.send(statsMsg(value)) },
		Confirm: func(ctx context.Context, message string) (bool, error) {
			index, err := b.request(ctx, modalRequest{title: "Permission required", message: message, choices: []contract.QuestionChoice{{Label: "Allow once", Recommended: true}, {Label: "Deny"}}})
			return index == 0, err
		},
		Ask: func(ctx context.Context, questions []contract.Question) ([]contract.Answer, error) {
			answers := make([]contract.Answer, 0, len(questions))
			for _, question := range questions {
				// Defense in depth: the engine rejects choiceless questions, but never
				// index an empty slice here — a malformed question degrades to a blank
				// answer instead of panicking the event loop.
				if len(question.Choices) == 0 {
					answers = append(answers, contract.Answer{Question: question.Question, Choice: contract.QuestionChoice{}, Index: -1})
					continue
				}
				index, err := b.request(ctx, modalRequest{title: "Choose", message: question.Question, choices: question.Choices})
				if err != nil {
					return nil, err
				}
				if index < 0 || index >= len(question.Choices) {
					index = recommendedChoice(question.Choices)
				}
				answers = append(answers, contract.Answer{Question: question.Question, Choice: question.Choices[index], Index: index})
			}
			return answers, nil
		},
	}
}

func (b *Bridge) request(ctx context.Context, request modalRequest) (int, error) {
	request.reply = make(chan int, 1)
	b.mu.Lock()
	program, fallback := b.program, b.fallback
	b.mu.Unlock()
	if program == nil {
		if fallback != nil {
			return fallback(request), nil
		}
		return -1, nil
	}
	b.send(request)
	select {
	case index := <-request.reply:
		return index, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

func (b *Bridge) queueStream(text, reasoning string) {
	b.mu.Lock()
	b.pending += text
	b.reasoning += reasoning
	b.scheduleLocked()
	b.mu.Unlock()
}

// queueToolOutput buffers a tool/shell output chunk per tool behind the same
// single-outstanding flush window as assistant tokens (US2 T031).
func (b *Bridge) queueToolOutput(name, output string) {
	b.mu.Lock()
	if b.toolBuf == nil {
		b.toolBuf = make(map[string]string)
	}
	if _, ok := b.toolBuf[name]; !ok {
		b.toolOrder = append(b.toolOrder, name)
	}
	b.toolBuf[name] += output
	b.scheduleLocked()
	b.mu.Unlock()
}

// scheduleLocked arms exactly one outstanding flush; callers hold b.mu.
func (b *Bridge) scheduleLocked() {
	if b.scheduled {
		return
	}
	b.scheduled = true
	time.AfterFunc(35*time.Millisecond, b.flush)
}

func (b *Bridge) flush() {
	b.mu.Lock()
	if b.pending == "" && b.reasoning == "" && len(b.toolOrder) == 0 {
		b.scheduled = false
		b.mu.Unlock()
		return
	}
	var stream *streamMsg
	if b.pending != "" || b.reasoning != "" {
		stream = &streamMsg{text: b.pending, reasoning: b.reasoning}
	}
	toolMsgs := make([]toolOutputMsg, 0, len(b.toolOrder))
	for _, name := range b.toolOrder {
		if out := b.toolBuf[name]; out != "" {
			toolMsgs = append(toolMsgs, toolOutputMsg{name: name, output: out})
		}
	}
	b.pending, b.reasoning, b.scheduled = "", "", false
	b.toolOrder, b.toolBuf = nil, nil
	program := b.program
	b.mu.Unlock()
	if program == nil {
		return
	}
	// Assistant text precedes tool output in a turn, so flush the stream first.
	if stream != nil {
		program.Send(*stream)
	}
	for _, message := range toolMsgs {
		program.Send(message)
	}
}

func recommendedChoice(choices []contract.QuestionChoice) int {
	for i, choice := range choices {
		if choice.Recommended {
			return i
		}
	}
	return 0
}
