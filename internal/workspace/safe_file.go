package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/muhiya/muhiyacode/internal/instructions"
)

// rootedTarget holds an operating-system directory handle and a path relative
// to it. os.Root rejects escapes and resolves every component relative to the
// opened handle, closing the authorize-then-symlink-swap race.
type rootedTarget struct {
	root *os.Root
	name string
}

func openRootedTarget(workspaceRoot, target string) (rootedTarget, error) {
	base := workspaceRoot
	if !IsInside(workspaceRoot, target) {
		base = nearestExistingDirectory(filepath.Dir(target))
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return rootedTarget{}, err
	}
	name, err := filepath.Rel(base, target)
	if err != nil || filepath.IsAbs(name) {
		root.Close()
		return rootedTarget{}, fmt.Errorf("resolve rooted target %s: %w", target, err)
	}
	return rootedTarget{root: root, name: name}, nil
}

func nearestExistingDirectory(path string) string {
	for {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

func (target rootedTarget) close() {
	_ = target.root.Close()
}

func safeReadTarget(workspaceRoot, path string) ([]byte, fs.FileInfo, error) {
	target, err := openRootedTarget(workspaceRoot, path)
	if err != nil {
		return nil, nil, err
	}
	defer target.close()
	content, err := target.root.ReadFile(target.name)
	if err != nil {
		return nil, nil, err
	}
	info, err := target.root.Stat(target.name)
	return content, info, err
}

func safeStatTarget(workspaceRoot, path string) (fs.FileInfo, error) {
	target, err := openRootedTarget(workspaceRoot, path)
	if err != nil {
		return nil, err
	}
	defer target.close()
	return target.root.Stat(target.name)
}

func safeReadTargetLimit(workspaceRoot, path string, limit int64) ([]byte, error) {
	target, err := openRootedTarget(workspaceRoot, path)
	if err != nil {
		return nil, err
	}
	defer target.close()
	file, err := target.root.Open(target.name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf(instructions.WorkspaceFileExceedsLimitTmpl, limit)
	}
	return data, nil
}

func safeWriteTarget(workspaceRoot, path string, content []byte, mode fs.FileMode) error {
	target, err := openRootedTarget(workspaceRoot, path)
	if err != nil {
		return err
	}
	defer target.close()
	parent := filepath.Dir(target.name)
	if parent != "." {
		if err := target.root.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	tempName, file, err := createRootedTemp(target.root, parent, mode)
	if err != nil {
		return err
	}
	defer target.root.Remove(tempName)
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return target.root.Rename(tempName, target.name)
}

func createRootedTemp(root *os.Root, parent string, mode fs.FileMode) (string, *os.File, error) {
	for attempt := 0; attempt < 20; attempt++ {
		suffix := make([]byte, 8)
		if _, err := rand.Read(suffix); err != nil {
			return "", nil, err
		}
		name := filepath.Join(parent, ".muhiya-"+hex.EncodeToString(suffix)+".tmp")
		file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		if err := file.Chmod(mode); err != nil {
			file.Close()
			root.Remove(name)
			return "", nil, err
		}
		return name, file, nil
	}
	return "", nil, fmt.Errorf("allocate temporary file for rooted write")
}

func safeRemoveTarget(workspaceRoot, path string) error {
	target, err := openRootedTarget(workspaceRoot, path)
	if err != nil {
		return err
	}
	defer target.close()
	return target.root.Remove(target.name)
}

func safeSetTimes(workspaceRoot, path string, modTime time.Time) error {
	target, err := openRootedTarget(workspaceRoot, path)
	if err != nil {
		return err
	}
	defer target.close()
	file, err := target.root.OpenFile(target.name, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return setFileTimes(file, modTime)
}
