package workspace

import (
	"fmt"
	"os"
)

// friendlyPathError wraps a raw OS filesystem error — Windows' GetFileAttributesEx,
// Unix's ENOENT/EACCES — with a plain-language message and a recovery hint, so a
// tool result never hands the model an opaque syscall name (Stability Overhaul
// T053, defect D9). Unrecognized errors pass through unchanged.
func friendlyPathError(err error, path string) error {
	if err == nil {
		return nil
	}
	switch {
	case os.IsNotExist(err):
		return fmt.Errorf("path does not exist: %s — check the path, or list its parent directory / glob to find the right one", path)
	case os.IsPermission(err):
		return fmt.Errorf("access denied: %s", path)
	}
	return err
}
