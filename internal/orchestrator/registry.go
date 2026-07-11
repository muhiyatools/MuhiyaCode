package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]contract.Tool
	order []string
}

func NewRegistry(tools ...contract.Tool) *Registry {
	r := &Registry{tools: make(map[string]contract.Tool)}
	for _, tool := range tools {
		r.Add(tool)
	}
	return r
}

func (r *Registry) Add(tool contract.Tool) {
	if tool == nil {
		return
	}
	name := tool.Definition().Function.Name
	if name == "" {
		return
	}
	r.mu.Lock()
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = tool
	r.mu.Unlock()
}

func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	_, ok := r.tools[name]
	r.mu.RUnlock()
	return ok
}

func (r *Registry) RemovePrefix(prefix string) {
	r.mu.Lock()
	next := r.order[:0]
	for _, name := range r.order {
		if strings.HasPrefix(name, prefix) {
			delete(r.tools, name)
			continue
		}
		next = append(next, name)
	}
	r.order = next
	r.mu.Unlock()
}

func (r *Registry) ReplacePrefix(prefix string, tools ...contract.Tool) {
	r.mu.Lock()
	next := r.order[:0]
	for _, name := range r.order {
		if strings.HasPrefix(name, prefix) {
			delete(r.tools, name)
			continue
		}
		next = append(next, name)
	}
	r.order = next
	sort.Slice(tools, func(i, j int) bool { return tools[i].Definition().Function.Name < tools[j].Definition().Function.Name })
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		name := tool.Definition().Function.Name
		if name == "" || !strings.HasPrefix(name, prefix) {
			continue
		}
		r.tools[name] = tool
		r.order = append(r.order, name)
	}
	r.mu.Unlock()
}

func (r *Registry) Definitions(allowed map[string]bool) []contract.ToolDefinition {
	base := r.BaseDefinitions(allowed)
	return append(base, r.MCPDefinitions(allowed)...)
}

func (r *Registry) BaseDefinitions(allowed map[string]bool) []contract.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]contract.ToolDefinition, 0, len(r.order))
	for _, name := range r.order {
		if strings.HasPrefix(name, "mcp__") {
			continue
		}
		if allowed != nil && !allowed[name] {
			continue
		}
		result = append(result, r.tools[name].Definition())
	}
	return result
}

func (r *Registry) MCPDefinitions(allowed map[string]bool) []contract.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for name := range r.tools {
		if strings.HasPrefix(name, "mcp__") && (allowed == nil || allowed[name]) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	result := make([]contract.ToolDefinition, 0, len(names))
	for _, name := range names {
		result = append(result, r.tools[name].Definition())
	}
	return result
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

func (r *Registry) Execute(ctx context.Context, name string, arguments json.RawMessage, allowed map[string]bool) (string, error) {
	if allowed != nil && !allowed[name] {
		return "", fmt.Errorf("tool %s is not available to this agent", name)
	}
	r.mu.RLock()
	tool := r.tools[name]
	r.mu.RUnlock()
	if tool == nil {
		return "", fmt.Errorf("unknown tool %s", name)
	}
	return tool.Execute(ctx, arguments)
}

func CapToolOutput(output string, cap int) string {
	if cap <= 0 || len(output) <= cap {
		return output
	}
	if marker := strings.Index(output, "\n--- diff ---\n"); marker >= 0 && marker < cap {
		cut := strings.LastIndex(output[:cap], "\n")
		if cut <= marker {
			cut = cap
		}
		return output[:cut] + fmt.Sprintf("\n... [diff truncated, %d chars omitted]", len(output)-cut)
	}
	head := output[:cap*7/10]
	tail := output[len(output)-cap*2/10:]
	return head + fmt.Sprintf("\n... [%d %s (grep/offset/limit) if needed] ...\n", len(output)-len(head)-len(tail), OutputTruncatedMarker) + tail
}

func IsToolFailure(output string, err error) bool {
	if err != nil {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(output))
	prefixes := []string{"edit failed", "patch failed", "invalid tool", "invalid arguments", "permission denied", "blocked:", "unknown tool", "tool failed"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func toolNames(definitions []contract.ToolDefinition) []string {
	result := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, definition.Function.Name)
	}
	sort.Strings(result)
	return result
}
