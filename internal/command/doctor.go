package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/buildinfo"
)

// This file adds the Stability Overhaul T005 diagnostics to `muhiyacode doctor`
// (defect D3): a self-identifying build line, launch-shadowing detection (the
// "same error keeps showing" root cause — the npm command runs a vendored exe a
// plain `go build` never touches), a masked API-key check, and workspace
// writability/git checks. The pure diagnoseShadow is unit-tested; the gatherers
// are thin OS I/O wrappers around it.

// printBuildDiagnostic prints the running binary's stamped identity and warns
// when it is an unstamped (plain `go build`) build.
func printBuildDiagnostic(out io.Writer) {
	stamped := buildinfo.Commit != "unknown" && buildinfo.Date != "unknown"
	status := "ok  "
	if !stamped {
		status = "warn"
	}
	fmt.Fprintf(out, "%s build: MuhiyaCode %s (commit %s, built %s)\n", status, buildinfo.Version, buildinfo.Commit, buildinfo.Date)
	if !stamped {
		fmt.Fprintln(out, "     unstamped build — official builds inject a commit/date via scripts/build.ps1")
	}
}

// shadowInputs is the pure snapshot diagnoseShadow reasons over. Every field is
// gathered by gatherShadowInputs from the real environment; tests construct it
// directly so no OS calls are needed to test the logic.
type shadowInputs struct {
	resolved      string // where `muhiyacode` resolves on PATH ("" if not found)
	isNpmShim     bool   // the resolved command is the npm launcher shim
	vendorExe     string // vendored exe path the npm shim runs ("" if n/a)
	vendorExists  bool
	vendorSize    int64
	vendorMod     string // formatted vendor exe mtime
	envBinary     string // MUHIYACODE_BINARY override
	thisCommit    string // buildinfo.Commit of the running process
	vendorVersion string // first line of `<vendorExe> --version` ("" if not run)
}

// diagnoseShadow turns a shadowInputs snapshot into ok/warn diagnostic lines. Its
// crux: does the binary the `muhiyacode` command would actually run match THIS
// build? A mismatch is the stale-vendored-exe defect (D3).
func diagnoseShadow(in shadowInputs) []string {
	var lines []string
	if in.envBinary != "" {
		lines = append(lines, "ok   launch: MUHIYACODE_BINARY override set → "+in.envBinary)
	}
	if in.resolved == "" {
		lines = append(lines, "warn launch: `muhiyacode` is not on PATH (you are running this binary directly)")
		return lines
	}
	lines = append(lines, "ok   launch: `muhiyacode` resolves to "+in.resolved)
	if in.isNpmShim {
		if in.vendorExists {
			lines = append(lines, fmt.Sprintf("ok   launch: npm shim runs vendored exe (%d bytes, %s)", in.vendorSize, in.vendorMod))
		} else {
			lines = append(lines, "warn launch: npm shim present but its vendored exe is missing — reinstall: npm install -g muhiyacode")
			return lines
		}
	}
	if in.vendorVersion != "" && in.thisCommit != "" && in.thisCommit != "unknown" {
		if strings.Contains(in.vendorVersion, in.thisCommit) {
			lines = append(lines, "ok   launch: the `muhiyacode` command matches THIS build ("+in.thisCommit+")")
		} else {
			lines = append(lines, "warn launch: the `muhiyacode` command runs a DIFFERENT build than this one —")
			lines = append(lines, "     this build:  commit "+in.thisCommit)
			lines = append(lines, "     muhiyacode:  "+in.vendorVersion)
			lines = append(lines, "     → run scripts/swap-global.ps1, then restart your terminal")
		}
	}
	return lines
}

// printShadowDiagnostic gathers real environment data and prints the lines.
func printShadowDiagnostic(out io.Writer) {
	for _, line := range diagnoseShadow(gatherShadowInputs()) {
		fmt.Fprintln(out, line)
	}
}

func gatherShadowInputs() shadowInputs {
	in := shadowInputs{thisCommit: buildinfo.Commit, envBinary: strings.TrimSpace(os.Getenv("MUHIYACODE_BINARY"))}
	if resolved, err := exec.LookPath("muhiyacode"); err == nil {
		in.resolved = resolved
	}
	lower := strings.ToLower(in.resolved)
	if strings.Contains(lower, "npm") || strings.Contains(lower, "node_modules") {
		in.isNpmShim = true
		if appData := os.Getenv("APPDATA"); appData != "" {
			in.vendorExe = filepath.Join(appData, "npm", "node_modules", "muhiyacode", "vendor", "muhiyacode.exe")
			if info, err := os.Stat(in.vendorExe); err == nil {
				in.vendorExists = true
				in.vendorSize = info.Size()
				in.vendorMod = info.ModTime().Format("2006-01-02 15:04")
			}
		}
	}
	// The parity check runs the VENDORED exe directly (clean .exe spawn) rather
	// than the shim (which would trampoline through node) — it is the binary the
	// command ultimately executes.
	if in.vendorExists {
		in.vendorVersion = firstVersionLine(in.vendorExe)
	}
	return in
}

// firstVersionLine runs `<exe> --version` with a short timeout and returns its
// first output line, or "" on any failure.
func firstVersionLine(exe string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0])
}

// maskAPIKey renders a key as ····<last4>, never revealing the whole value.
func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "(none — run /login or muhiyacode config set apiKey <key>)"
	}
	if len(key) <= 4 {
		return "····"
	}
	return "····" + key[len(key)-4:]
}

// workspaceDiagnostics reports whether the working directory is writable and
// whether it (or an ancestor) is a git repository. dir "" means the current cwd.
func workspaceDiagnostics(dir string) []string {
	var lines []string
	if strings.TrimSpace(dir) == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	probe := filepath.Join(dir, ".muhiyacode-doctor-write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		lines = append(lines, "warn workspace: NOT writable — "+dir)
	} else {
		_ = os.Remove(probe)
		lines = append(lines, "ok   workspace: writable — "+dir)
	}
	if hasGitRepo(dir) {
		lines = append(lines, "ok   workspace: inside a git repository (edits are also /undo-able in-session)")
	} else {
		lines = append(lines, "ok   workspace: not a git repository (edits are still /undo-able in-session)")
	}
	return lines
}

// hasGitRepo reports whether dir or any ancestor holds a .git entry.
func hasGitRepo(dir string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}
