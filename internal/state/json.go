package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// readJSON preserves the legacy recovery behavior: a missing file returns the
// supplied default, while malformed JSON is moved aside before the default is
// returned. Schema validation remains the caller's responsibility.
func readJSON[T any](file string, fallback T) (T, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return fallback, nil
	}
	if err != nil {
		return fallback, fmt.Errorf("read %s: %w", file, err)
	}
	var value T
	if err := json.Unmarshal(data, &value); err == nil {
		return value, nil
	}
	backup := fmt.Sprintf("%s.corrupt-%d.bak", file, time.Now().UnixMilli())
	if renameErr := os.Rename(file, backup); renameErr != nil {
		return fallback, fmt.Errorf("parse %s and preserve corrupt copy: %w", file, renameErr)
	}
	return fallback, nil
}

// writeJSON performs a same-directory, fsync-before-replace write. When secure
// is true, the temporary file receives user-only permissions before it ever
// becomes visible at the destination path.
func writeJSON(file string, value any, secure bool) error {
	var payload bytes.Buffer
	encoder := json.NewEncoder(&payload)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode %s: %w", file, err)
	}
	return writeFileAtomic(file, payload.Bytes(), secure)
}

func writeFileAtomic(file string, data []byte, secure bool) (retErr error) {
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(file)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if retErr != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("set temporary file mode: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write temporary state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary state file: %w", err)
	}
	if secure {
		if err := protectUserOnly(tmpName); err != nil {
			return fmt.Errorf("protect secret file: %w", err)
		}
	}
	if err := replaceFile(tmpName, file); err != nil {
		return fmt.Errorf("replace %s: %w", file, err)
	}
	return nil
}
