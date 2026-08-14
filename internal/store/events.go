package store

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const eventIndexFile = "events.idx"

// buildEventIndex stores one fixed-width byte offset per event revision. Commit
// replaces events.jsonl before this index, so an older index always remains
// valid for the unchanged prefix during a concurrent read.
func buildEventIndex(events []byte) []byte {
	offsets := make([]byte, 0, 8)
	atLineStart := true
	for offset, value := range events {
		if atLineStart && value != '\n' && value != '\r' {
			var encoded [8]byte
			binary.BigEndian.PutUint64(encoded[:], uint64(offset))
			offsets = append(offsets, encoded[:]...)
			atLineStart = false
		}
		if value == '\n' {
			atLineStart = true
		}
	}
	return offsets
}

// eventOffset returns the byte offset of the first event whose revision is
// greater than afterRevision. indexed reports that an index exists; found is
// false when the cursor is already at the indexed end of the journal.
func (f FS) eventOffset(id string, afterRevision uint64) (offset int64, indexed bool, found bool) {
	if afterRevision == 0 {
		return 0, true, true
	}
	file, err := os.Open(filepath.Join(f.RunDir(id), eventIndexFile))
	if err != nil {
		return 0, false, false
	}
	defer file.Close()
	var encoded [8]byte
	if _, err := file.ReadAt(encoded[:], int64(afterRevision*8)); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, true, false
		}
		return 0, false, false
	}
	return int64(binary.BigEndian.Uint64(encoded[:])), true, true
}

// ReadEvents returns persisted events with revision greater than afterRevision.
// Results preserve append order and are capped by limit when limit is positive.
// Incremental reads seek directly to the indexed event instead of rescanning
// the whole journal. Legacy Runs without an index fall back to a full scan.
func (f FS) ReadEvents(id string, afterRevision uint64, limit int) ([]Event, error) {
	if err := ValidateRunID(id); err != nil {
		return nil, err
	}
	release, err := acquireReadCommitLock(f.RunDir(id))
	if err != nil {
		return nil, err
	}
	if release != nil {
		defer release()
	}
	file, err := os.Open(filepath.Join(f.RunDir(id), "events.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if offset, indexed, found := f.eventOffset(id, afterRevision); indexed {
		if !found {
			return []Event{}, nil
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
	}

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
