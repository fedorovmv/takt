package session

import (
	"strings"
	"testing"
)

func TestResolveAcceptsExactResumedSession(t *testing.T) {
	id, resumed, err := Resolve("ses-123", []string{"ses-123", "ses-123"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "ses-123" || !resumed {
		t.Fatalf("Resolve() = id %q resumed %t", id, resumed)
	}
}

func TestResolveRejectsResumeMismatch(t *testing.T) {
	_, _, err := Resolve("ses-123", []string{"ses-new"})
	if err == nil || !strings.Contains(err.Error(), "ses-123") || !strings.Contains(err.Error(), "ses-new") {
		t.Fatalf("Resolve() error = %v, want requested and observed IDs", err)
	}
}

func TestResolveFreshSession(t *testing.T) {
	id, resumed, err := Resolve("", []string{"ses-new"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "ses-new" || resumed {
		t.Fatalf("Resolve() = id %q resumed %t", id, resumed)
	}
}
