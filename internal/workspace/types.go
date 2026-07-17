// Package workspace provides MuhiyaCode's guarded filesystem and local process
// boundary. It is deliberately independent from the agent loop: callers inject
// trust, approval, and checkpoint metadata ports and can use the same service
// from the TUI, tests, or a headless runtime.
package workspace

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

const (
	DefaultReadLines         = 1500
	MaxReadLines             = 4000
	MaxReadBytes       int64 = 5_000_000
	MaxSearchBytes     int64 = 1_000_000
	MaxSnapshotBytes   int64 = 2_000_000
	DefaultOutputLimit       = 64 * 1024
	MaxShellTimeout          = 10 * time.Minute
)

var (
	ErrPermissionDenied = errors.New("workspace: permission denied")
	ErrSensitivePath    = errors.New("workspace: protected credential path")
	ErrOutsideWorkspace = errors.New("workspace: path is outside the workspace")
	ErrUnreadOverwrite  = errors.New(instructions.WorkspaceUnreadOverwriteBody)
	ErrInvalidArguments = errors.New("workspace: invalid tool arguments")
)

type Action string

const (
	ActionRead   Action = "read"
	ActionSearch Action = "search"
	ActionEdit   Action = "edit"
	ActionWrite  Action = "write"
	ActionPatch  Action = "patch"
	ActionShell  Action = "shell"
)

func (a Action) Mutates() bool {
	return a == ActionEdit || a == ActionWrite || a == ActionPatch || a == ActionShell
}

// ApprovalRequest contains enough detail for a TUI to display a useful,
// non-spoofable confirmation card.
type ApprovalRequest struct {
	Action           Action
	Workspace        string
	Path             string
	Command          string
	Reason           string
	OutsideWorkspace bool
	TrustWorkspace   bool
}

// Approver is implemented by the interactive UI. A nil Approver is fail
// closed for every operation that requires confirmation.
type Approver interface {
	Confirm(context.Context, ApprovalRequest) (bool, error)
}

// TrustStore persists workspace trust independently from this package.
type TrustStore interface {
	IsTrusted(context.Context, string) (bool, error)
	Trust(context.Context, string) error
}

// KnownFile reports whether a file is already in the agent's durable ledger.
// It lets a resumed session safely overwrite a file it created previously.
type KnownFile func(workspaceRelativePath string) bool

type Options struct {
	PermissionMode contract.PermissionMode
	Trust          TrustStore
	Approver       Approver
	SensitiveRoots []string
	Status         func(string)
	KnownFile      KnownFile

	PreferredShell string
	ShellTimeout   time.Duration
	OutputLimit    int
	ShellOutput    func(string)

	Checkpoints *CheckpointStore
	SkillRoots  []string
}

// Workspace owns all operations rooted at one canonical workspace directory.
type Workspace struct {
	root       string
	guard      *Guard
	options    Options
	readLedger *pathLedger
	shell      *ShellRunner
}

type ListOptions struct {
	Path       string
	MaxEntries int
	Recursive  bool
}

type ListResult struct {
	Root      string
	Entries   []ListEntry
	Truncated bool
}

type ListEntry struct {
	Path  string
	Name  string
	Depth int
	Mode  fs.FileMode
	Size  int64
}

type ReadOptions struct {
	Path   string
	Offset int // one-based
	Limit  int
}

type ReadResult struct {
	Path       string
	Offset     int
	TotalLines int
	Lines      []string
	Outline    string
	Remaining  int
}

type GrepOptions struct {
	Pattern    string
	Path       string
	Glob       string
	IgnoreCase bool
	Literal    bool
	MaxResults int
}

type Match struct {
	Path string
	Line int
	Text string
}

type SearchResult struct {
	Matches   []Match
	Truncated bool
}

type GlobOptions struct {
	Pattern    string
	Path       string
	MaxResults int
}

type GlobMatch struct {
	Path    string
	ModTime time.Time
	Size    int64
}

type Edit struct {
	Old        string `json:"oldString"`
	New        string `json:"newString"`
	ReplaceAll bool   `json:"replaceAll,omitempty"`
}

type EditResult struct {
	Path         string
	Changed      bool
	AppliedEdits int
	Replacements int
	Skipped      []string
	Notes        []string
	Summary      string
	Diff         string
}

type WriteResult struct {
	Path      string
	Created   bool
	Changed   bool
	LineCount int
	Summary   string
	Diff      string
}

type PatchResult struct {
	Files   []string
	Summary string
}

type GitDiffOptions struct {
	Staged      bool
	Path        string
	Context     int
	OutputLimit int
}

// New creates a guarded workspace rooted at root. The root must already be a
// directory so its canonical identity cannot change after construction.
func New(root string, options Options) (*Workspace, error) {
	canonical, err := CanonicalPath(root)
	if err != nil {
		return nil, err
	}
	info, err := fs.Stat(osDirFSRoot{}, canonical)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &fs.PathError{Op: "workspace", Path: canonical, Err: errors.New("not a directory")}
	}
	if options.ShellTimeout <= 0 {
		options.ShellTimeout = 2 * time.Minute
	}
	if options.ShellTimeout > MaxShellTimeout {
		options.ShellTimeout = MaxShellTimeout
	}
	if options.OutputLimit <= 0 {
		options.OutputLimit = DefaultOutputLimit
	}
	guard, err := NewGuard(canonical, GuardOptions{
		Mode:           options.PermissionMode,
		Trust:          options.Trust,
		Approver:       options.Approver,
		SensitiveRoots: options.SensitiveRoots,
		Status:         options.Status,
	})
	if err != nil {
		return nil, err
	}
	return &Workspace{
		root:       canonical,
		guard:      guard,
		options:    options,
		readLedger: newPathLedger(),
		shell: &ShellRunner{
			Preferred:   options.PreferredShell,
			Timeout:     options.ShellTimeout,
			OutputLimit: options.OutputLimit,
		},
	}, nil
}

// SetPermissionMode applies a permission change immediately to subsequent tool
// calls. Persisting the setting remains the caller's responsibility.
func (w *Workspace) SetPermissionMode(mode contract.PermissionMode) error {
	return w.guard.SetMode(mode)
}

// osDirFSRoot is a tiny adapter used so New's validation remains easy to test
// without exporting an implementation detail.
type osDirFSRoot struct{}

func (osDirFSRoot) Open(name string) (fs.File, error) { return os.Open(name) }
