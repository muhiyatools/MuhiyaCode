package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaskAPIKey(t *testing.T) {
	cases := map[string]string{
		"":                 "(none",    // prefix — full text has guidance
		"abc":              "····",     // ≤4 → fully hidden
		"sk-virt-0123DEAD": "····DEAD", // reveal only the last 4
	}
	for key, wantPrefix := range cases {
		got := maskAPIKey(key)
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("maskAPIKey(%q) = %q, want prefix %q", key, got, wantPrefix)
		}
		// The full secret must never appear.
		if key != "" && len(key) > 4 && strings.Contains(got, key) {
			t.Errorf("maskAPIKey leaked the full key: %q", got)
		}
	}
}

func TestDiagnoseShadowStaleVendorWarns(t *testing.T) {
	// The command resolves to the npm shim; the vendored exe it runs reports a
	// DIFFERENT commit than this build → the D3 stale-binary warning must fire.
	lines := diagnoseShadow(shadowInputs{
		resolved: `C:\npm\muhiyacode.cmd`, isNpmShim: true,
		vendorExe: `C:\npm\...\vendor\muhiyacode.exe`, vendorExists: true, vendorSize: 28_000_000, vendorMod: "2026-07-14 15:39",
		thisCommit: "72c7d51", vendorVersion: "MuhiyaCode 1.0.2 (commit aaaaaaa, built 2026-07-14)",
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "DIFFERENT build") || !strings.Contains(joined, "swap-global") {
		t.Fatalf("stale vendor must warn with a fix hint:\n%s", joined)
	}
}

func TestDiagnoseShadowInSyncOK(t *testing.T) {
	lines := diagnoseShadow(shadowInputs{
		resolved: `C:\npm\muhiyacode.cmd`, isNpmShim: true,
		vendorExists: true, vendorSize: 1, vendorMod: "x",
		thisCommit: "72c7d51", vendorVersion: "MuhiyaCode 1.0.2 (commit 72c7d51+dirty, built 2026-07-16)",
	})
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "DIFFERENT build") {
		t.Fatalf("matching commits must NOT warn:\n%s", joined)
	}
	if !strings.Contains(joined, "matches THIS build") {
		t.Fatalf("matching commits should confirm sync:\n%s", joined)
	}
}

func TestDiagnoseShadowMissingVendorWarns(t *testing.T) {
	lines := diagnoseShadow(shadowInputs{resolved: `C:\npm\muhiyacode.cmd`, isNpmShim: true, vendorExists: false, thisCommit: "72c7d51"})
	if !strings.Contains(strings.Join(lines, "\n"), "vendored exe is missing") {
		t.Fatalf("missing vendor exe must warn to reinstall: %v", lines)
	}
}

func TestDiagnoseShadowEnvOverrideNoted(t *testing.T) {
	lines := diagnoseShadow(shadowInputs{envBinary: `F:\build\muhiyacode.exe`, resolved: "", thisCommit: "72c7d51"})
	if !strings.Contains(strings.Join(lines, "\n"), "MUHIYACODE_BINARY") {
		t.Fatalf("MUHIYACODE_BINARY override must be surfaced: %v", lines)
	}
}

func TestWorkspaceDiagnosticsWritableAndGit(t *testing.T) {
	dir := t.TempDir()
	lines := workspaceDiagnostics(dir)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "writable") {
		t.Fatalf("temp dir must read as writable: %s", joined)
	}
	if !strings.Contains(joined, "not a git repository") {
		t.Fatalf("bare temp dir must read as non-git: %s", joined)
	}
	// Now make it a git repo (a .git dir is enough for the walk-up detector).
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(workspaceDiagnostics(dir), "\n"), "inside a git repository") {
		t.Fatal("a .git dir must be detected as a git repository")
	}
}
