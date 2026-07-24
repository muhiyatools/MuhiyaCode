package orchestrator

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const (
	maxInspectionSignatures = 400
	maxCoveredFiles         = 200
	maxSegmentsPerFile      = 40
)

// InspectionSnapshot deliberately matches the TypeScript v3 session format.
// Entries is a transitional reader for early Go development snapshots and is
// never written by Snapshot.
type InspectionSnapshot struct {
	Version        int                           `json:"version"`
	Signatures     map[string]InspectionEntry    `json:"signatures"`
	Coverage       map[string]InspectionCoverage `json:"coverage"`
	InspectedFiles []string                      `json:"inspectedFiles"`
	FullReads      int                           `json:"fullReads"`
	Entries        map[string]InspectionEntry    `json:"entries,omitempty"`
}

type InspectionEntry struct {
	CallID string `json:"callId"`
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`

	// Transitional fields from the first Go snapshot shape.
	Tool      string `json:"tool,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	ModTimeNS int64  `json:"modTimeNs,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

type InspectionCoverage struct {
	TotalLines  int                    `json:"totalLines,omitempty"`
	Segments    []InspectionSegment    `json:"segments"`
	Fingerprint *InspectionFingerprint `json:"fingerprint,omitempty"`
}

type InspectionSegment struct {
	Start  int    `json:"start"`
	End    int    `json:"end"`
	CallID string `json:"callId"`
}

type InspectionFingerprint struct {
	MTimeMS float64 `json:"mtimeMs"`
	Size    int64   `json:"size"`
}

type InspectionLedger struct {
	mu         sync.Mutex
	signatures map[string]InspectionEntry
	coverage   map[string]InspectionCoverage
	inspected  map[string]bool
	fullReads  int
	root       string
	persist    func(InspectionSnapshot) error
}

func NewInspection(snapshot InspectionSnapshot, persist func(InspectionSnapshot) error, workspaceRoot ...string) *InspectionLedger {
	if snapshot.Version != 3 {
		snapshot = InspectionSnapshot{Version: 3}
	}
	if snapshot.Signatures == nil {
		snapshot.Signatures = make(map[string]InspectionEntry)
	}
	if snapshot.Coverage == nil {
		snapshot.Coverage = make(map[string]InspectionCoverage)
	}
	// Preserve exact-call memory from early Go snapshots when present.
	for signature, entry := range snapshot.Entries {
		if entry.Kind == "" {
			entry.Kind = "search"
			if entry.Tool == "read_file" {
				entry.Kind = "read"
			}
		}
		snapshot.Signatures[signature] = entry
	}
	root := ""
	if len(workspaceRoot) > 0 && workspaceRoot[0] != "" {
		root, _ = filepath.Abs(workspaceRoot[0])
	}
	inspected := make(map[string]bool)
	for _, path := range snapshot.InspectedFiles {
		inspected[path] = true
	}
	return &InspectionLedger{signatures: snapshot.Signatures, coverage: snapshot.Coverage, inspected: inspected, fullReads: snapshot.FullReads, root: root, persist: persist}
}

// readonlyTools is the dispatch-side dedup ledger's tool set. These are the
// tools whose results flow into the conversation unchanged on a second call
// and therefore pay nothing on a second hit — inspect_code and web_search are
// included because (a) inspect_code is an AST parse that is fully deterministic
// given (mode, path, symbol) and (b) web_search cache-cache of identical queries
// is the dominant token waste on research-heavy tasks. Both fall into the
// "search_kind" record path below, key on `callSignature` (compactJSON), and
// are wiped alongside other search results when any workspace file changes —
// which is the right trade-off because both kinds of results can describe any
// file in the workspace and have no per-file freshness key.
var readonlyTools = map[string]bool{"read_file": true, "list_files": true, "grep": true, "glob": true, "search_text": true, "git_status": true, "git_diff": true, "inspect_code": true, "web_search": true}

func (l *InspectionLedger) Duplicate(call contract.ToolCall, intact func(string) bool) (InspectionEntry, bool) {
	if !readonlyTools[call.ToolName()] {
		return InspectionEntry{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if call.ToolName() != "read_file" {
		// A-4: search dedup (grep/glob/list_files/inspect_code/web_search) is
		// invalidated ONLY by workspace mutations — every agent edit/write/patch/
		// shell clears all search-kind signatures via invalidatePathLocked (see
		// the entry.Kind == "search" branch there). Unlike read_file it carries no
		// per-file mtime gate, so a change made OUTSIDE the agent's tools between
		// turns is not detected; an identical re-search then serves the prior
		// "already ran" block. This is by design: a search spans many files with
		// no single freshness key, and the agent is the workspace's own mutator, so
		// an external mid-session edit is out of scope.
		key := callSignature(call)
		if call.ToolName() == "inspect_code" {
			key = inspectSignature(call)
		}
		entry, ok := l.signatures[key]
		return entry, ok && intact(entry.CallID)
	}
	path := pathArgument(call)
	if path == "" {
		return InspectionEntry{}, false
	}
	key := l.pathKey(path)
	if l.coverageStaleLocked(key) {
		l.invalidatePathLocked(key)
		delete(l.inspected, key)
		l.saveLocked()
		return InspectionEntry{}, false
	}
	if entry, ok := l.signatures[readSignature(call, key)]; ok && intact(entry.CallID) {
		return entry, true
	}
	coverage, ok := l.coverage[key]
	if !ok || coverage.TotalLines <= 0 {
		return InspectionEntry{}, false
	}
	start, end, ok := requestedReadRange(call, coverage.TotalLines)
	if !ok {
		return InspectionEntry{}, false
	}
	segments := coveringSegments(coverage.Segments, start, end)
	if len(segments) == 0 {
		return InspectionEntry{}, false
	}
	for _, segment := range segments {
		if !intact(segment.CallID) {
			return InspectionEntry{}, false
		}
	}
	return InspectionEntry{CallID: segments[0].CallID, Kind: "read", Path: key}, true
}

// Record stores only evidence that is actually present in bounded history.
func (l *InspectionLedger) Record(call contract.ToolCall, output string) {
	if !readonlyTools[call.ToolName()] || strings.Contains(output, OutputTruncatedMarker) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if call.ToolName() != "read_file" {
		key := callSignature(call)
		if call.ToolName() == "inspect_code" {
			key = inspectSignature(call)
		}
		l.addSignatureLocked(key, InspectionEntry{CallID: call.ID, Kind: "search"})
		l.saveLocked()
		return
	}
	path := pathArgument(call)
	start, end, total := parseReadResult(output)
	if headerPath := readResultPath(output); headerPath != "" {
		path = headerPath
	}
	if path == "" {
		return
	}
	key := l.pathKey(path)
	l.inspected[key] = true
	l.addSignatureLocked(readSignature(call, key), InspectionEntry{CallID: call.ID, Kind: "read", Path: key})
	if start <= 0 || end < start || total <= 0 {
		l.saveLocked()
		return
	}
	coverage := l.coverage[key]
	coverage.TotalLines = total
	coverage.Segments = append(coverage.Segments, InspectionSegment{Start: start, End: end, CallID: call.ID})
	if len(coverage.Segments) > maxSegmentsPerFile {
		coverage.Segments = coverage.Segments[len(coverage.Segments)-maxSegmentsPerFile:]
	}
	coverage.Fingerprint = l.fingerprint(key)
	l.coverage[key] = coverage
	if start == 1 && end >= total && total >= 400 {
		l.fullReads++
	}
	l.trimCoverageLocked()
	l.saveLocked()
}

// InvalidateFor forgets stale evidence and returns read result ids whose
// history payloads can now be folded as superseded.
func (l *InspectionLedger) InvalidateFor(call contract.ToolCall) []string {
	name := call.ToolName()
	mutates := name == "edit_file" || name == "multi_edit" || name == "write_file" || name == "apply_patch" || name == "run_shell" || strings.HasPrefix(name, "mcp__")
	if !mutates {
		return nil
	}
	if name == "run_shell" {
		var args struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal([]byte(call.ArgumentsJSON()), &args)
		if IsReadOnlyShell(args.Command) {
			return nil
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var dropped []string
	// apply_patch carries no single "path" argument, but the files it touches are
	// deterministic (the diff's file headers), so invalidate exactly those rather
	// than wiping the whole ledger and superseding reads of untouched files (A-2).
	// Only run_shell and mcp__* — whose effects the harness cannot bound — still
	// take the blanket wipe.
	if name == "apply_patch" {
		for _, target := range contract.ToolTargetPaths(name, []byte(call.ArgumentsJSON())) {
			dropped = append(dropped, l.invalidatePathLocked(l.pathKey(target))...)
		}
		l.saveLocked()
		return uniqueStrings(dropped)
	}
	path := pathArgument(call)
	if path == "" || name == "run_shell" || strings.HasPrefix(name, "mcp__") {
		for _, coverage := range l.coverage {
			for _, segment := range coverage.Segments {
				dropped = append(dropped, segment.CallID)
			}
		}
		l.signatures = make(map[string]InspectionEntry)
		l.coverage = make(map[string]InspectionCoverage)
	} else {
		dropped = l.invalidatePathLocked(l.pathKey(path))
	}
	l.saveLocked()
	return uniqueStrings(dropped)
}

func (l *InspectionLedger) Known(path string) bool {
	key := l.pathKey(path)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.coverageStaleLocked(key) {
		l.invalidatePathLocked(key)
		delete(l.inspected, key)
		l.saveLocked()
		return false
	}
	return l.inspected[key] || len(l.coverage[key].Segments) > 0
}

func (l *InspectionLedger) Snapshot() InspectionSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snapshotLocked()
}

func (l *InspectionLedger) snapshotLocked() InspectionSnapshot {
	signatures := make(map[string]InspectionEntry, len(l.signatures))
	for key, value := range l.signatures {
		signatures[key] = value
	}
	coverage := make(map[string]InspectionCoverage, len(l.coverage))
	for key, value := range l.coverage {
		value.Segments = append([]InspectionSegment(nil), value.Segments...)
		coverage[key] = value
	}
	inspected := make([]string, 0, len(l.inspected))
	for path := range l.inspected {
		inspected = append(inspected, path)
	}
	sort.Strings(inspected)
	if len(inspected) > maxInspectionSignatures {
		inspected = inspected[len(inspected)-maxInspectionSignatures:]
	}
	return InspectionSnapshot{Version: 3, Signatures: signatures, Coverage: coverage, InspectedFiles: inspected, FullReads: l.fullReads}
}

func (l *InspectionLedger) saveLocked() {
	if l.persist != nil {
		_ = l.persist(l.snapshotLocked())
	}
}

func (l *InspectionLedger) addSignatureLocked(key string, entry InspectionEntry) {
	if len(l.signatures) >= maxInspectionSignatures {
		keys := make([]string, 0, len(l.signatures))
		for candidate := range l.signatures {
			keys = append(keys, candidate)
		}
		sort.Strings(keys)
		delete(l.signatures, keys[0])
	}
	l.signatures[key] = entry
}

func (l *InspectionLedger) trimCoverageLocked() {
	if len(l.coverage) <= maxCoveredFiles {
		return
	}
	keys := make([]string, 0, len(l.coverage))
	for key := range l.coverage {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	delete(l.coverage, keys[0])
}

func (l *InspectionLedger) invalidatePathLocked(key string) []string {
	var dropped []string
	for _, segment := range l.coverage[key].Segments {
		dropped = append(dropped, segment.CallID)
	}
	delete(l.coverage, key)
	for signature, entry := range l.signatures {
		if entry.Kind == "search" || entry.Path == key {
			if entry.Kind == "read" {
				dropped = append(dropped, entry.CallID)
			}
			delete(l.signatures, signature)
		}
	}
	return dropped
}

func (l *InspectionLedger) fingerprint(key string) *InspectionFingerprint {
	if l.root == "" {
		return nil
	}
	info, err := os.Stat(filepath.Join(l.root, filepath.FromSlash(key)))
	if err != nil {
		return nil
	}
	return &InspectionFingerprint{MTimeMS: float64(info.ModTime().UnixNano()) / 1e6, Size: info.Size()}
}

func (l *InspectionLedger) coverageStaleLocked(key string) bool {
	coverage, ok := l.coverage[key]
	if !ok || coverage.Fingerprint == nil || l.root == "" {
		return false
	}
	current := l.fingerprint(key)
	return current != nil && (current.Size != coverage.Fingerprint.Size || math.Abs(current.MTimeMS-coverage.Fingerprint.MTimeMS) > .01)
}

func (l *InspectionLedger) pathKey(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", string(filepath.Separator)))
	if raw == "" {
		return ""
	}
	absolute := raw
	if l.root != "" && !filepath.IsAbs(absolute) {
		absolute = filepath.Join(l.root, absolute)
	}
	absolute, _ = filepath.Abs(absolute)
	key := absolute
	if l.root != "" {
		if relative, err := filepath.Rel(l.root, absolute); err == nil {
			key = relative
		}
	}
	key = filepath.ToSlash(filepath.Clean(key))
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

var readResultRE = regexp.MustCompile(`\(lines (\d+)-(\d+) of (\d+)\)`)

var readHeaderRE = regexp.MustCompile(`(?m)^(?:Read )?(.+?) \(lines \d+-\d+ of \d+\)\.?$`)

func parseReadResult(output string) (int, int, int) {
	match := readResultRE.FindStringSubmatch(output)
	if len(match) != 4 {
		return 0, 0, 0
	}
	var start, end, total int
	_, _ = fmt.Sscanf(match[0], "(lines %d-%d of %d)", &start, &end, &total)
	return start, end, total
}

func readResultPath(output string) string {
	match := readHeaderRE.FindStringSubmatch(output)
	if len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func pathArgument(call contract.ToolCall) string {
	var args struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(call.ArgumentsJSON()), &args)
	return args.Path
}

func readSignature(call contract.ToolCall, key string) string {
	var args struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if json.Unmarshal([]byte(call.ArgumentsJSON()), &args) != nil {
		return "read_file " + key + "#raw" + compactJSON(call.ArgumentsJSON())
	}
	if args.Offset < 1 {
		args.Offset = 1
	}
	limit := "auto"
	if args.Limit >= 1 {
		limit = fmt.Sprint(min(args.Limit, 4000))
	}
	return fmt.Sprintf("read_file %s#o%d#l%s", key, args.Offset, limit)
}

func requestedReadRange(call contract.ToolCall, total int) (int, int, bool) {
	var args struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if json.Unmarshal([]byte(call.ArgumentsJSON()), &args) != nil {
		return 0, 0, false
	}
	if args.Offset < 1 {
		args.Offset = 1
	}
	if args.Limit < 1 {
		args.Limit = 1500
	}
	args.Limit = min(args.Limit, 4000)
	if args.Offset > total {
		return 0, 0, false
	}
	return args.Offset, min(total, args.Offset+args.Limit-1), true
}

func coveringSegments(segments []InspectionSegment, start, end int) []InspectionSegment {
	ordered := append([]InspectionSegment(nil), segments...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Start == ordered[j].Start {
			return ordered[i].End > ordered[j].End
		}
		return ordered[i].Start < ordered[j].Start
	})
	cursor := start
	var used []InspectionSegment
	for _, segment := range ordered {
		if cursor > end {
			break
		}
		if segment.Start > cursor {
			return nil
		}
		if segment.End >= cursor {
			used = append(used, segment)
			cursor = segment.End + 1
		}
	}
	if cursor <= end {
		return nil
	}
	return used
}

func callSignature(call contract.ToolCall) string {
	return call.ToolName() + " " + compactJSON(call.ArgumentsJSON())
}

// inspectSignature produces a dedup key for inspect_code that is stable across
// reorders of JSON fields and that ignores irrelevant fields (testFiles). The
// output format is still deterministic because compactJSON orders keys
// canonically. It uses mode, path, and symbol; mode "outline" rarely carries a
// symbol and is keyed on (mode, path) only so a symbol-less outline and a
// symbol-bearing outline with the same path are correctly distinguished when
// a symbol is set but collapse to the same key when it is not.
func inspectSignature(call contract.ToolCall) string {
	var args struct {
		Mode      string `json:"mode"`
		Path      string `json:"path"`
		Symbol    string `json:"symbol"`
		TestFiles bool   `json:"testFiles"`
	}
	if err := json.Unmarshal([]byte(call.ArgumentsJSON()), &args); err != nil {
		return call.ToolName() + " " + compactJSON(call.ArgumentsJSON())
	}
	mode := strings.TrimSpace(args.Mode)
	if mode == "" {
		mode = "outline"
	}
	parts := []string{call.ToolName(), mode, strings.TrimSpace(args.Path)}
	if strings.TrimSpace(args.Symbol) != "" {
		parts = append(parts, strings.TrimSpace(args.Symbol))
	}
	return strings.Join(parts, " ")
}

func compactJSON(value string) string {
	var parsed any
	if json.Unmarshal([]byte(value), &parsed) != nil {
		return strings.TrimSpace(value)
	}
	encoded, _ := json.Marshal(parsed)
	return string(encoded)
}

func canonicalKey(path string) string {
	if path == "" {
		return ""
	}
	value := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		value = strings.ToLower(value)
	}
	return value
}
