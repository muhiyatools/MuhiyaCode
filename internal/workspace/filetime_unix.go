//go:build !windows

package workspace

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func setFileTimes(file *os.File, modTime time.Time) error {
	value := unix.NsecToTimeval(modTime.UnixNano())
	return unix.Futimes(int(file.Fd()), []unix.Timeval{value, value})
}
