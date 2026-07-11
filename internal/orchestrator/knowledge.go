package orchestrator

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

type KnowledgeFact struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Title string `json:"title"`
	Text  string `json:"text"`
	Full  string `json:"full"`
	Epoch int    `json:"epoch"`
	At    string `json:"at"`
}

type KnowledgeSnapshot struct {
	Version   int               `json:"version"`
	Facts     []KnowledgeFact   `json:"facts"`
	Files     map[string]string `json:"files"`
	EditEpoch int               `json:"editEpoch"`
}

type Knowledge struct {
	mu      sync.Mutex
	facts   []KnowledgeFact
	files   map[string]string
	epoch   int
	persist func(KnowledgeSnapshot) error
}

func NewKnowledge(snapshot KnowledgeSnapshot, persist func(KnowledgeSnapshot) error) *Knowledge {
	if snapshot.Version != 1 {
		snapshot = KnowledgeSnapshot{Version: 1}
	}
	if snapshot.Files == nil {
		snapshot.Files = make(map[string]string)
	}
	return &Knowledge{facts: append([]KnowledgeFact(nil), snapshot.Facts...), files: snapshot.Files, epoch: snapshot.EditEpoch, persist: persist}
}

func (k *Knowledge) AddReport(agent, title, task, report string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	key := TaskKey(agent, task)
	filtered := k.facts[:0]
	for _, fact := range k.facts {
		if fact.Key != key {
			filtered = append(filtered, fact)
		}
	}
	k.facts = append(filtered, KnowledgeFact{Kind: "agent_report", Key: key, Title: title, Text: digest(report, 500), Full: truncateEllipsis(report, 4000), Epoch: k.epoch, At: time.Now().UTC().Format(time.RFC3339Nano)})
	if len(k.facts) > 40 {
		k.facts = k.facts[len(k.facts)-40:]
	}
	k.saveLocked()
}

func (k *Knowledge) Reusable(agent, task string) (KnowledgeFact, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	key := TaskKey(agent, task)
	for i := len(k.facts) - 1; i >= 0; i-- {
		fact := k.facts[i]
		if fact.Kind == "agent_report" && fact.Key == key && fact.Epoch == k.epoch {
			return fact, true
		}
	}
	return KnowledgeFact{}, false
}

func (k *Knowledge) NoteFile(path, note string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	path = canonicalKey(path)
	k.files[path] = note
	if len(k.files) > 80 {
		// Maps have no stable age; cap deterministically by removing one key.
		for key := range k.files {
			delete(k.files, key)
			break
		}
	}
	if note == "edited" {
		k.epoch++
	}
	k.saveLocked()
}

func (k *Knowledge) MarkWorkspaceChanged() {
	k.mu.Lock()
	k.epoch++
	k.saveLocked()
	k.mu.Unlock()
}

func (k *Knowledge) Briefing(maxChars int) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	if maxChars <= 0 {
		maxChars = 1500
	}
	var lines []string
	start := max(0, len(k.facts)-4)
	if start < len(k.facts) {
		lines = append(lines, "Earlier session findings (trust unless contradicted):")
		for _, fact := range k.facts[start:] {
			lines = append(lines, fmt.Sprintf("- [%s] %s", fact.Title, digest(fact.Text, 260)))
		}
	}
	if len(k.files) > 0 {
		entries := make([]string, 0, min(len(k.files), 25))
		for path, note := range k.files {
			entries = append(entries, fmt.Sprintf("%s (%s)", path, note))
			if len(entries) == 25 {
				break
			}
		}
		lines = append(lines, "Files already inspected: "+strings.Join(entries, ", "))
	}
	return truncateEllipsis(strings.Join(lines, "\n"), maxChars)
}

func (k *Knowledge) CompactionFacts() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	start := max(0, len(k.facts)-8)
	result := make([]string, 0, len(k.facts)-start)
	for _, fact := range k.facts[start:] {
		result = append(result, fmt.Sprintf("- agent finding [%s]: %s", fact.Title, digest(fact.Text, 240)))
	}
	return result
}

func (k *Knowledge) Snapshot() KnowledgeSnapshot {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.snapshotLocked()
}

func (k *Knowledge) snapshotLocked() KnowledgeSnapshot {
	files := make(map[string]string, len(k.files))
	for key, value := range k.files {
		files[key] = value
	}
	return KnowledgeSnapshot{Version: 1, Facts: append([]KnowledgeFact(nil), k.facts...), Files: files, EditEpoch: k.epoch}
}

func (k *Knowledge) saveLocked() {
	if k.persist != nil {
		_ = k.persist(k.snapshotLocked())
	}
}

func TaskKey(agent, task string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(task), " "))
	units := utf16.Encode([]rune(normalized))
	var hash uint32 = 5381
	for _, unit := range units {
		hash = hash*33 + uint32(unit)
	}
	prefix := units
	if len(prefix) > 400 {
		prefix = prefix[:400]
	}
	return fmt.Sprintf("%s:%s#%s", agent, string(utf16.Decode(prefix)), strconv.FormatUint(uint64(hash), 36))
}

func digest(value string, maxChars int) string {
	return truncateEllipsis(strings.Join(strings.Fields(value), " "), maxChars)
}

func truncateEllipsis(value string, maxChars int) string {
	runes := []rune(value)
	if maxChars <= 0 || len(runes) <= maxChars {
		return value
	}
	if maxChars == 1 {
		return "…"
	}
	return string(runes[:maxChars-1]) + "…"
}
