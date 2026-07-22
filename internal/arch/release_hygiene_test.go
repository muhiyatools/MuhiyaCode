package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallersUseCanonicalRepository(t *testing.T) {
	root := moduleRoot(t)
	for _, relative := range []string{"scripts/install.sh", "scripts/install.ps1", "scripts/npm-postinstall.js", "package.json"} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		if !strings.Contains(text, "muhiyatools/MuhiyaCode") {
			t.Errorf("%s does not reference the canonical repository", relative)
		}
		if strings.Contains(strings.ToLower(text), "muhiya/muhiyacode") {
			t.Errorf("%s still references the obsolete repository", relative)
		}
	}
}

func TestGeneratedCoverageArtifactsStayIgnored(t *testing.T) {
	root := moduleRoot(t)
	content, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "/coverage") || !strings.Contains(string(content), "/coverage.*") {
		t.Fatal(".gitignore must cover root coverage artifacts")
	}
}
