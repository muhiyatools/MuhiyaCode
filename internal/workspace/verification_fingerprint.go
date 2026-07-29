package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// VerificationFingerprint identifies the code and manifests that can affect a
// verification command. Git repositories use HEAD plus staged, unstaged, and
// untracked content. Other workspaces hash their bounded source-file set.
func VerificationFingerprint(ctx context.Context, root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	if gitFingerprint(ctx, absolute, digest) == nil {
		return hex.EncodeToString(digest.Sum(nil)), nil
	}
	digest.Reset()
	files, err := CollectSourceFiles(absolute, false)
	if err != nil {
		return "", err
	}
	for _, name := range verificationManifestNames {
		path := filepath.Join(absolute, name)
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			files = append(files, path)
		}
	}
	config := filepath.Join(absolute, ".muhiya", "verification.json")
	if info, statErr := os.Stat(config); statErr == nil && !info.IsDir() {
		files = append(files, config)
	}
	sort.Strings(files)
	for _, path := range files {
		if err := hashFile(digest, absolute, path); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

var verificationManifestNames = []string{
	"go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml",
	"yarn.lock", "Cargo.toml", "Cargo.lock", "pyproject.toml",
}

func gitFingerprint(ctx context.Context, root string, digest hash.Hash) error {
	for _, args := range [][]string{
		{"-C", root, "rev-parse", "HEAD"},
		{"-C", root, "diff", "--no-ext-diff", "--binary", "HEAD", "--", "."},
	} {
		command := exec.CommandContext(ctx, "git", args...)
		command.Stdout = digest
		command.Stderr = io.Discard
		if err := command.Run(); err != nil {
			return err
		}
	}
	output, err := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		return err
	}
	for _, relative := range splitNUL(output) {
		if err := hashFile(digest, root, filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			return err
		}
	}
	return nil
}

func splitNUL(value []byte) []string {
	var result []string
	start := 0
	for index, item := range value {
		if item != 0 {
			continue
		}
		if index > start {
			result = append(result, string(value[start:index]))
		}
		start = index + 1
	}
	return result
}

func hashFile(digest hash.Hash, root, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(digest, "%s\x00", filepath.ToSlash(relative)); err != nil {
		return err
	}
	_, err = io.Copy(digest, file)
	return err
}
