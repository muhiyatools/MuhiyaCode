package evidence

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeature014EvidenceFixtureMatrix(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "specs", "014-token-economy-overhaul", "benchmarks", "fixtures", "evidence-results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		Cases []struct {
			ID        string          `json:"id"`
			Kind      string          `json:"kind"`
			Adapter   string          `json:"adapter"`
			Status    string          `json:"status"`
			RawText   string          `json:"raw_text"`
			RawBase64 string          `json:"raw_base64"`
			ExitCode  *int            `json:"exit_code"`
			Complete  bool            `json:"complete"`
			TimedOut  bool            `json:"timed_out"`
			RawJSON   json.RawMessage `json:"raw_json"`
			Metadata  json.RawMessage `json:"metadata"`
			Expect    struct {
				Protected []string `json:"protected"`
				MaxTokens int      `json:"max_tokens"`
			} `json:"expect"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(body, &suite); err != nil || len(suite.Cases) != 12 {
		t.Fatalf("fixture decode cases=%d err=%v", len(suite.Cases), err)
	}
	store, _ := NewStore(t.TempDir(), 1<<20)
	for _, fixture := range suite.Cases {
		t.Run(fixture.ID, func(t *testing.T) {
			raw := []byte(fixture.RawText)
			if fixture.RawBase64 != "" {
				raw, err = base64.StdEncoding.DecodeString(fixture.RawBase64)
				if err != nil {
					t.Fatal(err)
				}
			} else if len(fixture.RawJSON) > 0 {
				raw = fixture.RawJSON
			}
			metadata, err := store.Put(PutInput{SessionID: "s", WorkspaceID: "w", Source: fixture.ID, Status: fixture.Status, Complete: fixture.Complete, Content: raw})
			if err != nil {
				t.Fatal(err)
			}
			facts := []string{string(fixture.Metadata)}
			var card ObservationCard
			switch fixture.Kind {
			case "process":
				exit := 0
				if fixture.ExitCode != nil {
					exit = *fixture.ExitCode
				}
				card = ReduceProcess(ProcessReductionInput{Adapter: fixture.Adapter, Status: fixture.Status, ExitCode: exit, Complete: fixture.Complete, TimedOut: fixture.TimedOut, Artifact: metadata.Handle, Text: string(raw), Facts: facts})
			case "diff":
				card = ReduceDiff(DiffReductionInput{Status: fixture.Status, Complete: fixture.Complete, Artifact: metadata.Handle, Text: string(raw), Facts: facts})
			case "file_read", "search":
				card = ReduceFile(FileReductionInput{Kind: fixture.Kind, Source: fixture.ID, Status: fixture.Status, Complete: fixture.Complete, Artifact: metadata.Handle, Text: string(raw), Facts: facts})
			case "json":
				card = ReduceJSON(JSONReductionInput{Kind: fixture.Adapter, Status: fixture.Status, Complete: fixture.Complete, Artifact: metadata.Handle, Raw: raw})
			}
			card.MaxTokens = fixture.Expect.MaxTokens
			rendered := card.Render()
			if !strings.Contains(rendered, "status="+fixture.Status) || !strings.Contains(rendered, metadata.Handle) {
				t.Fatalf("identity lost: %q", rendered)
			}
			for _, protected := range fixture.Expect.Protected {
				if !strings.Contains(rendered, protected) {
					t.Errorf("protected fact %q missing from %q", protected, rendered)
				}
			}
			if fetched, _, err := store.Fetch(metadata.Handle, "s", "w", 0, int64(len(raw))); err != nil || string(fetched) != string(raw) {
				t.Fatalf("raw round-trip bytes=%d err=%v", len(fetched), err)
			}
		})
	}
}
