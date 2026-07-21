package shellsafe

import "testing"

func TestNormalizeCommandWord(t *testing.T) {
	cases := map[string]string{
		`rm`:           "rm",
		`\rm`:          "rm",
		`/bin/rm`:      "rm",
		`/usr/bin/rm`:  "rm",
		`rm.exe`:       "rm", // Segments lowercases before calling, so .exe is always lower here
		`c:\tools\rm`:  "rm",
		`remove-item`:  "remove-item",
		`./scripts/go`: "go",
	}
	for in, want := range cases {
		if got := NormalizeCommandWord(in); got != want {
			t.Errorf("NormalizeCommandWord(%q) = %q, want %q", in, got, want)
		}
	}
}

// Segments must mark command position correctly: the first word of each
// separator-delimited segment and the first word inside a subshell/substitution,
// but never a word that only appears inside quotes.
func TestSegmentsCommandPosition(t *testing.T) {
	commandWords := func(command string) []string {
		var words []string
		for _, segment := range Segments(command) {
			MarkThroughWrappers(segment)
			for _, token := range segment {
				if token.CommandPos {
					words = append(words, NormalizeCommandWord(token.Word))
				}
			}
		}
		return words
	}
	cases := map[string][]string{
		"rm -rf /":                {"rm"},
		"git status; rm -rf .":    {"git", "rm"},
		"git status && rm -rf .":  {"git", "rm"},
		"git status\nrm -rf .":    {"git", "rm"},
		"(rm -rf /)":              {"rm"},
		"sudo rm -rf /":           {"sudo", "rm"}, // wrapper see-through
		"env FOO=bar rm -rf .":    {"env", "rm"},  // skips VAR=value
		`git log --grep "rm -rf"`: {"git"},        // quoted rm is data, not a command
		"echo $(rm -rf x)":        {"echo", "rm"}, // substitution boundary
	}
	for command, want := range cases {
		got := commandWords(command)
		if len(got) != len(want) {
			t.Errorf("command words of %q = %v, want %v", command, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("command words of %q = %v, want %v", command, got, want)
				break
			}
		}
	}
}
