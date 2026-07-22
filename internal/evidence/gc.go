package evidence

import (
	"os"
	"strings"
)

func (store *Store) Collect(marked map[string]bool) (int, error) {
	entries, err := os.ReadDir(store.metadataDir())
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		handle := strings.TrimSuffix(entry.Name(), ".json")
		metadata, err := store.Metadata(handle)
		if err != nil || marked[handle] || metadata.Retention == RetentionDebug {
			continue
		}
		if os.Remove(store.metadataPath(handle)) == nil {
			removed++
		}
	}
	if err := store.RecoverOrphans(); err != nil {
		return removed, err
	}
	return removed, nil
}
