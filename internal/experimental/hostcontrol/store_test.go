package hostcontrol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFindSkipsTerminalSessions(t *testing.T) {
	store := Store{Workspace: t.TempDir()}
	now := time.Now().UTC()
	old := &Session{ID: "host-0123456789abcdef", Host: "pi", HostSessionID: "same", PlanID: "plan-old", Status: StatusCompleted, CreatedAt: now, UpdatedAt: now}
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Find("pi", "same"); !os.IsNotExist(err) {
		t.Fatalf("terminal session was reused: %v", err)
	}
	active := &Session{ID: "host-fedcba9876543210", Host: "pi", HostSessionID: "same", PlanID: "plan-new", Status: StatusPreview, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	if err := store.Save(active); err != nil {
		t.Fatal(err)
	}
	got, err := store.Find("pi", "same")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != active.ID {
		t.Fatalf("found %s, want %s", got.ID, active.ID)
	}
}

func TestListFailsClosedOnCorruptSession(t *testing.T) {
	store := Store{Workspace: t.TempDir()}
	if err := os.MkdirAll(store.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root(), "host-corrupt.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := store.List()
	if err == nil || !strings.Contains(err.Error(), "load host session") {
		t.Fatalf("expected corrupt-session error, got %v", err)
	}
}
