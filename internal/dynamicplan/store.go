package dynamicplan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Store struct{ Workspace string }

func (s Store) Root() string          { return filepath.Join(s.Workspace, ".takt", "plans") }
func (s Store) Dir(id string) string  { return filepath.Join(s.Root(), id) }
func (s Store) Path(id string) string { return filepath.Join(s.Dir(id), "plan.json") }

func ValidateID(id string) error {
	if !strings.HasPrefix(id, "plan-") || len(id) > 80 || strings.ContainsAny(id, `/\\`) || strings.Contains(id, "..") {
		return fmt.Errorf("invalid plan id %q", id)
	}
	return nil
}

func (s Store) Save(record *Record) error {
	if record == nil {
		return fmt.Errorf("plan record is required")
	}
	if err := ValidateID(record.ID); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(s.Dir(record.ID), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir(record.ID), ".plan-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.Path(record.ID))
}

func (s Store) Load(id string) (*Record, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(s.Path(id))
	if err != nil {
		return nil, err
	}
	var record Record
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&record); err != nil {
		return nil, err
	}
	if record.Results == nil {
		record.Results = map[string]string{}
	}
	return &record, nil
}

func (s Store) List() ([]*Record, error) {
	entries, err := os.ReadDir(s.Root())
	if errors.Is(err, os.ErrNotExist) {
		return []*Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]*Record, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		record, loadErr := s.Load(entry.Name())
		if loadErr == nil {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
