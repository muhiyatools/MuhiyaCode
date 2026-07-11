package workspace

import (
	"regexp"
	"strings"
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
	{regexp.MustCompile(`(?i)\bremove-item\b[^\r\n|;&]*\s-(?:[a-z]*r[a-z]*|recurse)\b[^\r\n|;&]*\s-(?:[a-z]*f[a-z]*|force)\b`), "recursive force delete"},
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

// recursiveDeleteAlias matches command names that alias to Remove-Item -Recurse
// -Force (ri, and the cmd.exe-style del/erase/rd/rmdir, which PowerShell also
// accepts with PowerShell-style -Recurse/-Force switches).
var recursiveDeleteAlias = regexp.MustCompile(`(?i)(?:^|[;&|\s])(?:ri|del|erase|rd|rmdir)\s`)

// registryHiveRef and registryMutationCmdlet together catch PowerShell registry
// provider mutations (e.g. Set-ItemProperty -Path HKLM:\...), which the
// cmd.exe-oriented `reg.exe` pattern above does not cover.
var registryHiveRef = regexp.MustCompile(`(?i)\b(?:hklm|hkcu|hkcr|hku|hkcc|hkey_local_machine|hkey_current_user|hkey_classes_root|hkey_users|hkey_current_config)\b`)
var registryMutationCmdlet = regexp.MustCompile(`(?i)\b(?:set-itemproperty|new-itemproperty|remove-itemproperty|new-item|remove-item|set-item)\b`)

var rmCommand = regexp.MustCompile(`(?i)(?:^|[;&|]\s*)rm\s+([^\r\n;&|]+)`)

// ClassifyShell is a backstop for clearly destructive or credential-stealing
// commands. It is intentionally not presented as a sandbox; safe-looking
// commands still go through approval unless auto-accept mode is enabled.
func ClassifyShell(command string) ShellRisk {
	lower := strings.ToLower(command)
	if (strings.Contains(lower, "remove-item") || recursiveDeleteAlias.MatchString(lower)) && strings.Contains(lower, "-recurse") && strings.Contains(lower, "-force") {
		return ShellRisk{Blocked: true, Reason: "recursive force delete"}
	}
	if registryHiveRef.MatchString(command) && registryMutationCmdlet.MatchString(command) {
		return ShellRisk{Blocked: true, Reason: "registry mutation"}
	}
	for _, pattern := range shellRiskPatterns {
		if pattern.re.MatchString(command) {
			return ShellRisk{Blocked: true, Reason: pattern.reason}
		}
	}
	for _, match := range rmCommand.FindAllStringSubmatch(command, -1) {
		recursive, force := false, false
		for _, field := range strings.Fields(match[1]) {
			lower := strings.ToLower(field)
			if lower == "--recursive" {
				recursive = true
			}
			if lower == "--force" {
				force = true
			}
			if strings.HasPrefix(lower, "-") && !strings.HasPrefix(lower, "--") {
				recursive = recursive || strings.Contains(lower[1:], "r")
				force = force || strings.Contains(lower[1:], "f")
			}
		}
		if recursive && force {
			return ShellRisk{Blocked: true, Reason: "recursive force delete"}
		}
	}
	return ShellRisk{}
}
