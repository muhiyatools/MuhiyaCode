//go:build windows

package workspace

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func setFileTimes(file *os.File, modTime time.Time) error {
	value := windows.NsecToFiletime(modTime.UnixNano())
	handle := windows.Handle(file.Fd())
	return windows.SetFileTime(handle, nil, &value, &value)
}
