//go:build windows

package state

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
	replaceRetries          = 10
	replaceBackoff          = 20 * time.Millisecond
)

var moveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

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

func replaceFile(source, target string) error {
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
		result, _, callErr := moveFileExW.Call(
			uintptr(unsafe.Pointer(from)),
			uintptr(unsafe.Pointer(to)),
			moveFileReplaceExisting|moveFileWriteThrough,
		)
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
