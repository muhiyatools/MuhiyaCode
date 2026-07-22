package orchestrator

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type KnowledgeFact struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Phase string `json:"phase,omitempty"`
	Role  string `json:"role,omitempty"`
	Scope string `json:"scope,omitempty"`
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
	mu             sync.Mutex
	facts          []KnowledgeFact
	files          map[string]string
	epoch          int
	persist        func(KnowledgeSnapshot) error
	lastPersistErr error
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
	k.AddPhaseReport(agent, "", agent, title, task, report)
}

func (k *Knowledge) AddPhaseReport(agent, phase, role, title, task, report string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	key := TaskKey(agent, task)
	filtered := k.facts[:0]
	for _, fact := range k.facts {
		if fact.Key != key {
			filtered = append(filtered, fact)
		}
	}
	k.facts = append(filtered, KnowledgeFact{Kind: "agent_report", Key: key, Phase: phase, Role: role, Scope: contract.TruncateEllipsis(strings.TrimSpace(task), 1000), Title: title, Text: contract.Digest(report, 500), Full: contract.TruncateEllipsis(report, 4000), Epoch: k.epoch, At: time.Now().UTC().Format(time.RFC3339Nano)})
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
			lines = append(lines, fmt.Sprintf("- [%s] %s", fact.Title, contract.Digest(fact.Text, 260)))
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
	return contract.TruncateEllipsis(strings.Join(lines, "\n"), maxChars)
}

// BriefingForScope passes only facts/files that overlap the requested scope.
// It is the handoff seam between phases: bounded digests, never transcripts or
// predecessor full reports.
func (k *Knowledge) BriefingForScope(scope string, maxChars int) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	if maxChars <= 0 {
		maxChars = 1500
	}
	terms := scopeTerms(scope)
	var lines []string
	for i := len(k.facts) - 1; i >= 0 && len(lines) < 6; i-- {
		fact := k.facts[i]
		candidate := strings.ToLower(fact.Title + " " + fact.Scope + " " + fact.Text)
		if !termsOverlap(terms, candidate) {
			continue
		}
		if len(lines) == 0 {
			lines = append(lines, "Relevant banked findings:")
		}
		label := strings.Trim(strings.Join([]string{fact.Phase, fact.Role}, "/"), "/")
		if label == "" {
			label = fact.Title
		} else {
			label += ": " + fact.Title
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", label, contract.Digest(fact.Text, 320)))
	}
	var files []string
	for path, note := range k.files {
		if termsOverlap(terms, strings.ToLower(path)) {
			files = append(files, fmt.Sprintf("%s (%s)", path, note))
		}
	}
	sort.Strings(files)
	if len(files) > 12 {
		files = files[:12]
	}
	if len(files) > 0 {
		lines = append(lines, "Relevant files already inspected: "+strings.Join(files, ", "))
	}
	return contract.TruncateEllipsis(strings.Join(lines, "\n"), maxChars)
}

func scopeTerms(value string) map[string]bool {
	terms := make(map[string]bool)
	for _, field := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '.' && r != '/' && r != '\\'
	}) {
		if len(field) >= 4 {
			terms[field] = true
		}
	}
	return terms
}

func termsOverlap(terms map[string]bool, candidate string) bool {
	if len(terms) == 0 {
		return false
	}
	for term := range terms {
		if strings.Contains(candidate, term) {
			return true
		}
	}
	return false
}

func (k *Knowledge) CompactionFacts() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	start := max(0, len(k.facts)-8)
	result := make([]string, 0, len(k.facts)-start)
	for _, fact := range k.facts[start:] {
		result = append(result, fmt.Sprintf("- agent finding [%s]: %s", fact.Title, contract.Digest(fact.Text, 240)))
	}
	return result
}

// Findings returns a bounded copy of the newest banked agent reports for the
// durable execution-plan artifact. Full reports remain out of the main prompt;
// the artifact receives only their digested, cited summaries.
func (k *Knowledge) Findings(limit int) []KnowledgeFact {
	k.mu.Lock()
	defer k.mu.Unlock()
	if limit <= 0 {
		limit = 8
	}
	start := max(0, len(k.facts)-limit)
	result := make([]KnowledgeFact, 0, len(k.facts)-start)
	for _, fact := range k.facts[start:] {
		if fact.Kind == "agent_report" {
			fact.Full = ""
			result = append(result, fact)
		}
	}
	return result
}

// ResearchFindings returns only current-epoch facts with research PROVENANCE
// (role "research-scope" — the explore/plan read-only kinds), regardless of
// which pipeline phase banked them: an investigation subagent launched from
// the PLAN phase is research evidence too (live fix — a phase-tagged filter
// rejected plan-phase findings, making the grounding gap unsatisfiable).
// Implementation/review reports (role implement-step/review) and any stale
// pre-edit facts still never satisfy the research-grounding requirement.
func (k *Knowledge) ResearchFindings(limit int) []KnowledgeFact {
	k.mu.Lock()
	defer k.mu.Unlock()
	if limit <= 0 {
		limit = 8
	}
	result := make([]KnowledgeFact, 0, limit)
	for i := len(k.facts) - 1; i >= 0 && len(result) < limit; i-- {
		fact := k.facts[i]
		if fact.Kind == "agent_report" && fact.Role == "research-scope" && fact.Epoch == k.epoch {
			fact.Full = ""
			result = append(result, fact)
		}
	}
	// Restore chronological order (the scan above walks newest-first).
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

func (k *Knowledge) Snapshot() KnowledgeSnapshot {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.snapshotLocked()
}

func (k *Knowledge) RetryPersistence() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.lastPersistErr != nil {
		k.saveLocked()
	}
	return k.lastPersistErr
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
		k.lastPersistErr = k.persist(k.snapshotLocked())
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

// RelatedToSession reports whether a prompt shares scope terms with anything
// this session has already worked on — banked reports or touched files. It is
// the cheap, model-free signal behind the fresh-session advisory: a large task
// with no overlap is probably different work, and a new session would give it
// a clean model choice and an uncluttered cache.
//
// An empty knowledge bank returns true (not related-unknown): with nothing to
// compare against, the harness must not nag.
func (k *Knowledge) RelatedToSession(prompt string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.facts) == 0 && len(k.files) == 0 {
		return true
	}
	terms := scopeTerms(prompt)
	if len(terms) == 0 {
		return true
	}
	for _, fact := range k.facts {
		if termsOverlap(terms, fact.Title+" "+fact.Scope+" "+fact.Text) {
			return true
		}
	}
	for path := range k.files {
		if termsOverlap(terms, path) {
			return true
		}
	}
	return false
}
