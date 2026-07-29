package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type patchChange struct {
	Path          string      `json:"path"`
	Before        string      `json:"before"`
	After         string      `json:"after"`
	Existed       bool        `json:"existed"`
	Drop          bool        `json:"drop"`
	BeforeMode    os.FileMode `json:"beforeMode"`
	AfterMode     os.FileMode `json:"afterMode"`
	BeforeModTime time.Time   `json:"beforeModTime"`
	AfterModTime  time.Time   `json:"afterModTime"`
}

func (w *Workspace) preparePatchChanges(ctx context.Context, patches []unifiedPatch) ([]patchChange, error) {
	changes := make([]patchChange, 0, len(patches))
	for _, patch := range patches {
		change, err := w.preparePatchChange(ctx, patch)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func (w *Workspace) preparePatchChange(ctx context.Context, patch unifiedPatch) (patchChange, error) {
	name := patch.newName
	if name == "/dev/null" || name == "" {
		name = patch.oldName
	}
	name = strings.TrimPrefix(strings.TrimPrefix(name, "a/"), "b/")
	target, err := w.authorizePath(ctx, ActionPatch, name)
	if err != nil {
		return patchChange{}, err
	}
	before, existed, mode, modTime, err := readPatchTarget(w.root, target)
	if err != nil {
		return patchChange{}, err
	}
	if existed && !w.canOverwrite(target) {
		return patchChange{}, fmt.Errorf("%w: %s", ErrUnreadOverwrite, name)
	}
	after, err := patchedContent(before, patch)
	if err != nil {
		return patchChange{}, fmt.Errorf("patch %s: %w", name, err)
	}
	return patchChange{
		Path: target, Before: before, After: after, Existed: existed,
		Drop: patch.newName == "/dev/null", BeforeMode: mode, AfterMode: mode,
		BeforeModTime: modTime,
	}, nil
}

func readPatchTarget(root, path string) (string, bool, os.FileMode, time.Time, error) {
	source, fileInfo, err := safeReadTarget(root, path)
	if os.IsNotExist(err) {
		return "", false, 0o644, time.Time{}, nil
	}
	if err != nil {
		return "", false, 0, time.Time{}, err
	}
	return string(source), true, fileInfo.Mode(), fileInfo.ModTime(), nil
}

func patchedContent(before string, patch unifiedPatch) (string, error) {
	normalized := strings.ReplaceAll(before, "\r\n", "\n")
	after, err := applyHunks(normalized, patch.hunks)
	if err != nil {
		return "", err
	}
	if dominantCRLF(before) {
		after = strings.ReplaceAll(after, "\n", "\r\n")
	}
	return after, nil
}

func (w *Workspace) commitPatchChanges(changes []patchChange) error {
	transaction, err := w.patchJournal.begin(changes)
	if err != nil {
		return err
	}
	if transaction != nil {
		if err := transaction.commit(w.root); err != nil {
			return err
		}
	} else if err := w.commitPatchChangesWithoutJournal(changes); err != nil {
		return err
	}
	for _, change := range changes {
		w.readLedger.add(change.Path)
	}
	return nil
}

func (w *Workspace) commitPatchChangesWithoutJournal(changes []patchChange) error {
	committed := make([]patchChange, 0, len(changes))
	for _, change := range changes {
		if err := applyPatchChange(w.root, change); err != nil {
			return errors.Join(err, rollbackPatchChanges(w.root, committed))
		}
		committed = append(committed, change)
	}
	return nil
}

func applyPatchChange(root string, change patchChange) error {
	if change.Drop {
		if err := safeRemoveTarget(root, change.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := safeWriteTarget(root, change.Path, []byte(change.After), change.AfterMode.Perm()); err != nil {
		return err
	}
	if !change.AfterModTime.IsZero() {
		return safeSetTimes(root, change.Path, change.AfterModTime)
	}
	return nil
}

func rollbackPatchChanges(root string, changes []patchChange) error {
	var rollbackErrors []error
	for index := len(changes) - 1; index >= 0; index-- {
		if err := restorePatchChange(root, changes[index]); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	return errors.Join(rollbackErrors...)
}

func restorePatchChange(root string, change patchChange) error {
	if !change.Existed {
		if err := safeRemoveTarget(root, change.Path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove newly created %s: %w", change.Path, err)
		}
		return nil
	}
	if err := safeWriteTarget(root, change.Path, []byte(change.Before), change.BeforeMode.Perm()); err != nil {
		return fmt.Errorf("restore %s: %w", change.Path, err)
	}
	if err := safeSetTimes(root, change.Path, change.BeforeModTime); err != nil {
		return fmt.Errorf("restore timestamps for %s: %w", change.Path, err)
	}
	return nil
}

func patchChangePaths(changes []patchChange) []string {
	paths := make([]string, len(changes))
	for index := range changes {
		paths[index] = changes[index].Path
	}
	return paths
}

func patchResult(root string, changes []patchChange) PatchResult {
	result := PatchResult{Files: make([]string, 0, len(changes))}
	for _, change := range changes {
		result.Files = append(result.Files, relativeSlash(root, change.Path))
	}
	result.Summary = fmt.Sprintf("Applied patch to %d file(s): %s.", len(result.Files), strings.Join(result.Files, ", "))
	return result
}
