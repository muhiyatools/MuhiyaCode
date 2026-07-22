package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

var capsuleIDPattern = regexp.MustCompile("^[a-f0-9]{64}$")

type CapsuleIndexRecord struct {
	Version int      `json:"version"`
	IDs     []string `json:"ids"`
}

func (s *Sessions) WriteCapsule(sessionID, id string, value any) error {
	if !capsuleIDPattern.MatchString(id) {
		return fmt.Errorf("invalid capsule id")
	}
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	capsuleDir := filepath.Join(dir, "capsules")
	if err := os.MkdirAll(capsuleDir, 0o700); err != nil {
		return err
	}
	target := filepath.Join(capsuleDir, id+".json")
	if existing, err := os.ReadFile(target); err == nil {
		incoming, marshalErr := json.MarshalIndent(value, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		incoming = append(incoming, '\n')
		if string(existing) != string(incoming) {
			return fmt.Errorf("immutable capsule %s already exists with different bytes", id)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := writeJSON(target, value, true); err != nil {
		return err
	}
	return s.rebuildCapsuleIndex(sessionID)
}

func (s *Sessions) ReadCapsules(sessionID string) ([]json.RawMessage, error) {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return nil, err
	}
	capsuleDir := filepath.Join(dir, "capsules")
	entries, err := os.ReadDir(capsuleDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []json.RawMessage
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || entry.Name() == "index.json" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(capsuleDir, entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		if !json.Valid(data) {
			continue
		}
		result = append(result, json.RawMessage(data))
	}
	return result, nil
}

func (s *Sessions) rebuildCapsuleIndex(sessionID string) error {
	dir, err := SessionDir(s.DB.paths, sessionID)
	if err != nil {
		return err
	}
	capsuleDir := filepath.Join(dir, "capsules")
	entries, err := os.ReadDir(capsuleDir)
	if err != nil {
		return err
	}
	ids := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && filepath.Ext(name) == ".json" {
			id := name[:len(name)-len(filepath.Ext(name))]
			if capsuleIDPattern.MatchString(id) {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	return writeJSON(filepath.Join(capsuleDir, "index.json"), CapsuleIndexRecord{Version: 1, IDs: ids}, false)
}
