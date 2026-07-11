//go:build !windows

package workspace

import "os"

func replaceAtomic(source, target string) error { return os.Rename(source, target) }
