package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ReadEvents returns persisted events with revision greater than afterRevision.
// Results preserve append order and are capped by limit when limit is positive.
func (f FS) ReadEvents(id string, afterRevision uint64, limit int) ([]Event, error) {
	if err := ValidateRunID(id); err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(f.RunDir(id), "events.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	events := make([]Event, 0)
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 4*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, &InconsistentError{RunID: id, Err: fmt.Errorf("decode event line %d: %w", line, err)}
		}
		if event.RunID != "" && event.RunID != id {
			return nil, &InconsistentError{RunID: id, Err: fmt.Errorf("event line %d contains run id %q", line, event.RunID)}
		}
		if event.Revision <= afterRevision {
			continue
		}
		events = append(events, event)
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}
