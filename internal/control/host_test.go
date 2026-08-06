package control

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/dynamicplan"
	"takt/internal/hostcontrol"
	"takt/internal/profile"
)

func TestHostBeginBindsPlanBeforeManagedExecution(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, filepath.Join(workspace, ".takt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateDynamicPlan()
	result, err := service.BeginHostSession(context.Background(), HostBeginRequest{Host: "pi", HostSessionID: "pi-session-1", Goal: candidate.Goal, Profile: "code", Enforcement: hostcontrol.EnforcementStrict, Capabilities: hostcontrol.Capabilities{CommandInterception: true, InputInterception: true, ToolCallBlocking: true, CompletionBlocking: true, SessionRecovery: true}, Candidate: &candidate})
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.Status != hostcontrol.StatusPreview || result.Session.PlanID != result.Plan.PlanID || result.Session.Enforcement != hostcontrol.EnforcementStrict {
		t.Fatalf("unexpected host begin result: %#v", result)
	}
	recovered, err := service.FindHostSession("pi", "pi-session-1")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Session.ID != result.Session.ID || recovered.Plan.Record.Status != "draft" {
		t.Fatalf("unexpected recovered session: %#v", recovered)
	}
}

func TestStrictHostRequiresBlockingCapabilities(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, filepath.Join(workspace, ".takt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateDynamicPlan()
	_, err = service.BeginHostSession(context.Background(), HostBeginRequest{Host: "opencode", HostSessionID: "ses-incomplete", Goal: candidate.Goal, Profile: "code", Enforcement: hostcontrol.EnforcementStrict, Capabilities: hostcontrol.Capabilities{ToolCallBlocking: true}, Candidate: &candidate})
	if err == nil {
		t.Fatal("expected strict host capability rejection")
	}
}

func TestManagedHostBlocksMutationAndFinalCompletion(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, filepath.Join(workspace, ".takt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan := candidateDynamicPlan()
	now := time.Now().UTC()
	record := &dynamicplan.Record{ID: "plan-hostguard123456", Status: "running", Profile: "code", ConfigPath: service.ConfigPath, CreatedAt: now, UpdatedAt: now, Results: map[string]string{}, Revisions: []dynamicplan.Revision{{Number: 1, Reason: "test", CreatedAt: now, Plan: plan}}}
	if err := (dynamicplan.Store{Workspace: workspace}).Save(record); err != nil {
		t.Fatal(err)
	}
	session := &hostcontrol.Session{ID: "host-0123456789abcdef", Host: "opencode", HostSessionID: "ses-1", PlanID: record.ID, Status: hostcontrol.StatusManaged, Enforcement: hostcontrol.EnforcementStrict, CreatedAt: now, UpdatedAt: now}
	if err := (hostcontrol.Store{Workspace: workspace}).Save(session); err != nil {
		t.Fatal(err)
	}
	mutation, err := service.GuardHostTool(HostToolGuardRequest{SessionID: session.ID, Tool: "edit", Category: "write"})
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Allowed {
		t.Fatalf("mutation escaped managed mode: %#v", mutation)
	}
	spoofed, err := service.GuardHostTool(HostToolGuardRequest{SessionID: session.ID, Tool: "edit", Category: "read", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if spoofed.Allowed {
		t.Fatalf("host-supplied read-only claim bypassed canonical tool classification: %#v", spoofed)
	}
	read, err := service.GuardHostTool(HostToolGuardRequest{SessionID: session.ID, Tool: "grep", Category: "read", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !read.Allowed {
		t.Fatalf("read-only inspection was blocked: %#v", read)
	}
	final, err := service.GuardHostCompletion(HostCompletionGuardRequest{SessionID: session.ID, Kind: "final"})
	if err != nil {
		t.Fatal(err)
	}
	if final.Allowed {
		t.Fatalf("premature final completion escaped managed mode: %#v", final)
	}
	status, err := service.GuardHostCompletion(HostCompletionGuardRequest{SessionID: session.ID, Kind: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Allowed {
		t.Fatalf("progress status should be allowed: %#v", status)
	}
}

func TestBeginHostSessionRejectsStrictReuseOfAdvisorySession(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, filepath.Join(workspace, ".takt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateDynamicPlan()
	ctx := context.Background()
	first, err := service.BeginHostSession(ctx, HostBeginRequest{Host: "pi", HostSessionID: "session-reuse", Goal: candidate.Goal, Profile: "code", Enforcement: hostcontrol.EnforcementAdvisory, Candidate: &candidate})
	if err != nil {
		t.Fatal(err)
	}
	if first.Session.Enforcement != hostcontrol.EnforcementAdvisory {
		t.Fatalf("enforcement = %q", first.Session.Enforcement)
	}
	_, err = service.BeginHostSession(ctx, HostBeginRequest{Host: "pi", HostSessionID: "session-reuse", Goal: candidate.Goal, Profile: "code", Enforcement: hostcontrol.EnforcementStrict, Candidate: &candidate, Capabilities: hostcontrol.Capabilities{CommandInterception: true, InputInterception: true, ToolCallBlocking: true, CompletionBlocking: true, SessionRecovery: true}})
	if err == nil || !strings.Contains(err.Error(), "does not satisfy strict") {
		t.Fatalf("expected strict reuse error, got %v", err)
	}
}
