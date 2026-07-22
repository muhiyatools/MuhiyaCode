package evidence

import (
	"strings"
	"testing"
)

func TestReduceJSONIsTypedBoundedQueryAwareAndRetainsArtifact(t *testing.T) {
	card := ReduceJSON(JSONReductionInput{Kind: "mcp", Status: "success", Complete: true, Artifact: "artifact_x", Query: "needle", Raw: []byte(`{"z":1,"match":{"needle":true},"a":"x"}`)})
	rendered := card.Render()
	if !strings.Contains(rendered, "artifact=artifact_x") || strings.Index(rendered, "needle") > strings.Index(rendered, "$.a") {
		t.Fatalf("rendered=%q", rendered)
	}
	invalid := ReduceJSON(JSONReductionInput{Kind: "web", Raw: []byte(`{"broken"`)})
	if !strings.Contains(invalid.Render(), "invalid_json") {
		t.Fatalf("invalid=%+v", invalid)
	}
}

func TestReduceJSONBoundsDeepPayloads(t *testing.T) {
	deep := []byte(`{"a":{"b":{"c":{"d":{"e":{"f":{"g":{"h":{"i":{"j":1}}}}}}}}}}`)
	card := ReduceJSON(JSONReductionInput{Kind: "web", Status: "success", Complete: true, Raw: deep})
	if card.OmittedLines < 0 || !strings.Contains(card.Render(), "depth-or-item-limit") {
		t.Fatalf("deep card=%+v", card)
	}
}
