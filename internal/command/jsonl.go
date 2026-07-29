package command

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const headlessEventVersion = 1

type headlessEvent struct {
	Version int                 `json:"version"`
	Type    string              `json:"type"`
	At      time.Time           `json:"at"`
	Name    string              `json:"name,omitempty"`
	Text    string              `json:"text,omitempty"`
	Input   json.RawMessage     `json:"input,omitempty"`
	Answer  string              `json:"answer,omitempty"`
	Error   string              `json:"error,omitempty"`
	Stats   *contract.TaskStats `json:"stats,omitempty"`
}

type jsonlEmitter struct {
	mu      sync.Mutex
	encoder *json.Encoder
	err     error
	redact  func(string) string
}

func newJSONLEmitter(output io.Writer) *jsonlEmitter {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return &jsonlEmitter{encoder: encoder}
}

func (e *jsonlEmitter) emit(event headlessEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return
	}
	if e.redact != nil {
		event.Text = e.redact(event.Text)
		event.Answer = e.redact(event.Answer)
		event.Error = e.redact(event.Error)
		if len(event.Input) > 0 {
			redacted := []byte(e.redact(string(event.Input)))
			if json.Valid(redacted) {
				event.Input = json.RawMessage(redacted)
			} else {
				event.Input = json.RawMessage(`{"redacted":true}`)
			}
		}
	}
	event.Version = headlessEventVersion
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	e.err = e.encoder.Encode(event)
}

func (e *jsonlEmitter) SetRedactor(redact func(string) string) {
	e.mu.Lock()
	e.redact = redact
	e.mu.Unlock()
}

func (e *jsonlEmitter) callbacks() contract.Callbacks {
	return contract.Callbacks{
		Status:      func(text string) { e.emit(headlessEvent{Type: "status", Text: text}) },
		Notice:      func(text string) { e.emit(headlessEvent{Type: "notice", Text: text}) },
		Token:       func(text string) { e.emit(headlessEvent{Type: "assistant_delta", Text: text}) },
		StreamReset: func() { e.emit(headlessEvent{Type: "assistant_reset"}) },
		ToolStart: func(name string, input json.RawMessage) {
			e.emit(headlessEvent{Type: "tool_start", Name: name, Input: append(json.RawMessage(nil), input...)})
		},
		ToolOutput: func(name, text string) { e.emit(headlessEvent{Type: "tool_delta", Name: name, Text: text}) },
		ToolEnd:    func(name, text string) { e.emit(headlessEvent{Type: "tool_end", Name: name, Text: text}) },
		Confirm:    func(context.Context, string) (bool, error) { return false, nil },
		Ask: func(context.Context, []contract.Question) ([]contract.Answer, error) {
			return nil, errors.New("user input is required, but JSONL mode is non-interactive")
		},
	}
}

func (e *jsonlEmitter) terminal(answer string, stats contract.TaskStats, runErr error) {
	event := headlessEvent{Type: "task_terminal", Answer: answer, Stats: &stats}
	if runErr != nil {
		event.Error = runErr.Error()
	}
	e.emit(event)
}

func (e *jsonlEmitter) Err() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}
