package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	patchJournalVersion  = 2
	maxPatchJournalBytes = 32 << 20
)

type patchJournalState string

const (
	patchPrepared   patchJournalState = "prepared"
	patchCommitting patchJournalState = "committing"
	patchCommitted  patchJournalState = "committed"
)

type patchJournalManifest struct {
	Version   int               `json:"version"`
	ID        string            `json:"id"`
	Workspace string            `json:"workspace"`
	State     patchJournalState `json:"state"`
	Changes   []patchChange     `json:"changes"`
	CreatedAt time.Time         `json:"createdAt"`
}

type patchJournal struct {
	directory string
	workspace string
	status    func(string)
}

type activePatchJournal struct {
	owner    *patchJournal
	path     string
	manifest patchJournalManifest
}

func newPatchJournal(store *CheckpointStore, workspace string, status func(string)) *patchJournal {
	if store == nil || store.SessionDir == "" {
		return nil
	}
	return &patchJournal{
		directory: filepath.Join(store.SessionDir, "transactions"),
		workspace: workspace,
		status:    status,
	}
}

func (journal *patchJournal) begin(changes []patchChange) (*activePatchJournal, error) {
	if journal == nil {
		return nil, nil
	}
	id, err := checkpointID()
	if err != nil {
		return nil, err
	}
	manifest := patchJournalManifest{
		Version:   patchJournalVersion,
		ID:        id,
		Workspace: journal.workspace,
		State:     patchPrepared,
		Changes:   append([]patchChange(nil), changes...),
		CreatedAt: time.Now().UTC(),
	}
	active := &activePatchJournal{
		owner:    journal,
		path:     filepath.Join(journal.directory, id+".json"),
		manifest: manifest,
	}
	if err := active.persist(); err != nil {
		return nil, err
	}
	return active, nil
}

func (transaction *activePatchJournal) persist() error {
	payload, err := json.Marshal(transaction.manifest)
	if err != nil {
		return err
	}
	if len(payload) > maxPatchJournalBytes {
		return fmt.Errorf("patch transaction exceeds %d-byte journal limit", maxPatchJournalBytes)
	}
	if err := os.MkdirAll(filepath.Dir(transaction.path), 0o700); err != nil {
		return err
	}
	return writeAtomic(transaction.path, payload, 0o600)
}

func (transaction *activePatchJournal) commit(workspaceRoot string) error {
	transaction.manifest.State = patchCommitting
	if err := transaction.persist(); err != nil {
		return err
	}
	for _, change := range transaction.manifest.Changes {
		if err := applyPatchChange(workspaceRoot, change); err != nil {
			return transaction.rollbackAfter(err)
		}
	}
	transaction.manifest.State = patchCommitted
	if err := transaction.persist(); err != nil {
		return transaction.rollbackAfter(fmt.Errorf("persist patch commit marker: %w", err))
	}
	if err := os.Remove(transaction.path); err != nil && !os.IsNotExist(err) {
		if transaction.owner.status != nil {
			transaction.owner.status("Patch committed; deferred transaction-journal cleanup: " + err.Error())
		}
	}
	return nil
}

func (transaction *activePatchJournal) rollbackAfter(cause error) error {
	rollbackErr := rollbackPatchChanges(transaction.manifest.Workspace, transaction.manifest.Changes)
	if rollbackErr == nil {
		if removeErr := os.Remove(transaction.path); removeErr != nil && !os.IsNotExist(removeErr) {
			rollbackErr = removeErr
		}
	}
	return errors.Join(cause, rollbackErr)
}

func (journal *patchJournal) recover() error {
	if journal == nil {
		return nil
	}
	entries, err := os.ReadDir(journal.directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := journal.recoverFile(filepath.Join(journal.directory, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (journal *patchJournal) recoverFile(path string) error {
	manifest, err := readPatchManifest(path)
	if err != nil {
		return err
	}
	if err := journal.validateManifest(manifest); err != nil {
		return fmt.Errorf("validate patch journal %s: %w", filepath.Base(path), err)
	}
	if manifest.State != patchCommitted {
		if err := rollbackPatchChanges(journal.workspace, manifest.Changes); err != nil {
			return fmt.Errorf("recover patch journal %s: %w", filepath.Base(path), err)
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readPatchManifest(path string) (patchJournalManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return patchJournalManifest{}, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxPatchJournalBytes+1))
	if err != nil {
		return patchJournalManifest{}, err
	}
	if len(payload) > maxPatchJournalBytes {
		return patchJournalManifest{}, fmt.Errorf("patch journal exceeds %d-byte limit", maxPatchJournalBytes)
	}
	var manifest patchJournalManifest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return patchJournalManifest{}, fmt.Errorf("decode patch journal: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return patchJournalManifest{}, fmt.Errorf("decode patch journal: %w", err)
	}
	return manifest, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func (journal *patchJournal) validateManifest(manifest patchJournalManifest) error {
	if manifest.Version != patchJournalVersion || manifest.ID == "" {
		return fmt.Errorf("unsupported or incomplete manifest")
	}
	if normalizePath(manifest.Workspace) != normalizePath(journal.workspace) {
		return fmt.Errorf("workspace identity mismatch")
	}
	switch manifest.State {
	case patchPrepared, patchCommitting, patchCommitted:
	default:
		return fmt.Errorf("invalid state %q", manifest.State)
	}
	for _, change := range manifest.Changes {
		inside, err := IsInsideReal(journal.workspace, change.Path)
		if err != nil || !inside {
			return fmt.Errorf("transaction target is outside workspace: %s", change.Path)
		}
	}
	return nil
}
