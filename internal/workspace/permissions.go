package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type GuardOptions struct {
	Mode           contract.PermissionMode
	Trust          TrustStore
	Approver       Approver
	SensitiveRoots []string
	Status         func(string)
}

// Guard centralizes authorization so no tool accidentally implements a weaker
// variant of workspace containment or trust policy.
type Guard struct {
	root             string
	modeMu           sync.RWMutex
	mode             contract.PermissionMode
	trust            TrustStore
	approver         Approver
	status           func(string)
	sensitive        []string
	sensitiveMatch   []*regexp.Regexp
	sensitiveMatchOf []string
	trustMu          sync.Mutex
}

// SetMode updates the live authorization policy. The guard is shared by tool
// calls and the TUI, so reads and updates are synchronized explicitly.
func (g *Guard) SetMode(mode contract.PermissionMode) error {
	if mode != contract.PermissionNormal && mode != contract.PermissionAutoAccept {
		return fmt.Errorf("invalid permission mode %q", mode)
	}
	g.modeMu.Lock()
	g.mode = mode
	g.modeMu.Unlock()
	return nil
}

func (g *Guard) Mode() contract.PermissionMode {
	g.modeMu.RLock()
	defer g.modeMu.RUnlock()
	return g.mode
}

func NewGuard(root string, options GuardOptions) (*Guard, error) {
	canonicalRoot, err := CanonicalPath(root)
	if err != nil {
		return nil, err
	}
	roots := options.SensitiveRoots
	if roots == nil {
		roots = defaultSensitiveRoots()
	}
	canonicalSensitive := make([]string, 0, len(roots))
	for _, candidate := range roots {
		canonical, err := CanonicalPath(candidate)
		if err == nil {
			canonicalSensitive = append(canonicalSensitive, canonical)
		}
	}
	matchers := make([]*regexp.Regexp, 0, len(canonicalSensitive))
	labels := make([]string, 0, len(canonicalSensitive))
	for _, root := range canonicalSensitive {
		base := filepath.Base(root)
		if base == "" || base == "." || base == string(filepath.Separator) {
			continue
		}
		matchers = append(matchers, regexp.MustCompile(`(?i)(?:^|[\\/'"( ]|~)`+regexp.QuoteMeta(base)+`(?:$|[\\/'") ])`))
		labels = append(labels, root)
	}
	return &Guard{
		root:             canonicalRoot,
		mode:             options.Mode,
		trust:            options.Trust,
		approver:         options.Approver,
		status:           options.Status,
		sensitive:        canonicalSensitive,
		sensitiveMatch:   matchers,
		sensitiveMatchOf: labels,
	}, nil
}

func (g *Guard) emit(message string) {
	if g.status != nil {
		g.status(message)
	}
}

func (g *Guard) ApprovePath(ctx context.Context, action Action, target string) (string, error) {
	canonical, err := CanonicalPath(target)
	if err != nil {
		return "", err
	}
	for _, root := range g.sensitive {
		if IsInside(root, canonical) {
			g.emit(fmt.Sprintf("Blocked %s: %s is a protected credential location.", action, target))
			return "", fmt.Errorf("%w: %s", ErrSensitivePath, target)
		}
	}
	outside := !IsInside(g.root, canonical)
	if outside {
		approved, err := g.confirm(ctx, ApprovalRequest{
			Action:           action,
			Workspace:        g.root,
			Path:             canonical,
			OutsideWorkspace: true,
			Reason:           fmt.Sprintf("%s wants access outside the workspace", action),
		})
		if err != nil || !approved {
			if err != nil {
				return "", err
			}
			return "", ErrPermissionDenied
		}
	}
	if action.Mutates() {
		if err := g.ensureTrusted(ctx, action); err != nil {
			return "", err
		}
		if g.Mode() != contract.PermissionAutoAccept {
			approved, err := g.confirm(ctx, ApprovalRequest{
				Action:    action,
				Workspace: g.root,
				Path:      canonical,
				Reason:    fmt.Sprintf("allow %s on %s", action, canonical),
			})
			if err != nil || !approved {
				if err != nil {
					return "", err
				}
				return "", ErrPermissionDenied
			}
		}
	}
	return canonical, nil
}

func (g *Guard) ApproveShell(ctx context.Context, command string) error {
	risk := ClassifyShell(command)
	if risk.Blocked {
		g.emit("Blocked shell command: " + risk.Reason)
		return fmt.Errorf("%w: %s", ErrPermissionDenied, risk.Reason)
	}
	if root, ok := g.sensitiveShellReference(command); ok {
		g.emit(fmt.Sprintf("Blocked shell command: %s is a protected credential location.", root))
		return fmt.Errorf("%w: %s", ErrSensitivePath, root)
	}
	if err := g.ensureTrusted(ctx, ActionShell); err != nil {
		return err
	}
	if g.Mode() == contract.PermissionAutoAccept {
		return nil
	}
	approved, err := g.confirm(ctx, ApprovalRequest{
		Action:    ActionShell,
		Workspace: g.root,
		Command:   command,
		Reason:    "allow shell command",
	})
	if err != nil {
		return err
	}
	if !approved {
		return ErrPermissionDenied
	}
	return nil
}

// sensitiveShellReference is a textual backstop: it cannot resolve the
// arguments of an arbitrary shell command the way ApprovePath resolves a real
// path, but it blocks the common case of a command that names a protected
// credential root literally (e.g. "cat ~/.ssh/id_rsa", "type %USERPROFILE%\.aws\credentials").
func (g *Guard) sensitiveShellReference(command string) (string, bool) {
	for i, matcher := range g.sensitiveMatch {
		if matcher.MatchString(command) {
			return g.sensitiveMatchOf[i], true
		}
	}
	return "", false
}

func (g *Guard) ensureTrusted(ctx context.Context, action Action) error {
	g.trustMu.Lock()
	defer g.trustMu.Unlock()
	trusted := false
	if g.trust != nil {
		var err error
		trusted, err = g.trust.IsTrusted(ctx, g.root)
		if err != nil {
			return err
		}
	}
	if trusted {
		return nil
	}
	approved, err := g.confirm(ctx, ApprovalRequest{
		Action:         action,
		Workspace:      g.root,
		Path:           g.root,
		TrustWorkspace: true,
		Reason:         fmt.Sprintf("trust this workspace before %s", action),
	})
	if err != nil {
		return err
	}
	if !approved {
		return ErrPermissionDenied
	}
	if g.trust == nil {
		// Without a durable trust port, confirmation authorizes this operation
		// only; it must never silently upgrade to persisted trust.
		return nil
	}
	return g.trust.Trust(ctx, g.root)
}

func (g *Guard) confirm(ctx context.Context, request ApprovalRequest) (bool, error) {
	if g.approver == nil {
		return false, nil
	}
	return g.approver.Confirm(ctx, request)
}

// MemoryTrustStore is useful for ephemeral/headless sessions and tests.
type MemoryTrustStore struct {
	mu      sync.RWMutex
	trusted map[string]struct{}
}

func NewMemoryTrustStore() *MemoryTrustStore {
	return &MemoryTrustStore{trusted: make(map[string]struct{})}
}

func (m *MemoryTrustStore) IsTrusted(_ context.Context, path string) (bool, error) {
	m.mu.RLock()
	_, ok := m.trusted[normalizePath(path)]
	m.mu.RUnlock()
	return ok, nil
}

func (m *MemoryTrustStore) Trust(_ context.Context, path string) error {
	m.mu.Lock()
	m.trusted[normalizePath(path)] = struct{}{}
	m.mu.Unlock()
	return nil
}

type ApproverFunc func(context.Context, ApprovalRequest) (bool, error)

func (f ApproverFunc) Confirm(ctx context.Context, request ApprovalRequest) (bool, error) {
	return f(ctx, request)
}
