// Package buildinfo exposes release metadata injected by the linker.
package buildinfo

// Version is the client version shown in the UI and `--version`. It defaults
// to the current release series for source builds and is overridden at release
// time via -ldflags "-X .../buildinfo.Version=<tag>".
var (
	Version = "1.3.1"
	Commit  = "unknown"
	Date    = "unknown"
)
