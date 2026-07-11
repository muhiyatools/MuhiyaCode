//go:build !windows

package state

import "os"

func protectUserOnly(file string) error {
	return os.Chmod(file, 0o600)
}
