package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/pmezard/go-difflib/difflib"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

var ignoredNames = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, ".next": true, ".cache": true, "coverage": true}

func (w *Workspace) List(ctx context.Context, options ListOptions) (ListResult, error) {
	target, err := w.authorizePath(ctx, ActionRead, options.Path)
	if err != nil {
		return ListResult{}, err
	}
	maxEntries := options.MaxEntries
	if maxEntries <= 0 || maxEntries > 2000 {
		maxEntries = 300
	}
	result := ListResult{Root: relativeSlash(w.root, target)}
	info, err := os.Stat(target)
	if err != nil {
		return result, friendlyPathError(err, relativeSlash(w.root, target))
	}
	if !info.IsDir() {
		return result, fmt.Errorf(instructions.WorkspaceListNotDirectoryTmpl, target)
	}
	baseDepth := strings.Count(filepath.Clean(target), string(filepath.Separator))
	errStop := errors.New("list complete")
	err = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == target {
			return nil
		}
		if entry.IsDir() && ignoredNames[entry.Name()] {
			return filepath.SkipDir
		}
		depth := strings.Count(filepath.Clean(path), string(filepath.Separator)) - baseDepth
		if !options.Recursive && depth > 1 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		result.Entries = append(result.Entries, ListEntry{Path: relativeSlash(w.root, path), Name: entry.Name(), Depth: depth, Mode: info.Mode(), Size: info.Size()})
		if len(result.Entries) >= maxEntries {
			result.Truncated = true
			return errStop
		}
		return nil
	})
	if errors.Is(err, errStop) {
		err = nil
	}
	return result, err
}

func (w *Workspace) Read(ctx context.Context, options ReadOptions) (ReadResult, error) {
	target, err := w.authorizePath(ctx, ActionRead, options.Path)
	if err != nil {
		return ReadResult{}, err
	}
	data, err := readTextFile(target, MaxReadBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if suggestions := w.suggestByBaseName(target); len(suggestions) > 0 {
				err = fmt.Errorf("%w; did you mean: %s?", err, strings.Join(suggestions, ", "))
			}
		}
		return ReadResult{}, err
	}
	lines := splitLines(string(data))
	offset := options.Offset
	if offset <= 0 {
		offset = 1
	}
	limit := options.Limit
	if limit <= 0 {
		limit = DefaultReadLines
	}
	limit = min(limit, MaxReadLines)
	start := min(offset-1, len(lines))
	end := min(len(lines), start+limit)
	selected := append([]string(nil), lines[start:end]...)
	for i, line := range selected {
		if len(line) > 500 {
			selected[i] = line[:500] + " ..."
		}
	}
	w.readLedger.add(target)
	return ReadResult{Path: relativeSlash(w.root, target), Offset: start + 1, TotalLines: len(lines), Lines: selected, Outline: buildOutline(lines), Remaining: len(lines) - end}, nil
}

func (w *Workspace) Grep(ctx context.Context, options GrepOptions) (SearchResult, error) {
	target, err := w.authorizePath(ctx, ActionSearch, options.Path)
	if err != nil {
		return SearchResult{}, err
	}
	maxResults := options.MaxResults
	if maxResults <= 0 || maxResults > 500 {
		maxResults = 80
	}
	pattern := options.Pattern
	if options.Literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	if options.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		// Teach the fix, not just the failure: literal emoji in character
		// classes are a live recurring model mistake ("invalid character
		// class range: 😀-🇿").
		return SearchResult{}, fmt.Errorf(instructions.WorkspaceGrepInvalidPatternTmpl, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return SearchResult{}, friendlyPathError(err, relativeSlash(w.root, target))
	}
	files := []string{target}
	if info.IsDir() {
		files, err = collectFiles(target, 20_000)
		if err != nil {
			return SearchResult{}, err
		}
	}
	var result SearchResult
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if options.Glob != "" {
			match, matchErr := doublestar.Match(options.Glob, filepath.ToSlash(relativeSlash(target, file)))
			if matchErr != nil {
				return result, matchErr
			}
			if !match {
				continue
			}
		}
		data, readErr := readTextFile(file, MaxSearchBytes)
		if readErr != nil {
			continue
		}
		for index, line := range splitLines(string(data)) {
			if re.MatchString(line) {
				result.Matches = append(result.Matches, Match{Path: relativeSlash(w.root, file), Line: index + 1, Text: contract.TruncateEllipsis(line, 500)})
				if len(result.Matches) >= maxResults {
					result.Truncated = true
					return result, nil
				}
			}
		}
	}
	return result, nil
}

func (w *Workspace) Glob(ctx context.Context, options GlobOptions) ([]GlobMatch, error) {
	target, err := w.authorizePath(ctx, ActionSearch, options.Path)
	if err != nil {
		return nil, err
	}
	maxResults := options.MaxResults
	if maxResults <= 0 || maxResults > 2000 {
		maxResults = 200
	}
	pattern := filepath.ToSlash(options.Pattern)
	var result []GlobMatch
	errStop := errors.New("glob complete")
	err = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() && ignoredNames[entry.Name()] && path != target {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		rel := filepath.ToSlash(relativeSlash(target, path))
		matched, matchErr := doublestar.Match(pattern, rel)
		if matchErr != nil {
			return matchErr
		}
		if matched {
			info, _ := entry.Info()
			result = append(result, GlobMatch{Path: relativeSlash(w.root, path), ModTime: info.ModTime(), Size: info.Size()})
			if len(result) >= maxResults {
				return errStop
			}
		}
		return nil
	})
	if errors.Is(err, errStop) {
		err = nil
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, err
}

func (w *Workspace) Edit(ctx context.Context, path string, edit Edit) (EditResult, error) {
	return w.MultiEdit(ctx, path, []Edit{edit})
}

func (w *Workspace) MultiEdit(ctx context.Context, path string, edits []Edit) (EditResult, error) {
	if len(edits) == 0 || len(edits) > 30 {
		return EditResult{}, ErrInvalidArguments
	}
	target, err := w.authorizePath(ctx, ActionEdit, path)
	if err != nil {
		return EditResult{}, err
	}
	if !w.canOverwrite(target) {
		return EditResult{}, fmt.Errorf("%w: %s", ErrUnreadOverwrite, path)
	}
	data, err := readTextFile(target, MaxReadBytes)
	if err != nil {
		return EditResult{}, err
	}
	before := string(data)
	current := before
	result := EditResult{Path: relativeSlash(w.root, target)}
	for index, edit := range edits {
		if edit.Old == "" {
			result.Skipped = append(result.Skipped, fmt.Sprintf(instructions.WorkspaceEditEmptyOldStringTmpl, index+1))
			continue
		}
		next, replacements, note, applyErr := applyEditText(current, edit)
		if applyErr != nil {
			result.Skipped = append(result.Skipped, fmt.Sprintf("edit %d: %v", index+1, applyErr))
			continue
		}
		if note != "" {
			result.Notes = append(result.Notes, note)
		}
		if next != current {
			current = next
			result.AppliedEdits++
			result.Replacements += replacements
		}
	}
	if current == before {
		result.Summary = "No changes needed."
		return result, nil
	}
	if err := w.checkpoint(ctx, "before editing "+result.Path, []string{target}); err != nil {
		return result, err
	}
	if err := writeTextPreservingMode(target, []byte(current)); err != nil {
		return result, err
	}
	result.Changed = true
	result.Diff = compactDiff(result.Path, before, current, 120)
	result.Summary = fmt.Sprintf("Edited %s (%d edit(s), %d replacement(s)).", result.Path, result.AppliedEdits, result.Replacements)
	w.readLedger.add(target)
	return result, nil
}

func (w *Workspace) Write(ctx context.Context, path, content string) (WriteResult, error) {
	target, err := w.authorizePath(ctx, ActionWrite, path)
	if err != nil {
		return WriteResult{}, err
	}
	beforeBytes, readErr := os.ReadFile(target)
	existed := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return WriteResult{}, readErr
	}
	if existed && !w.canOverwrite(target) {
		return WriteResult{}, fmt.Errorf("%w: %s", ErrUnreadOverwrite, path)
	}
	before := string(beforeBytes)
	if existed && before == content {
		return WriteResult{Path: relativeSlash(w.root, target), Summary: "No changes needed.", LineCount: len(splitLines(content))}, nil
	}
	if err := w.checkpoint(ctx, "before writing "+relativeSlash(w.root, target), []string{target}); err != nil {
		return WriteResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return WriteResult{}, err
	}
	if err := writeTextPreservingMode(target, []byte(content)); err != nil {
		return WriteResult{}, err
	}
	w.readLedger.add(target)
	return WriteResult{Path: relativeSlash(w.root, target), Created: !existed, Changed: true, LineCount: len(splitLines(content)), Summary: "Wrote " + relativeSlash(w.root, target) + ".", Diff: compactDiff(relativeSlash(w.root, target), before, content, 120)}, nil
}

func (w *Workspace) RunShell(ctx context.Context, command string, timeout time.Duration) (ShellResult, error) {
	if err := w.guard.ApproveShell(ctx, command); err != nil {
		return ShellResult{}, err
	}
	runner := *w.shell
	runner.OnOutput = w.options.ShellOutput
	return runner.Run(ctx, w.root, command, timeout)
}

func (w *Workspace) GitStatus(ctx context.Context) (ShellResult, error) {
	return w.RunShell(ctx, "git status --short", 30*time.Second)
}

func (w *Workspace) GitDiff(ctx context.Context, options GitDiffOptions) (ShellResult, error) {
	args := []string{"git", "diff", "--no-ext-diff"}
	if options.Staged {
		args = append(args, "--cached")
	}
	if options.Context > 0 && options.Context <= 100 {
		args = append(args, fmt.Sprintf("--unified=%d", options.Context))
	}
	if options.Path != "" {
		target, err := w.authorizePath(ctx, ActionSearch, options.Path)
		if err != nil {
			return ShellResult{}, err
		}
		args = append(args, "--", relativeSlash(w.root, target))
	}
	runner := *w.shell
	if options.OutputLimit > 0 {
		runner.OutputLimit = options.OutputLimit
	}
	if err := w.guard.ApproveShell(ctx, strings.Join(args, " ")); err != nil {
		return ShellResult{}, err
	}
	return runner.runArgv(ctx, w.root, "git", args, 30*time.Second)
}

func (w *Workspace) authorizePath(ctx context.Context, action Action, requested string) (string, error) {
	target, err := Resolve(w.root, requested)
	if err != nil {
		return "", err
	}
	return w.guard.ApprovePath(ctx, action, target)
}

func (w *Workspace) canOverwrite(target string) bool {
	if w.readLedger.has(target) {
		return true
	}
	if w.options.KnownFile != nil {
		return w.options.KnownFile(relativeSlash(w.root, target))
	}
	return false
}

func applyEditText(content string, edit Edit) (string, int, string, error) {
	if edit.Old == edit.New {
		return content, 0, instructions.WorkspaceEditIdenticalBody, nil
	}
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	normalize := func(value string) string { return strings.ReplaceAll(value, "\r\n", "\n") }
	current, old, nextValue := normalize(content), normalize(edit.Old), normalize(edit.New)
	count := strings.Count(current, old)
	if count == 0 {
		if nextValue != "" && strings.Contains(current, nextValue) {
			return content, 0, instructions.WorkspaceEditAlreadyPresentBody, nil
		}
		current, count = trailingWhitespaceMatch(current, old)
		if count == 0 {
			return content, 0, instructions.WorkspaceEditNotFoundPrefix + closestRegion(normalize(content), old), nil
		}
	}
	if count > 1 && !edit.ReplaceAll {
		return content, 0, fmt.Sprintf(instructions.WorkspaceEditAmbiguousTmpl, count), nil
	}
	if edit.ReplaceAll {
		current = strings.ReplaceAll(current, old, nextValue)
	} else {
		current = strings.Replace(current, old, nextValue, 1)
		count = 1
	}
	if newline == "\r\n" {
		current = strings.ReplaceAll(current, "\n", "\r\n")
	}
	return current, count, "", nil
}

// trailingWhitespaceMatch returns a normalized content variant where the
// uniquely matched region has its original trailing whitespace removed, so
// the normal replacement path can proceed without fuzzy indentation changes.
func trailingWhitespaceMatch(content, old string) (string, int) {
	contentLines, oldLines := strings.Split(content, "\n"), strings.Split(old, "\n")
	if len(oldLines) == 0 || len(oldLines) > len(contentLines) {
		return content, 0
	}
	var starts []int
	for start := 0; start+len(oldLines) <= len(contentLines); start++ {
		matched := true
		for i := range oldLines {
			if strings.TrimRight(contentLines[start+i], " \t") != strings.TrimRight(oldLines[i], " \t") {
				matched = false
				break
			}
		}
		if matched {
			starts = append(starts, start)
		}
	}
	if len(starts) != 1 {
		return content, len(starts)
	}
	start := starts[0]
	copy(contentLines[start:start+len(oldLines)], oldLines)
	return strings.Join(contentLines, "\n"), 1
}

func closestRegion(content, needle string) string {
	needleLines := strings.Split(needle, "\n")
	anchor := ""
	for _, line := range needleLines {
		if strings.TrimSpace(line) != "" {
			anchor = strings.TrimSpace(line)
			break
		}
	}
	lines := strings.Split(content, "\n")
	best := 0
	for i, line := range lines {
		if anchor != "" && strings.Contains(strings.TrimSpace(line), contract.TruncateEllipsis(anchor, 40)) {
			best = i
			break
		}
	}
	start, end := max(0, best-2), min(len(lines), best+3)
	var result []string
	for i := start; i < end; i++ {
		result = append(result, fmt.Sprintf("%d | %s", i+1, lines[i]))
	}
	return strings.Join(result, "\n")
}

func compactDiff(path, before, after string, maxLines int) string {
	diff := difflib.UnifiedDiff{A: difflib.SplitLines(before), B: difflib.SplitLines(after), FromFile: "a/" + filepath.ToSlash(path), ToFile: "b/" + filepath.ToSlash(path), Context: 3}
	text, _ := difflib.GetUnifiedDiffString(diff)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("... [diff truncated, %d lines omitted]", len(lines)-maxLines))
	}
	return strings.Join(lines, "\n")
}

// suggestByBaseName returns up to three workspace-relative paths whose base
// name equals the missing file's base name case-insensitively. It only runs on
// the read-not-found error path, so a bounded workspace walk is acceptable.
func (w *Workspace) suggestByBaseName(missing string) []string {
	base := filepath.Base(missing)
	files, err := collectFiles(w.root, 20_000)
	if err != nil {
		return nil
	}
	var suggestions []string
	for _, file := range files {
		if strings.EqualFold(filepath.Base(file), base) {
			suggestions = append(suggestions, relativeSlash(w.root, file))
			if len(suggestions) >= 3 {
				break
			}
		}
	}
	return suggestions
}

func collectFiles(root string, maxFiles int) ([]string, error) {
	var files []string
	errStop := errors.New("enough files")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() && ignoredNames[entry.Name()] && path != root {
			return filepath.SkipDir
		}
		if entry.Type().IsRegular() {
			files = append(files, path)
			if len(files) >= maxFiles {
				return errStop
			}
		}
		return nil
	})
	if errors.Is(err, errStop) {
		err = nil
	}
	return files, err
}

func readTextFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		// Return the raw not-exist error unchanged so Read's basename-suggestion
		// path (errors.Is(fs.ErrNotExist)) still fires; os.Open's message already
		// names the path in plain English (no opaque GetFileAttributesEx — that is
		// os.Stat's spelling, handled on the list/search paths via friendlyPathError).
		return nil, err
	}
	defer file.Close()
	reader := io.LimitReader(file, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf(instructions.WorkspaceFileExceedsLimitTmpl, limit)
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, errors.New(instructions.WorkspaceBinaryUnsupportedBody)
	}
	return data, nil
}

func writeTextPreservingMode(path string, data []byte) error {
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".muhiya-edit-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replaceAtomic(name, path)
}

func splitLines(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.TrimSuffix(value, "\n")
	if value == "" {
		return []string{}
	}
	return strings.Split(value, "\n")
}

var outlineRE = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?(?:function|class|interface|enum|type|def|func|struct|trait|impl|const|let|var)\s+([A-Za-z_$][\w$]*)`)

func buildOutline(lines []string) string {
	if len(lines) < 300 {
		return ""
	}
	var symbols []string
	for i, line := range lines {
		if match := outlineRE.FindStringSubmatch(line); len(match) > 1 {
			symbols = append(symbols, fmt.Sprintf("%s@%d", match[1], i+1))
			if len(symbols) >= 40 {
				break
			}
		}
	}
	return strings.Join(symbols, ", ")
}
