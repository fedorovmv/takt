package cliargs

import (
	"reflect"
	"testing"
)

func TestInjectPlacesManagedFlagsBeforeGoalSeparator(t *testing.T) {
	args := []string{"host", "begin", "--", "fix bug"}
	managed := []string{"--workspace", "/tmp/work", "--json"}
	want := []string{"host", "begin", "--workspace", "/tmp/work", "--json", "--", "fix bug"}

	if got := Inject(args, managed); !reflect.DeepEqual(got, want) {
		t.Fatalf("Inject() = %#v, want %#v", got, want)
	}
}

func TestInjectDoesNotMutateInputs(t *testing.T) {
	args := []string{"run", "--", "goal"}
	managed := []string{"--json"}
	originalArgs := append([]string(nil), args...)
	originalManaged := append([]string(nil), managed...)

	_ = Inject(args, managed)

	if !reflect.DeepEqual(args, originalArgs) || !reflect.DeepEqual(managed, originalManaged) {
		t.Fatalf("Inject mutated inputs: args=%#v managed=%#v", args, managed)
	}
}
