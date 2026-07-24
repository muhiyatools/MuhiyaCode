package workspace

import (
	"regexp"
	"strings"

	"github.com/muhiya/muhiyacode/internal/shellsafe"
)

type ShellRisk struct {
	Blocked bool
	Reason  string
}

type riskPattern struct {
	re     *regexp.Regexp
	reason string
}

var shellRiskPatterns = []riskPattern{
	// PowerShell / Unix recursive-force deletes (Remove-Item and its aliases, rm,
	// and their laundered spellings) are handled by destructiveDeleteRisk, which
	// tokenizes command position so `/bin/rm`, `\rm`, `(rm`, and abbreviated
	// switches like `-r -fo` cannot slip past. These regexes cover the cmd.exe
	// slash-flag forms that the switch-oriented detector does not.
	{regexp.MustCompile(`(?i)\b(?:del|erase)\b[^\r\n|;&]*/[sq]\b`), "recursive delete"},
	{regexp.MustCompile(`(?i)\b(?:rmdir|rd)\b[^\r\n|;&]*/s\b`), "recursive directory delete"},
	{regexp.MustCompile(`(?i)\bfind\b[^\r\n|;&]*\s-delete\b`), "bulk file delete"},
	{regexp.MustCompile(`(?i)\bformat(?:\.com)?\s+[a-z]:`), "disk format command"},
	{regexp.MustCompile(`(?i)\bmkfs(?:\.[a-z0-9]+)?\b`), "filesystem format command"},
	{regexp.MustCompile(`(?i)\bdd\b[^\r\n|;&]*\bof=/dev/`), "raw disk write"},
	{regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;?\s*:`), "fork bomb"},
	{regexp.MustCompile(`(?i)\bchmod\b[^\r\n|;&]*\s0?777\b`), "world-writable permission change"},
	{regexp.MustCompile(`(?i)\b(?:curl|wget|iwr|invoke-webrequest|irm|invoke-restmethod)\b[^\r\n]*\|\s*(?:sh|bash|zsh|pwsh|powershell|iex)\b`), "piping remote content to a shell"},
	{regexp.MustCompile(`(?i)\biex\s*\(\s*(?:new-object\s+net\.webclient|invoke-webrequest|iwr|invoke-restmethod|irm)\b`), "executing downloaded content"},
	{regexp.MustCompile(`(?i)\b(?:shutdown|restart-computer|stop-computer)\b`), "system power command"},
	{regexp.MustCompile(`(?i)\breg(?:\.exe)?\s+(?:delete|add|import)\b`), "registry mutation"},
	{regexp.MustCompile(`(?i)\bset-executionpolicy\b`), "execution policy mutation"},
	{regexp.MustCompile(`(?i)\bgit\s+push\b[^\r\n]*(?:--force(?:-with-lease)?\b|-f\b)`), "force push"},
	{regexp.MustCompile(`(?i)\.muhiya[\\/]+(?:secrets|mcp-secrets)\.json\b`), "MuhiyaCode secret file access"},
	{regexp.MustCompile(`(?i)\b(?:get-childitem|gci|dir|ls|get-content|gc|cat|type)\s+env:`), "environment secret enumeration"},
	{regexp.MustCompile(`(?im)(?:^|[;&|]\s*)(?:printenv|env|set)\s*(?:$|[;&|])`), "environment secret enumeration"},
}

// registryHiveRef and registryMutationCmdlet together catch PowerShell registry
// provider mutations (e.g. Set-ItemProperty -Path HKLM:\...), which the
// cmd.exe-oriented `reg.exe` pattern above does not cover.
var registryHiveRef = regexp.MustCompile(`(?i)\b(?:hklm|hkcu|hkcr|hku|hkcc|hkey_local_machine|hkey_current_user|hkey_classes_root|hkey_users|hkey_current_config)\b`)
var registryMutationCmdlet = regexp.MustCompile(`(?i)\b(?:set-itemproperty|new-itemproperty|remove-itemproperty|new-item|remove-item|set-item)\b`)

// powershellDeleteWords name PowerShell's Remove-Item and the aliases that also
// take -Recurse/-Force switches (ri, and the cmd.exe-style del/erase/rd/rmdir,
// which PowerShell accepts with PowerShell switches). `rm` is handled with the
// Unix cluster rule instead — see destructiveDeleteRisk.
var powershellDeleteWords = map[string]bool{
	"remove-item": true, "ri": true, "del": true, "erase": true, "rd": true, "rmdir": true,
}

// ClassifyShell is a backstop for clearly destructive or credential-stealing
// commands. It is intentionally not presented as a sandbox; safe-looking
// commands still go through approval unless auto-accept mode is enabled.
func ClassifyShell(command string) ShellRisk {
	if registryHiveRef.MatchString(command) && registryMutationCmdlet.MatchString(command) {
		return ShellRisk{Blocked: true, Reason: "registry mutation"}
	}
	for _, pattern := range shellRiskPatterns {
		if pattern.re.MatchString(command) {
			return ShellRisk{Blocked: true, Reason: pattern.reason}
		}
	}
	return destructiveDeleteRisk(command)
}

// destructiveDeleteRisk blocks a recursive force delete wherever the delete verb
// sits at a real COMMAND position — the first word of a chain segment, the word
// a wrapper (sudo/env/xargs/nohup) runs, or the first word inside a subshell or
// substitution. Tokenizing (rather than regex-scanning) is what closes the
// laundering vectors: `/bin/rm`, `\rm`, `RM.EXE`, and `(rm -rf /)` all normalize
// to `rm` at command position, and a delete verb that appears only inside quotes
// (`git log --grep "rm -rf"`) is never at command position, so it stays a
// searchable string rather than a blocked command. The tokenizer is shared with
// the read-only classifier so the two gates cannot drift.
func destructiveDeleteRisk(command string) ShellRisk {
	for _, segment := range shellsafe.Segments(command) {
		shellsafe.MarkThroughWrappers(segment)
		for i, token := range segment {
			if !token.CommandPos {
				continue
			}
			word := shellsafe.NormalizeCommandWord(token.Word)
			isRM := word == "rm"
			if !isRM && !powershellDeleteWords[word] {
				continue
			}
			recursive, force := deleteSwitches(commandFlags(segment, i), isRM)
			if recursive && force {
				return ShellRisk{Blocked: true, Reason: "recursive force delete"}
			}
		}
	}
	return ShellRisk{}
}

// commandFlags returns the argument tokens that belong to the delete verb at
// cmdIndex: everything up to the next command-position token (a wrapped or
// subshelled command) or the end of the segment. Tokens are already lowercased
// by shellsafe.Segments.
func commandFlags(segment []shellsafe.Token, cmdIndex int) []string {
	var flags []string
	for j := cmdIndex + 1; j < len(segment); j++ {
		if segment[j].CommandPos {
			break
		}
		flags = append(flags, segment[j].Word)
	}
	return flags
}

// deleteSwitches reports whether the flags request recursion and force. `rm`
// uses the Unix cluster rule (a single-dash cluster carries each letter, so
// `-rf` is both, plus the long `--recursive`/`--force`). PowerShell delete verbs
// use prefix matching against the full switch name, so any unambiguous
// abbreviation counts (`-r`, `-rec`, `-recurse`; `-f`, `-fo`, `-force`) while a
// force-only `-Force` is NOT misread as recursion.
func deleteSwitches(flags []string, unix bool) (recursive, force bool) {
	for _, flag := range flags {
		if unix {
			if flag == "--recursive" {
				recursive = true
			}
			if flag == "--force" {
				force = true
			}
			if strings.HasPrefix(flag, "-") && !strings.HasPrefix(flag, "--") {
				recursive = recursive || strings.Contains(flag[1:], "r")
				force = force || strings.Contains(flag[1:], "f")
			}
			continue
		}
		if !strings.HasPrefix(flag, "-") {
			continue
		}
		name := strings.TrimLeft(flag, "-")
		if name == "" {
			continue
		}
		if strings.HasPrefix("recurse", name) {
			recursive = true
		}
		if strings.HasPrefix("force", name) {
			force = true
		}
	}
	return recursive, force
}

var shellRiskPatternsNonAutoAccept = shellRiskPatterns // keep pointing to full list for normal mode

var shellRiskPatternsAutoAccept = buildAutoAcceptPatterns()

func buildAutoAcceptPatterns() []riskPattern {
    result := make([]riskPattern, 0, len(shellRiskPatterns))
    for _, p := range shellRiskPatterns {
        // Drop env enumeration and chmod 777 patterns - permitted in auto-accept
        if p.reason == "environment secret enumeration" || p.reason == "world-writable permission change" {
            continue
        }
        result = append(result, p)
    }
    return result
}

var gitPublishPattern = regexp.MustCompile(`(?i)\bgit\s+(?:commit|push|add\b)`)

func ClassifyShellAutoAccept(command, workspaceRoot string) ShellRisk {
    if gitPublishPattern.MatchString(command) {
        return ShellRisk{Blocked: true, Reason: "git repository publication is not permitted in benchmark mode"}
    }
    if registryHiveRef.MatchString(command) && registryMutationCmdlet.MatchString(command) {
        return ShellRisk{Blocked: true, Reason: "registry mutation"}
    }
    for _, pattern := range shellRiskPatternsAutoAccept {
        if pattern.re.MatchString(command) {
            return ShellRisk{Blocked: true, Reason: pattern.reason}
        }
    }
    return destructiveDeleteRiskAutoAccept(command, workspaceRoot)
}

func destructiveDeleteRiskAutoAccept(command, workspaceRoot string) ShellRisk {
    return destructiveDeleteRisk(command)
}

