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

// DeclaresReadOnly reports whether the named tool has declared itself
// non-mutating (contract.ReadOnlyDeclaring). Queried live rather than cached at
// registration: an MCP tool learns its annotation when its server connects,
// which for a lazily-connected server happens after the tool is registered.
// Unknown tools and tools that do not implement the interface answer false, so
// every caller stays fail-closed.
func (r *Registry) DeclaresReadOnly(name string) bool {
	r.mu.RLock()
	tool := r.tools[name]
	r.mu.RUnlock()
	declaring, ok := tool.(contract.ReadOnlyDeclaring)
	return ok && declaring.DeclaresReadOnly()
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
		// H8: a tool whose name matches the mcp__ prefix is most likely a
		// dead MCP definition (server disconnected mid-task). Surface a
		// clear message instead of the generic "unknown tool" so the model
		// does not waste turns retrying it.
		if strings.HasPrefix(name, "mcp__") {
			//lint:ignore ST1005 model-facing instruction, not a wrapped Go error
			return "", fmt.Errorf("MCP tool %s is unavailable (server disconnected). Do not retry it this task; use another approach.", name)
		}
		// 004 US3 (T034): point a hallucinated tool name at the closest real one so
		// the model corrects in one step instead of guessing again.
		if suggestion := r.nearestToolName(name, allowed); suggestion != "" {
			//lint:ignore ST1005 model-facing instruction, not a wrapped Go error
			return "", fmt.Errorf("unknown tool %s. Closest available: %s.", name, suggestion)
		}
		return "", fmt.Errorf("unknown tool %s", name)
	}
	return tool.Execute(ctx, arguments)
}

// nearestToolName returns the registered (and, if a filter is set, allowed) tool
// name closest to the given name within an edit distance of 3, or "" when none
// is close enough. Iteration follows insertion order and ties break toward the
// smaller edit distance then the earlier-registered name, so the suggestion is
// deterministic.
func (r *Registry) nearestToolName(name string, allowed map[string]bool) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	best, bestDist := "", 4
	for _, candidate := range r.order {
		if allowed != nil && !allowed[candidate] {
			continue
		}
		if d := levenshtein(name, candidate); d < bestDist {
			best, bestDist = candidate, d
		}
	}
	return best
}

// levenshtein is the classic edit distance between two short tool names, used
// only to suggest a correction for an unknown-tool error.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(min(prev[j]+1, curr[j-1]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
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
