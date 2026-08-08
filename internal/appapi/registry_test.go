package appapi

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"takt/internal/testsupport/appfixture"
)

func TestCanonicalOperationRegistry(t *testing.T) {
	services, err := appfixture.New(t.TempDir(), ".takt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	registry := New(services)
	got := make([]string, 0, len(registry.handlers))
	for id := range registry.handlers {
		got = append(got, id)
	}
	sort.Strings(got)
	want := []string{
		"block.describe", "block.list",
		"host.begin", "host.confirm", "host.find", "host.get", "host.guard_completion", "host.guard_tool", "host.release",
		"notify.ack", "notify.dispatch", "notify.list", "notify.test",
		"plan.create", "plan.execute", "plan.get", "plan.promote", "plan.steer",
		"run.abandon", "run.answer", "run.artifacts", "run.attention", "run.cancel", "run.children", "run.events", "run.fork", "run.get", "run.list", "run.pause", "run.recover", "run.resume", "run.resume_paused", "run.retry", "run.start", "run.summary",
		"task.explain", "task.respond", "task.start", "task.status", "task.stop",
		"workflow.describe", "workflow.list",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operation registry mismatch\n got=%v\nwant=%v", got, want)
	}
}

func TestCanonicalOperationRejectsUnknownMethodAndFields(t *testing.T) {
	services, err := appfixture.New(t.TempDir(), ".takt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	registry := New(services)
	if _, err := registry.Call(context.Background(), "missing.operation", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected unknown operation error")
	}
	if _, err := registry.Call(context.Background(), "run.get", json.RawMessage(`{"run_id":"x","surprise":true}`)); err == nil {
		t.Fatal("expected strict argument decoding error")
	}
}
