package store

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
)

func (f FS) ListRunIDs() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(f.Workspace, ".takt", "runs"))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || ValidateRunID(entry.Name()) != nil {
			continue
		}
		ids = append(ids, entry.Name())
	}
	sort.Strings(ids)
	return ids, nil
}
