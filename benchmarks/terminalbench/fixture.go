package main

// fixture.go builds a fixed-seed workspace/session for the terminalbench Phase 1
// measurement harness (feature 005, tasks T001+T002; contract §7). It populates
// a real SQLite state database plus the session sidecars the terminal must page
// through: a transcript JSONL, a context-bounded history.json with tool-call /
// result pairs, and knowledge/usage/invalidation sidecars. All randomness is
// drawn from a single seeded *math/rand.Rand and all timestamps derive from a
// fixed epoch, so the same seed always yields the same fixture hash. The hash is
// computed over the LOGICAL content (not the physical DB bytes, which carry a
// random session id and wall-clock write times) so it is reproducible.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
	"github.com/muhiya/muhiyacode/internal/state"
)

// fixtureEpoch anchors every generated timestamp. Using a constant (never
// time.Now) keeps the fixture hash independent of the wall clock, as the
// contract §7 requires ("no wall-clock in the fixture hash").
var fixtureEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// eventRole pairs a role with its event type for the mixed event stream the
// contract §7 fixture must contain (user/assistant/tool/agent/status/error).
type eventRole struct {
	Role string
	Type string
}

var fixtureRoles = []eventRole{
	{"user", "message"},
	{"assistant", "message"},
	{"tool", "tool_result"},
	{"agent", "agent_event"},
	{"status", "status"},
	{"error", "error"},
}

// genEvent is one logical transcript event. The stable index doubles as the
// stable event cursor the transcript page interface (contract §2) walks.
type genEvent struct {
	Index   int    `json:"index"`
	Role    string `json:"role"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Bytes   int    `json:"bytes"`
	Large   bool   `json:"large"`
}

// fixtureData is the complete logical fixture. It is what the fixture hash is
// computed over, and it is the in-memory source the scenario runners page
// through without re-reading disk.
type fixtureData struct {
	Seed          int64                          `json:"seed"`
	EventCount    int                            `json:"event_count"`
	Events        []genEvent                     `json:"events"`
	Transcript    []map[string]any               `json:"transcript"`
	History       orchestrator.HistorySnapshot   `json:"history"`
	Knowledge     orchestrator.KnowledgeSnapshot `json:"knowledge"`
	Usage         []contract.UsageRecord         `json:"usage"`
	Invalidations []contract.InvalidationEvent   `json:"invalidations"`
}

// Fixture is the materialized result: the logical data, the on-disk session
// identity, and the derived hash / size metadata the Result records.
type Fixture struct {
	Data         fixtureData
	Root         string
	SessionID    string
	Seed         int64
	Hash         string
	EventCount   int
	PayloadBytes int64
}

// buildFixtureData deterministically generates the logical fixture from a seed.
// It performs no I/O, so tests can hash it directly for the determinism check.
func buildFixtureData(seed int64, eventCount int) fixtureData {
	rng := rand.New(rand.NewSource(seed))
	data := fixtureData{Seed: seed, EventCount: eventCount}
	data.Events = make([]genEvent, 0, eventCount)
	data.Transcript = make([]map[string]any, 0, eventCount)

	for i := 0; i < eventCount; i++ {
		role := fixtureRoles[rng.Intn(len(fixtureRoles))]
		// A small fraction of entries are oversized (contract §7: "large
		// individual messages/tool outputs"). tool_result and message roles get
		// the big payloads a real session accumulates.
		large := rng.Float64() < 0.02
		var content string
		if large {
			content = largeBlob(rng, 24_000+rng.Intn(72_000))
		} else {
			content = sentence(rng, 6+rng.Intn(30))
		}
		event := genEvent{Index: i, Role: role.Role, Type: role.Type, Content: content, Bytes: len(content), Large: large}
		data.Events = append(data.Events, event)
		data.Transcript = append(data.Transcript, map[string]any{
			"index":   i,
			"role":    role.Role,
			"type":    role.Type,
			"content": content,
			"at":      deriveTime(i).Format(time.RFC3339Nano),
		})
	}

	data.History = buildHistory(rng)
	data.Knowledge = buildKnowledge(rng)
	data.Usage = buildUsage(rng, eventCount)
	data.Invalidations = buildInvalidations(rng)
	return data
}

// hashFixtureData hashes the canonical JSON encoding of the logical fixture.
// encoding/json emits struct fields in declaration order and map keys sorted,
// so the encoding — and therefore the hash — is stable for a given seed.
func hashFixtureData(data fixtureData) string {
	encoded, err := json.Marshal(data)
	if err != nil {
		// The fixture contains only JSON-safe values; a marshal error would be a
		// programming error, so surface it as a poisoned hash rather than panic.
		return "unmarshalable:" + err.Error()
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// payloadBytes is the total logical content the terminal would have to hold and
// page: event content plus every sidecar's encoded size.
func payloadBytes(data fixtureData) int64 {
	var total int64
	for _, event := range data.Events {
		total += int64(len(event.Content))
	}
	for _, sidecar := range []any{data.History, data.Knowledge, data.Usage, data.Invalidations} {
		if encoded, err := json.Marshal(sidecar); err == nil {
			total += int64(len(encoded))
		}
	}
	return total
}

// GenerateFixture builds the logical fixture and writes it to a real SQLite
// database and session sidecars rooted at root (used as MUHIYA_HOME). The
// returned Fixture carries the seed-stable hash and the in-memory data.
func GenerateFixture(ctx context.Context, root string, seed int64, eventCount int) (Fixture, error) {
	data := buildFixtureData(seed, eventCount)
	fixture := Fixture{
		Data:         data,
		Root:         root,
		Seed:         seed,
		Hash:         hashFixtureData(data),
		EventCount:   eventCount,
		PayloadBytes: payloadBytes(data),
	}

	restore := setEnv(state.HomeEnvironment, root)
	defer restore()

	db, err := state.Open(ctx)
	if err != nil {
		return Fixture{}, fmt.Errorf("open state db: %w", err)
	}
	defer db.Close()

	sessions := &state.Sessions{DB: db}
	session, err := sessions.New(ctx, root, "terminalbench fixture")
	if err != nil {
		return Fixture{}, fmt.Errorf("create session: %w", err)
	}
	fixture.SessionID = session.ID

	for _, event := range data.Events {
		if err := db.AddEvent(ctx, session.ID, event.Role, event.Type, event.Content); err != nil {
			return Fixture{}, fmt.Errorf("add event %d: %w", event.Index, err)
		}
	}
	for _, entry := range data.Transcript {
		if err := sessions.AppendTranscript(session.ID, entry); err != nil {
			return Fixture{}, fmt.Errorf("append transcript: %w", err)
		}
	}
	if err := sessions.WriteJSON(session.ID, "history.json", data.History); err != nil {
		return Fixture{}, fmt.Errorf("write history: %w", err)
	}
	if err := sessions.WriteJSON(session.ID, "knowledge.json", data.Knowledge); err != nil {
		return Fixture{}, fmt.Errorf("write knowledge: %w", err)
	}
	for _, record := range data.Usage {
		if err := sessions.AppendUsage(session.ID, record); err != nil {
			return Fixture{}, fmt.Errorf("append usage: %w", err)
		}
	}
	for _, event := range data.Invalidations {
		if err := sessions.AppendInvalidation(session.ID, event); err != nil {
			return Fixture{}, fmt.Errorf("append invalidation: %w", err)
		}
	}
	return fixture, nil
}

// buildHistory produces a context-bounded history.json with realistic
// tool-call / tool-result message pairs (contract §7). It is intentionally kept
// far below the full event count: history is the model's working window, not the
// whole transcript.
func buildHistory(rng *rand.Rand) orchestrator.HistorySnapshot {
	const pairs = 20
	messages := make([]contract.Message, 0, pairs*3+1)
	messages = append(messages, contract.Message{Role: contract.RoleSystem, Content: "You are MuhiyaCode, a terminal coding agent."})
	tools := []string{"read_file", "grep", "bash", "edit_file", "glob"}
	for i := 0; i < pairs; i++ {
		messages = append(messages, contract.Message{Role: contract.RoleUser, Content: sentence(rng, 8+rng.Intn(16))})
		tool := tools[rng.Intn(len(tools))]
		callID := fmt.Sprintf("call_%04d", i)
		args := fmt.Sprintf(`{"path":%q,"query":%q}`, fmt.Sprintf("internal/pkg%d/file.go", i), sentence(rng, 3+rng.Intn(4)))
		call := contract.NewToolCall(callID, tool, args)
		messages = append(messages, contract.Message{Role: contract.RoleAssistant, Content: sentence(rng, 4+rng.Intn(8)), ToolCalls: []contract.ToolCall{call}})
		result := sentence(rng, 20+rng.Intn(60))
		if rng.Float64() < 0.15 {
			result = largeBlob(rng, 8_000+rng.Intn(16_000))
		}
		messages = append(messages, contract.Message{Role: contract.RoleTool, ToolCallID: callID, Content: result})
	}
	return orchestrator.HistorySnapshot{
		Version:           1,
		Messages:          messages,
		LastTaskStart:     len(messages) - 3,
		RewriteVersion:    2,
		LastWindowStart:   0,
		WindowInitialized: true,
	}
}

// buildKnowledge produces the knowledge sidecar with a handful of durable facts
// and a file digest map.
func buildKnowledge(rng *rand.Rand) orchestrator.KnowledgeSnapshot {
	facts := make([]orchestrator.KnowledgeFact, 0, 8)
	for i := 0; i < 8; i++ {
		text := sentence(rng, 10+rng.Intn(20))
		facts = append(facts, orchestrator.KnowledgeFact{
			Kind:  "note",
			Key:   fmt.Sprintf("fact-%02d", i),
			Title: sentence(rng, 3+rng.Intn(4)),
			Text:  text,
			Full:  text + " " + sentence(rng, 8+rng.Intn(12)),
			Epoch: i,
			At:    deriveTime(i * 3).Format(time.RFC3339Nano),
		})
	}
	files := map[string]string{}
	for i := 0; i < 12; i++ {
		files[fmt.Sprintf("internal/pkg%d/file.go", i)] = fmt.Sprintf("%x", sha256.Sum256([]byte(sentence(rng, 6))))[:16]
	}
	return orchestrator.KnowledgeSnapshot{Version: 1, Facts: facts, Files: files, EditEpoch: 8}
}

// buildUsage produces a provider-faithful usage ledger scaled to the event
// count, mixing cold-start and steady-state records so the aggregate has a
// meaningful cache profile.
func buildUsage(rng *rand.Rand, eventCount int) []contract.UsageRecord {
	requests := max(8, eventCount/40)
	records := make([]contract.UsageRecord, 0, requests)
	for seq := 1; seq <= requests; seq++ {
		prompt := 2_000 + rng.Intn(6_000)
		completion := 200 + rng.Intn(1_200)
		read := 0
		miss := prompt
		attribution := contract.CacheAttributionColdStart
		if seq > 1 {
			read = prompt - (200 + rng.Intn(400))
			if read < 0 {
				read = 0
			}
			miss = prompt - read
			attribution = contract.CacheAttributionProvider
		}
		newTail := miss
		records = append(records, contract.UsageRecord{
			Seq:              seq,
			At:               deriveTime(seq * 7),
			Model:            "muhiya-benchmark-model",
			Stream:           contract.UsageStreamMain,
			PromptTokens:     intPtr(prompt),
			CompletionTokens: intPtr(completion),
			CacheReadTokens:  intPtr(read),
			CacheMissTokens:  intPtr(miss),
			NewTailTokens:    intPtr(newTail),
			HitRate:          contract.HitRate(intPtr(read), intPtr(miss)),
			Attribution:      attribution,
		})
	}
	return records
}

// buildInvalidations produces a small set of attributable prefix rewrites.
func buildInvalidations(rng *rand.Rand) []contract.InvalidationEvent {
	causes := []contract.InvalidationCause{contract.InvalidationFold, contract.InvalidationTrim, contract.InvalidationCompact}
	triggers := []contract.InvalidationTrigger{contract.InvalidationPressure, contract.InvalidationUserAction, contract.InvalidationBoundary}
	events := make([]contract.InvalidationEvent, 0, 4)
	for i := 0; i < 4; i++ {
		pressure := 0.5 + rng.Float64()*0.4
		events = append(events, contract.InvalidationEvent{
			At:         deriveTime(i * 11),
			Cause:      causes[i%len(causes)],
			Trigger:    triggers[i%len(triggers)],
			Scope:      "history",
			Pressure:   &pressure,
			RequestSeq: i*3 + 2,
		})
	}
	return events
}

// deriveTime returns a deterministic timestamp offset from the fixed epoch.
func deriveTime(step int) time.Time {
	return fixtureEpoch.Add(time.Duration(step) * time.Second)
}

// sentence builds deterministic pseudo-prose of the requested word count.
func sentence(rng *rand.Rand, words int) string {
	vocab := []string{
		"session", "transcript", "render", "anchor", "prefix", "cache", "token",
		"window", "frame", "scroll", "resume", "endurance", "stream", "fixture",
		"paging", "budget", "overscan", "generation", "cursor", "invalidation",
	}
	parts := make([]string, words)
	for i := range parts {
		parts[i] = vocab[rng.Intn(len(vocab))]
	}
	return strings.Join(parts, " ") + "."
}

// largeBlob builds a deterministic payload of about size bytes, exercising the
// oversized-entry path (contract §2 row-windowing, §7 large outputs).
func largeBlob(rng *rand.Rand, size int) string {
	var builder strings.Builder
	builder.Grow(size)
	for builder.Len() < size {
		builder.WriteString(sentence(rng, 12))
		builder.WriteByte('\n')
	}
	return builder.String()[:size]
}

func intPtr(value int) *int { return &value }

// setEnv sets an environment variable and returns a restore func.
func setEnv(key, value string) func() {
	previous, existed := os.LookupEnv(key)
	_ = os.Setenv(key, value)
	return func() {
		if existed {
			_ = os.Setenv(key, previous)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}
