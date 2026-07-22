package orchestrator

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReplacePrefixIgnoresNilTools(t *testing.T) {
	registry := NewRegistry(&schemaTool{name: "base"})
	registry.ReplacePrefix("mcp__", nil, &schemaTool{name: "mcp__valid"}, nil)
	if !registry.Has("base") || !registry.Has("mcp__valid") {
		t.Fatalf("names=%v", registry.Names())
	}
}

func TestCapToolOutputPreservesUTF8(t *testing.T) {
	value := strings.Repeat("😀", 100)
	result := CapToolOutput(value, 73)
	if !utf8.ValidString(result) {
		t.Fatalf("truncated output is invalid UTF-8: %q", result)
	}
}
