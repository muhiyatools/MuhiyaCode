package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const deferredCatalogPageSize = 25

type deferredToolSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Active      bool   `json:"active"`
}

type toolActivationInput struct {
	Names  []string `json:"names"`
	Query  string   `json:"query"`
	Offset int      `json:"offset"`
}

type integrationToolInput struct {
	Action    string          `json:"action"`
	Name      string          `json:"name"`
	Query     string          `json:"query"`
	Offset    int             `json:"offset"`
	Arguments json.RawMessage `json:"arguments"`
}

// integrationTools keeps one stable schema in the main prompt while retaining
// access to every configured MCP tool. Listing returns compact catalog entries;
// calling validates the exact catalog name and forwards only that tool.
func (e *Engine) integrationTools(ctx context.Context, raw json.RawMessage) (string, error) {
	var input integrationToolInput
	if err := decodeToolArgs(raw, &input); err != nil {
		return "", err
	}
	catalog := e.registry.MCPDefinitions(nil)
	switch strings.ToLower(strings.TrimSpace(input.Action)) {
	case "list":
		active := map[string]bool{}
		if query := strings.TrimSpace(input.Query); query != "" {
			requested := requestedDeferredTools(toolActivationInput{Query: query}, catalog)
			filtered := make([]contract.ToolDefinition, 0, len(requested))
			for _, definition := range catalog {
				if requested[definition.Function.Name] {
					filtered = append(filtered, definition)
				}
			}
			catalog = filtered
		}
		return renderDeferredCatalog(catalog, active, input.Offset)
	case "call":
		name := strings.TrimSpace(input.Name)
		if !strings.HasPrefix(name, "mcp__") {
			return "", fmt.Errorf("integration tool name must start with mcp__")
		}
		if unknown := unknownDeferredTools(map[string]bool{name: true}, catalog); len(unknown) > 0 {
			return "", fmt.Errorf("unknown integration tool %q; list integrations first", name)
		}
		arguments := input.Arguments
		if len(arguments) == 0 {
			arguments = json.RawMessage(`{}`)
		}
		result := e.registry.Execute(ctx, name, arguments, map[string]bool{name: true})
		if result.Err != nil {
			return result.Output, result.Err
		}
		return result.Output, nil
	default:
		return "", fmt.Errorf("integration_tools action must be list or call")
	}
}

func (e *Engine) activateTools(ctx context.Context, raw json.RawMessage) (string, error) {
	var input toolActivationInput
	if err := decodeToolArgs(raw, &input); err != nil {
		return "", err
	}
	catalog := e.registry.MCPDefinitions(nil)
	active := e.activeDeferredTools()
	requested := requestedDeferredTools(input, catalog)
	if unknown := unknownDeferredTools(requested, catalog); len(unknown) > 0 {
		return "", fmt.Errorf("unknown deferred tool names: %s; call activate_tools with no arguments to list the catalog", strings.Join(unknown, ", "))
	}
	activated := newlyActivatedTools(requested, active)
	if err := e.commitToolActivation(ctx, activated); err != nil {
		return "", err
	}
	if len(requested) == 0 {
		return renderDeferredCatalog(catalog, active, input.Offset)
	}
	return renderActivationResult(active, activated)
}

func (e *Engine) activeDeferredTools() map[string]bool {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	active := make(map[string]bool, len(e.taskActiveDeferred))
	for name := range e.taskActiveDeferred {
		active[name] = true
	}
	return active
}

func requestedDeferredTools(input toolActivationInput, catalog []contract.ToolDefinition) map[string]bool {
	requested := requestedToolNames(input.Names)
	query := strings.ToLower(strings.TrimSpace(input.Query))
	if query == "" {
		return requested
	}
	for _, definition := range catalog {
		haystack := strings.ToLower(definition.Function.Name + " " + definition.Function.Description)
		if strings.Contains(haystack, query) {
			requested[definition.Function.Name] = true
		}
	}
	return requested
}

func requestedToolNames(names []string) map[string]bool {
	requested := make(map[string]bool)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			requested[name] = true
		}
	}
	return requested
}

func unknownDeferredTools(requested map[string]bool, catalog []contract.ToolDefinition) []string {
	known := make(map[string]bool, len(catalog))
	for _, definition := range catalog {
		known[definition.Function.Name] = true
	}
	var unknown []string
	for name := range requested {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func newlyActivatedTools(requested, active map[string]bool) []string {
	var activated []string
	for name := range requested {
		if !active[name] {
			activated = append(activated, name)
		}
	}
	sort.Strings(activated)
	return activated
}

func (e *Engine) commitToolActivation(ctx context.Context, activated []string) error {
	if len(activated) == 0 {
		return nil
	}
	if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
		Cause:      contract.InvalidationToolsetChange,
		Trigger:    contract.InvalidationBoundary,
		Scope:      "activated deferred tools: " + strings.Join(activated, ", "),
		RequestSeq: e.nextRequestSeq(),
	}); err != nil {
		return fmt.Errorf("record deferred tool activation: %w", err)
	}
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	if e.taskActiveDeferred == nil {
		e.taskActiveDeferred = make(map[string]bool)
	}
	for _, name := range activated {
		e.taskActiveDeferred[name] = true
	}
	return nil
}

func renderActivationResult(active map[string]bool, activated []string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"activated":   activated,
		"active":      sortedActiveNames(active, activated),
		"instruction": "Activated tools are advertised on the next turn; call them then.",
	})
	return string(payload), err
}

func sortedActiveNames(previous map[string]bool, activated []string) []string {
	names := make([]string, 0, len(previous)+len(activated))
	for name := range previous {
		names = append(names, name)
	}
	names = append(names, activated...)
	sort.Strings(names)
	return names
}

func renderDeferredCatalog(catalog []contract.ToolDefinition, active map[string]bool, offset int) (string, error) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(catalog) {
		offset = len(catalog)
	}
	end := min(len(catalog), offset+deferredCatalogPageSize)
	payload := map[string]any{
		"deferredTools": deferredToolSummaries(catalog[offset:end], active),
		"instruction":   "Call integration_tools with action=call, the exact name, and its arguments.",
	}
	if end < len(catalog) {
		payload["nextOffset"] = end
	}
	raw, err := json.Marshal(payload)
	return string(raw), err
}

func deferredToolSummaries(definitions []contract.ToolDefinition, active map[string]bool) []deferredToolSummary {
	summaries := make([]deferredToolSummary, 0, len(definitions))
	for _, definition := range definitions {
		description := strings.TrimSpace(definition.Function.Description)
		if len(description) > 120 {
			description = description[:120] + "..."
		}
		summaries = append(summaries, deferredToolSummary{
			Name:        definition.Function.Name,
			Description: description,
			Active:      active[definition.Function.Name],
		})
	}
	return summaries
}
