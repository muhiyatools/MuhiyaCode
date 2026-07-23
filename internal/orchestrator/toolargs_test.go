package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDecodeToolArgsRepairsRegexEscapes locks in the live tool-args fix: a model
// writing regex/paths verbatim into JSON string args ("\s", "\d") no longer
// hard-fails the tool call; valid JSON passes through untouched.
func TestDecodeToolArgsRepairsRegexEscapes(t *testing.T) {
	var v struct {
		Note string `json:"note"`
	}
	raw := json.RawMessage(`{"note":"Verify: grep 'fontSize:\s*\d+' src"}`)
	if err := decodeToolArgs(raw, &v); err != nil {
		t.Fatalf("escape rescue failed: %v", err)
	}
	if !strings.Contains(v.Note, `\s*\d+`) {
		t.Fatalf("repaired value = %q, want the literal regex preserved", v.Note)
	}
	var w struct {
		A string `json:"a"`
	}
	if err := decodeToolArgs(json.RawMessage(`{"a":"line\nbreak \"q\""}`), &w); err != nil || w.A != "line\nbreak \"q\"" {
		t.Fatalf("valid JSON mishandled: err=%v value=%q", err, w.A)
	}
}
