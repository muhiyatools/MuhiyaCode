// Package shellsafe holds the shared command-line tokenizer and command-word
// normalizer used by BOTH destructive-command gates: the read-only classifier
// (internal/orchestrator.IsReadOnlyShell) and the auto-accept backstop
// (internal/workspace.ClassifyShell). Keeping the tokenizer in one foundation
// leaf is what stops the two gates from drifting — a laundering trick closed in
// one gate is closed in both, because they parse command position the same way.
//
// It imports only the standard library, so it sits at the foundation layer and
// can be imported by workspace and orchestrator without a cycle.
package shellsafe

import "strings"

// Token is one bare word plus whether it sits at COMMAND position — the first
// word of a pipeline/chain segment, or the first word inside a command
// substitution ($(…) / backticks / a subshell). Only command-position words can
// be a destructive command; the same word as an argument is data. Word is
// lowercased so callers match case-insensitively.
type Token struct {
	Word       string
	CommandPos bool
}

// NormalizeCommandWord reduces a command-position word to the bare program name
// a destructive-word check keys on. A leading backslash (the shell idiom for
// bypassing an alias), any directory prefix (/bin/rm, /usr/bin/rm), and a
// Windows .exe suffix all name the same program and must not be a way past the
// check.
func NormalizeCommandWord(word string) string {
	word = strings.TrimPrefix(word, `\`)
	if index := strings.LastIndexAny(word, `/\`); index >= 0 {
		word = word[index+1:]
	}
	return strings.TrimSuffix(word, ".exe")
}

// wrapperCommands run another program, so the token after them (past their own
// flags) is the command that actually executes.
var wrapperCommands = map[string]bool{
	"env": true, "xargs": true, "sudo": true, "doas": true, "nice": true,
	"nohup": true, "time": true, "timeout": true, "command": true,
	"builtin": true, "exec": true, "stdbuf": true, "busybox": true, "setsid": true,
	"watch": true, "script": true,
}

// MarkThroughWrappers re-marks the program a wrapper is about to run as command
// position. `env rm -rf .`, `sudo rm -rf /`, and `xargs rm -rf` all put the real
// verb at argument position, where a destructive-word check skips it. Chains
// resolve naturally: marking a token that is itself a wrapper lets the same loop
// see through the next one.
func MarkThroughWrappers(segment []Token) {
	for i := 0; i < len(segment); i++ {
		if !segment[i].CommandPos || !wrapperCommands[NormalizeCommandWord(segment[i].Word)] {
			continue
		}
		for j := i + 1; j < len(segment); j++ {
			word := segment[j].Word
			// Skip the wrapper's own flags, its VAR=value assignments (env), and
			// its numeric arguments (nice 10, timeout 5).
			if strings.HasPrefix(word, "-") || strings.Contains(word, "=") || isNumericToken(word) {
				continue
			}
			segment[j].CommandPos = true
			break
		}
	}
}

func isNumericToken(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if (r < '0' || r > '9') && r != '.' && r != 's' && r != 'm' && r != 'h' {
			return false
		}
	}
	return true
}

// Segments tokenizes a command into segments split on the unquoted separators
// `|`, `;`, `&`, `&&`, `||`, and marks each token's command position. Quoted
// spans are opaque data appended to the current word (so `"rm -rf"` is one
// harmless argument token). A substitution boundary (`$`, backtick, `(`) starts
// a fresh command position, so a destructive word hidden in `$(rm -rf x)` or a
// subshell `(rm -rf x)` is still at command position. Redirect characters
// (`<`, `>`) are treated as ordinary word bytes here; callers that care about
// file-write redirects scan for them separately. A NEWLINE separates commands.
func Segments(command string) [][]Token {
	var segments [][]Token
	var current []Token
	var word strings.Builder
	inDouble, inSingle := false, false
	commandPending := true // the next word begins a command

	flushWord := func() {
		if word.Len() > 0 {
			current = append(current, Token{Word: strings.ToLower(word.String()), CommandPos: commandPending})
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
		case c == '|' || c == ';' || c == '&' || c == '\n' || c == '\r':
			flushSegment()
			if (c == '|' || c == '&') && i+1 < len(command) && command[i+1] == c { // second char of && or ||
				i++
			}
		case c == '$' || c == '`' || c == '(':
			// substitution / subshell boundary: the next word is a nested command
			flushWord()
			commandPending = true
		case c == ' ' || c == '\t' || c == ')' || c == '{' || c == '}':
			flushWord()
		default:
			word.WriteByte(c)
		}
	}
	flushSegment()
	return segments
}
