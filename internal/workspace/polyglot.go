package workspace

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/processenv"
)

var (
	// TypeScript / JavaScript
	tsFuncRegexp  = regexp.MustCompile(`(?m)^(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s+([A-Za-z0-9_$]+)\s*\(([^)]*)\)`)
	tsConstRegexp = regexp.MustCompile(`(?m)^(?:export\s+)?const\s+([A-Za-z0-9_$]+)\s*=\s*(?:async\s+)?(?:\([^)]*\)|[A-Za-z0-9_$]+)\s*=>`)
	tsClassRegexp = regexp.MustCompile(`(?m)^(?:export\s+)?(?:default\s+)?class\s+([A-Za-z0-9_$]+)`)
	tsIfaceRegexp = regexp.MustCompile(`(?m)^(?:export\s+)?interface\s+([A-Za-z0-9_$]+)`)
	tsTypeRegexp  = regexp.MustCompile(`(?m)^(?:export\s+)?type\s+([A-Za-z0-9_$]+)\s*=`)

	// Python
	pyFuncRegexp  = regexp.MustCompile(`(?m)^[ \t]*(?:async\s+)?def\s+([A-Za-z0-9_]+)\s*\(([^)]*)\)`)
	pyClassRegexp = regexp.MustCompile(`(?m)^[ \t]*class\s+([A-Za-z0-9_]+)`)

	// Rust
	rsFuncRegexp   = regexp.MustCompile(`(?m)^(?:pub\s+)?(?:async\s+)?fn\s+([A-Za-z0-9_]+)\s*\(([^)]*)\)`)
	rsStructRegexp = regexp.MustCompile(`(?m)^(?:pub\s+)?struct\s+([A-Za-z0-9_]+)`)
	rsEnumRegexp   = regexp.MustCompile(`(?m)^(?:pub\s+)?enum\s+([A-Za-z0-9_]+)`)
	rsTraitRegexp  = regexp.MustCompile(`(?m)^(?:pub\s+)?trait\s+([A-Za-z0-9_]+)`)

	// C++ / C
	cppFuncRegexp  = regexp.MustCompile(`(?m)^(?:[A-Za-z0-9_:<>]+\s+)+([A-Za-z0-9_]+)\s*\(([^)]*)\)\s*(?:const)?\s*\{?`)
	cppClassRegexp = regexp.MustCompile(`(?m)^(?:class|struct)\s+([A-Za-z0-9_]+)`)

	// Java
	javaClassRegexp = regexp.MustCompile(`(?m)^(?:public\s+|protected\s+|private\s+)?(?:static\s+)?class\s+([A-Za-z0-9_]+)`)
	javaIfaceRegexp = regexp.MustCompile(`(?m)^(?:public\s+|protected\s+|private\s+)?interface\s+([A-Za-z0-9_]+)`)
	javaFuncRegexp  = regexp.MustCompile(`(?m)^(?:public\s+|protected\s+|private\s+)?(?:static\s+)?(?:[A-Za-z0-9_<>\[\]]+\s+)+([A-Za-z0-9_]+)\s*\(([^)]*)\)`)
)

// SupportedPolyglotExtensions maps supported file extensions to language names.
var SupportedPolyglotExtensions = map[string]string{
	".go":   "go",
	".ts":   "typescript",
	".tsx":  "typescript",
	".js":   "javascript",
	".jsx":  "javascript",
	".mjs":  "javascript",
	".cjs":  "javascript",
	".py":   "python",
	".rs":   "rust",
	".cpp":  "cpp",
	".cc":   "cpp",
	".cxx":  "cpp",
	".c":    "c",
	".h":    "c",
	".hpp":  "cpp",
	".java": "java",
}

type CodeIntelligenceCapability struct {
	Language   string
	Outline    string
	Definition string
	References string
}

// CodeIntelligenceFor makes the accuracy boundary explicit to callers. The
// built-in index never claims compiler-level or cross-module semantic results.
func CodeIntelligenceFor(path string) (CodeIntelligenceCapability, bool) {
	language, supported := SupportedPolyglotExtensions[strings.ToLower(filepath.Ext(path))]
	if !supported {
		return CodeIntelligenceCapability{}, false
	}
	if language == "go" {
		return CodeIntelligenceCapability{
			Language: language, Outline: "syntax-aware AST",
			Definition: "syntax-aware top-level declaration match",
			References: "syntax-aware identifier match (not type-resolved)",
		}, true
	}
	return CodeIntelligenceCapability{
		Language: language, Outline: "lexical declaration extraction",
		Definition: "lexical declaration match", References: "lexical identifier match",
	}, true
}

// IsPolyglotSupported checks if a file path is supported by the code indexer.
func IsPolyglotSupported(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, supported := SupportedPolyglotExtensions[ext]
	return supported
}

// ParsePolyglotOutline extracts top-level structural declarations from non-Go files.
func ParsePolyglotOutline(path string) (*FileOutline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("codeindex: read file %s: %w", path, err)
	}
	return ParsePolyglotOutlineSource(path, data)
}

// ParsePolyglotOutlineSource extracts an outline from an immutable source
// snapshot. Keeping parsing independent from filesystem I/O lets workspace
// consumers share content-identified parse results without accepting stale
// stat- or timestamp-based cache hits.
func ParsePolyglotOutlineSource(path string, data []byte) (*FileOutline, error) {
	ext := strings.ToLower(filepath.Ext(path))
	lang, ok := SupportedPolyglotExtensions[ext]
	if !ok {
		return nil, fmt.Errorf("codeindex: unsupported file type %q for %s", ext, path)
	}

	outline := &FileOutline{
		Path:    path,
		Package: lang,
		Symbols: []SymbolEntry{},
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	switch lang {
	case "typescript", "javascript":
		extractMatches(lines, content, tsFuncRegexp, SymbolFunc, outline)
		extractMatches(lines, content, tsConstRegexp, SymbolFunc, outline)
		extractMatches(lines, content, tsClassRegexp, SymbolStruct, outline)
		extractMatches(lines, content, tsIfaceRegexp, SymbolInterface, outline)
		extractMatches(lines, content, tsTypeRegexp, SymbolType, outline)

	case "python":
		extractMatches(lines, content, pyFuncRegexp, SymbolFunc, outline)
		extractMatches(lines, content, pyClassRegexp, SymbolStruct, outline)

	case "rust":
		extractMatches(lines, content, rsFuncRegexp, SymbolFunc, outline)
		extractMatches(lines, content, rsStructRegexp, SymbolStruct, outline)
		extractMatches(lines, content, rsEnumRegexp, SymbolType, outline)
		extractMatches(lines, content, rsTraitRegexp, SymbolInterface, outline)

	case "cpp", "c":
		extractMatches(lines, content, cppClassRegexp, SymbolStruct, outline)
		extractMatches(lines, content, cppFuncRegexp, SymbolFunc, outline)

	case "java":
		extractMatches(lines, content, javaClassRegexp, SymbolStruct, outline)
		extractMatches(lines, content, javaIfaceRegexp, SymbolInterface, outline)
		extractMatches(lines, content, javaFuncRegexp, SymbolFunc, outline)
	}

	sort.Slice(outline.Symbols, func(i, j int) bool {
		return outline.Symbols[i].Line < outline.Symbols[j].Line
	})

	if len(outline.Symbols) > maxSymbols {
		outline.Symbols = outline.Symbols[:maxSymbols]
		outline.Truncated = true
	}

	return outline, nil
}

func extractMatches(lines []string, content string, re *regexp.Regexp, kind SymbolKind, outline *FileOutline) {
	matches := re.FindAllStringSubmatchIndex(content, -1)
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		nameStart, nameEnd := m[2], m[3]
		if nameStart < 0 || nameEnd > len(content) {
			continue
		}
		name := content[nameStart:nameEnd]
		lineNo := calculateLineNumber(content, m[0])

		sig := ""
		if lineNo > 0 && lineNo <= len(lines) {
			sig = strings.TrimSpace(lines[lineNo-1])
			if len(sig) > 100 {
				sig = sig[:100] + "..."
			}
		}

		outline.Symbols = append(outline.Symbols, SymbolEntry{
			Name:      name,
			Kind:      kind,
			Line:      lineNo,
			Signature: sig,
			File:      outline.Path,
		})
	}
}

func calculateLineNumber(content string, byteOffset int) int {
	if byteOffset <= 0 {
		return 1
	}
	if byteOffset > len(content) {
		byteOffset = len(content)
	}
	return strings.Count(content[:byteOffset], "\n") + 1
}

// CollectSourceFiles gathers source files for all supported polyglot languages.
func CollectSourceFiles(path string, includeTests bool) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("codeindex: %w", err)
	}

	if !info.IsDir() {
		if !IsPolyglotSupported(path) {
			return nil, fmt.Errorf("inspect_code: file %s has unsupported type", path)
		}
		return []string{path}, nil
	}

	const maxFiles = 2_000
	if files, found := indexedSourceFiles(path, includeTests, maxFiles); found {
		return files, nil
	}
	var files []string
	err = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			name := filepath.Base(p)
			if info != nil && info.IsDir() && (name == "node_modules" || name == ".git" || name == "dist" || name == "target" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if IsPolyglotSupported(p) {
			if !includeTests && (strings.Contains(p, "_test.") || strings.Contains(p, ".test.") || strings.Contains(p, ".spec.")) {
				return nil
			}
			files = append(files, p)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("codeindex: walk directory %s: %w", path, err)
	}

	sort.Strings(files)
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}
	return files, nil
}

func indexedSourceFiles(path string, includeTests bool, maxFiles int) ([]string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	commands := [][]string{
		{"git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"},
		{"rg", "--files", "-0", "."},
	}
	for _, command := range commands {
		files, err := runSourceIndex(ctx, path, command, includeTests, maxFiles)
		if err == nil {
			sort.Strings(files)
			return files, true
		}
	}
	return nil, false
}

func runSourceIndex(
	ctx context.Context,
	root string,
	command []string,
	includeTests bool,
	maxFiles int,
) ([]string, error) {
	executable, err := exec.LookPath(command[0])
	if err != nil {
		return nil, err
	}
	process := exec.CommandContext(ctx, executable, command[1:]...)
	process.Dir = root
	process.Env = processenv.Sanitized(os.Environ())
	process.Stderr = io.Discard
	stdout, err := process.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := process.Start(); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(stdout, 64<<10)
	files := make([]string, 0, min(maxFiles, 256))
	for len(files) < maxFiles {
		value, readErr := reader.ReadString(0)
		value = strings.TrimSuffix(value, "\x00")
		if value != "" {
			candidate := filepath.Clean(filepath.Join(root, filepath.FromSlash(value)))
			if pathInside(root, candidate) && IsPolyglotSupported(candidate) &&
				(includeTests || !isTestSource(candidate)) {
				files = append(files, candidate)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			_ = process.Wait()
			return nil, readErr
		}
	}
	if len(files) == maxFiles && process.Process != nil {
		_ = process.Process.Kill()
	}
	waitErr := process.Wait()
	if waitErr != nil && len(files) == 0 {
		return nil, waitErr
	}
	return files, nil
}

func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func isTestSource(path string) bool {
	return strings.Contains(path, "_test.") ||
		strings.Contains(path, ".test.") ||
		strings.Contains(path, ".spec.")
}
