package tasksource

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"takt/internal/spec"
	"testing"
)

func TestProcessResolveAndSecretEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	d := t.TempDir()
	f := filepath.Join(d, "src")
	if err := os.WriteFile(f, []byte(`#!/bin/sh
read req
[ "$TOKEN" = "secret-value" ] || exit 9
printf '%s\n' '{"apiVersion":"takt-task-source/v1alpha1","kind":"ResolveResponse","task":{"apiVersion":"takt-task-source/v1alpha1","kind":"Task","id":"x","title":"Fix","goal":"Fix bug","source":{"adapter":"ignored","kind":"fixture","reference":"R-1","revision":"sha256:1"}}}'
`), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASK_TOKEN", "secret-value")
	r := Resolver{Name: "fixture", Spec: spec.TaskSourceSpec{Transport: "process", Argv: []string{f}, Env: map[string]string{"TOKEN": "secret://TASK_TOKEN"}}}
	task, err := r.Resolve(context.Background(), "R-1", d)
	if err != nil {
		t.Fatal(err)
	}
	if task.Source.Adapter != "fixture" || !strings.Contains(task.Goal, "Fix") {
		t.Fatalf("%+v", task)
	}
}
func TestProcessResolveMissingSecretFailsClosed(t *testing.T) {
	r := Resolver{Name: "fixture", Spec: spec.TaskSourceSpec{Transport: "process", Argv: []string{"true"}, Env: map[string]string{"TOKEN": "secret://TAKT_MISSING_TASKSOURCE_SECRET"}}}
	if _, err := r.Resolve(context.Background(), "R", ""); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("%v", err)
	}
}
