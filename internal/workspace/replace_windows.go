//go:build windows

package workspace

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

const (
	replaceRetries = 10
	replaceBackoff = 20 * time.Millisecond
)

// isTransientReplaceError reports whether MoveFileExW failed because the
// target was momentarily locked by another process (an editor, an antivirus
// scan, a search indexer) rather than a real, permanent failure. This is
// routine on Windows and essentially impossible on POSIX rename().
func isTransientReplaceError(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case 5, 32, 33: // ERROR_ACCESS_DENIED, ERROR_SHARING_VIOLATION, ERROR_LOCK_VIOLATION
		return true
	default:
		return false
	}
}

func replaceAtomic(source, target string) error {
	from, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < replaceRetries; attempt++ {
		result, _, callErr := moveFileEx.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), 0x1|0x8)
		if result != 0 {
			return nil
		}
		lastErr = callErr
		if !isTransientReplaceError(callErr) {
			break
		}
		time.Sleep(replaceBackoff)
	}
	return fmt.Errorf("MoveFileExW: %w", lastErr)
}
