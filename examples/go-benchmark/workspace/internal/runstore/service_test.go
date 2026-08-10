package runstore

import (
	"errors"
	"testing"
)

type recordingRepository struct {
	err       error
	committed *State
}

func (r *recordingRepository) Commit(state *State) error {
	r.committed = state
	return r.err
}

func TestCompleteReturnsPersistenceError(t *testing.T) {
	want := errors.New("disk full")
	repo := &recordingRepository{err: want}

	err := Complete(repo, &State{Status: "running"})
	if !errors.Is(err, want) {
		t.Fatalf("Complete() error = %v, want %v", err, want)
	}
}

func TestCompletePersistsCompletedState(t *testing.T) {
	repo := &recordingRepository{}
	state := &State{Status: "running"}

	if err := Complete(repo, state); err != nil {
		t.Fatal(err)
	}
	if repo.committed != state || state.Status != "completed" {
		t.Fatalf("persisted=%p state=%p status=%q", repo.committed, state, state.Status)
	}
}
