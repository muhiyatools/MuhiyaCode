package orchestrator

import "strings"

// IsReadOnlyShell reports whether a shell command is safe for a read-only agent
// (a research/plan/review subagent, or the main loop in manual plan mode) to run
// via run_shell.
//
// It is PERMISSIVE BY DEFAULT — a denylist, not an allowlist. Any inspection,
// build, or test command is allowed, in any shell syntax: `cd` anywhere, chained
// with `&&`, `||`, `;`, `&`, or pipes, including command substitution. Only a
// genuinely destructive operation is refused:
//
//   - a bare command word that deletes/moves/overwrites files or mutates the repo
//     (rm, del, mv, cp, tee, sed, PowerShell Remove-Item, `git commit/checkout/…`),
//   - an in-place / file-writing flag (--fix, --in-place, --output, find -delete/-exec),
//   - an output redirect that writes a file (`>` / `>>` to anything but a null
//     device or an fd duplication such as `2>&1`).
//
// This replaced the former allowlist parser, which walled off every command whose
// first word was not pre-enumerated and so produced a steady stream of false
// "this read-only agent's run_shell only runs read-only commands" blocks on
// perfectly harmless inspection commands (`cd path ; go version`, `dir & dir &
// where go`, `go env …`). A denylist cannot regress that way: a new read-only
// tool never has to be added to a list before it works. Destructive words are
// still detected even inside quote-stripped substitutions, and chaining a
// destructive command after a safe one is caught because the whole command is
// scanned, not just its first segment.
func IsReadOnlyShell(command string) bool {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false
	}
	if hasFileWriteRedirect(trimmed) {
		return false
	}
	// D1 fix (T031): a destructive COMMAND WORD is only destructive at command
	// position (the first word of a pipeline/chain segment, or the first word
	// inside a command substitution). As an ARGUMENT it is harmless — `grep format
	// main.go`, `rg kill internal/`, `git log --grep "rm -rf"` are all reads. The
	// prior flat scan blocked them. Destructive FLAGS (--fix, -delete, …) mutate
	// wherever they appear, so they are checked at every position.
	for _, segment := range shellCommandSegments(trimmed) {
		for i, token := range segment {
			word := token.word
			if shellDestructiveFlags[word] {
				return false
			}
			if flag, _, cut := strings.Cut(word, "="); cut && shellDestructiveFlags[flag] {
				return false
			}
			if !token.commandPos {
				continue
			}
			if shellDestructiveWords[word] {
				return false
			}
			// A mutating git subcommand is destructive only in `git <sub>` position.
			if word == "git" && i+1 < len(segment) && gitMutatingSubcommands[segment[i+1].word] {
				return false
			}
		}
	}
	return true
}

// shellDestructiveWords are bare command words (or PowerShell cmdlets) that write,
// delete, move, or otherwise mutate the filesystem. If any appears as an unquoted
// token anywhere in the command, the command is not read-only. sed is blocked
// wholesale (its `-i` form writes, and grep/awk cover every read-only need).
var shellDestructiveWords = map[string]bool{
	"rm": true, "rmdir": true, "del": true, "erase": true, "rd": true, "unlink": true,
	"mv": true, "move": true, "ren": true, "rename": true,
	"cp": true, "copy": true, "xcopy": true, "robocopy": true,
	"mkdir": true, "md": true, "touch": true, "tee": true, "ln": true, "mklink": true,
	"chmod": true, "chown": true, "chgrp": true, "attrib": true, "icacls": true, "takeown": true,
	"truncate": true, "shred": true, "dd": true, "mkfs": true, "format": true, "fsutil": true,
	"sed":  true,
	"kill": true, "pkill": true, "taskkill": true, "shutdown": true, "reboot": true, "halt": true,
	// PowerShell mutating cmdlets (lowercased for the case-insensitive match).
	"remove-item": true, "move-item": true, "copy-item": true, "new-item": true,
	"rename-item": true, "set-content": true, "add-content": true, "clear-content": true,
	"out-file": true, "set-item": true, "clear-item": true, "new-itemproperty": true,
	"remove-itemproperty": true, "set-itemproperty": true, "stop-process": true,
	"stop-computer": true, "restart-computer": true, "start-process": true,
	"ri": true, "mi": true, "cpi": true, "ni": true, "sc": true, "ac": true, "spps": true,
}

// shellDestructiveFlags are per-token argument spellings that turn an otherwise
// read-only command into a writer (linters fixing in place, find executing or
// deleting matches, a tool writing an output file). Kept deliberately narrow so
// common read-only flags — grep -i, grep -o — are never caught.
var shellDestructiveFlags = map[string]bool{
	"--fix": true, "--write": true, "--in-place": true, "--output": true, "--output-file": true,
	"-delete": true, "-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
}

// gitMutatingSubcommands are git subcommands that change the working tree, index,
// or repository. Detected only immediately after a `git` token.
var gitMutatingSubcommands = map[string]bool{
	"add": true, "commit": true, "push": true, "pull": true, "reset": true,
	"checkout": true, "switch": true, "restore": true, "clean": true, "rm": true,
	"mv": true, "stash": true, "merge": true, "rebase": true, "apply": true,
	"cherry-pick": true, "revert": true, "am": true, "tag": true, "gc": true,
	"prune": true, "filter-branch": true, "update-ref": true, "config": true,
	"init": true, "clone": true, "worktree": true,
}

// hasFileWriteRedirect scans (outside quotes) for a `>` or `>>` redirect whose
// target is a real file rather than a null device or an fd duplication. Input
// redirection (`<`) reads a file and is not destructive, so it is allowed.
func hasFileWriteRedirect(command string) bool {
	inDouble, inSingle := false, false
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case inDouble || inSingle:
			// quoted content is data, not a redirect
		case c == '>':
			j := i + 1
			if j < len(command) && command[j] == '>' { // ">>"
				j++
			}
			for j < len(command) && (command[j] == ' ' || command[j] == '\t') {
				j++
			}
			if !harmlessRedirectTarget(command[j:]) {
				return true
			}
		}
	}
	return false
}

// harmlessRedirectTarget reports whether the redirect target beginning rest is a
// null device or an fd duplication (2>&1, 1>&2) — targets that discard or merge
// streams and never write a file the model cares about. Every shell's null
// spelling is accepted so the model's platform-native form is never mistaken for
// a file write: Unix /dev/null, cmd.exe nul/NUL, and PowerShell $null.
func harmlessRedirectTarget(rest string) bool {
	if strings.HasPrefix(rest, "&") {
		return true // fd duplication: 2>&1, 1>&2, &>
	}
	lower := strings.ToLower(rest)
	for _, target := range []string{"/dev/null", "/dev/stdout", "/dev/stderr"} {
		if strings.HasPrefix(lower, target) {
			return true
		}
	}
	// cmd.exe "nul"/"NUL" and PowerShell "$null", each only as a whole token.
	for _, nullDevice := range []string{"nul", "$null"} {
		if strings.HasPrefix(lower, nullDevice) {
			after := rest[len(nullDevice):]
			if after == "" || after[0] == ' ' || after[0] == '\t' || after[0] == ';' || after[0] == '|' || after[0] == '&' || after[0] == ':' {
				return true
			}
		}
	}
	return false
}

// shellToken is one bare word plus whether it sits at COMMAND position — the
// first word of a pipeline/chain segment, or the first word inside a command
// substitution ($(…) / backticks / a subshell). Only command-position words can
// be a destructive command; the same word as an argument is data.
type shellToken struct {
	word       string
	commandPos bool
}

// shellCommandSegments tokenizes a command into segments split on the unquoted
// separators `|`, `;`, `&`, `&&`, `||`, and marks each token's command position.
// Quoted spans are opaque data appended to the current word (so `"rm -rf"` is one
// harmless argument token). A substitution boundary (`$`, backtick, `(`) starts a
// fresh command position, so a destructive word hidden in `$(rm -rf x)` is still
// caught. Redirect characters (`<`, `>`) are treated as ordinary word bytes here;
// real file writes are rejected earlier by hasFileWriteRedirect.
func shellCommandSegments(command string) [][]shellToken {
	var segments [][]shellToken
	var current []shellToken
	var word strings.Builder
	inDouble, inSingle := false, false
	commandPending := true // the next word begins a command

	flushWord := func() {
		if word.Len() > 0 {
			current = append(current, shellToken{word: strings.ToLower(word.String()), commandPos: commandPending})
			word.Reset()
			commandPending = false
		}
	}
	flushSegment := func() {
		flushWord()
		if len(current) > 0 {
			segments = append(segments, current)
			current = nil
		}
		commandPending = true // first word of the next segment is a command
	}

	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case inDouble || inSingle:
			word.WriteByte(c) // quoted data belongs to the current word
		case c == '|' || c == ';' || c == '&':
			flushSegment()
			if i+1 < len(command) && command[i+1] == c { // consume the second char of && or ||
				i++
			}
		case c == '$' || c == '`' || c == '(':
			// substitution / subshell boundary: the next word is a nested command
			flushWord()
			commandPending = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ')' || c == '{' || c == '}':
			flushWord()
		default:
			word.WriteByte(c)
		}
	}
	flushSegment()
	return segments
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
