package compatibility

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"takt/internal/assistant"
	assistantproviders "takt/internal/extensions/assistants"
	"takt/internal/spec"
)

func TestCurrentMatrixSeparatesSessionAndHostContracts(t *testing.T) {
	matrix := CurrentMatrix()
	if matrix.APIVersion != MatrixVersion || matrix.SchemaSubset.Version == "" {
		t.Fatalf("bad matrix: %+v", matrix)
	}
	var piAssistant, piHost bool
	for _, item := range matrix.Assistants {
		if item.Type == "pi" {
			piAssistant = true
			if item.Support != "supported-alpha" || item.LiveVersionRequired != true {
				t.Fatalf("bad pi assistant policy: %+v", item)
			}
		}
	}
	for _, item := range matrix.Hosts {
		if item.Host == "pi" {
			piHost = true
			if item.Enforcement != "guarded" || item.StrictAllowed || item.LiveVerified {
				t.Fatalf("bad pi host policy: %+v", item)
			}
		}
	}
	if !piAssistant || !piHost {
		t.Fatalf("pi session/host contracts not both represented")
	}
}

func TestCheckWarnsOnLegacyProcessAndDeprecatesV1Alpha1(t *testing.T) {
	cfg := &spec.Config{Assistants: map[string]spec.AssistantSpec{
		"raw": {Type: "process", Argv: []string{"fake"}},
		"old": {Type: "process", Protocol: assistant.ProtocolV1Alpha1, Argv: []string{"fake"}},
		"new": {Type: "process", Protocol: assistant.ProtocolV1Alpha2, Argv: []string{"fake"}, Capabilities: []string{"tool_control"}},
	}}
	report := Check(context.Background(), cfg, CheckOptions{Providers: assistant.MustRegistry(assistantproviders.Registrations()...)})
	if report.Status != "warning" {
		t.Fatalf("status=%s problems=%v warnings=%v", report.Status, report.Problems, report.Warnings)
	}
	byName := map[string]ComponentCheck{}
	for _, item := range report.Assistants {
		byName[item.Name] = item
	}
	if byName["old"].Support != "deprecated" || byName["raw"].Support != "legacy" || byName["new"].Support != "supported-alpha" {
		t.Fatalf("unexpected support: %+v", byName)
	}
	if len(byName["new"].Capabilities) != 1 || byName["new"].Capabilities[0] != "tool_control" {
		t.Fatalf("v1alpha2 capabilities=%v", byName["new"].Capabilities)
	}
}

func TestCheckLivePiProbeReportsVersionWithoutClaimingStrictHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-pi")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 9.8.7; exit 0; fi\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &spec.Config{Assistants: map[string]spec.AssistantSpec{
		"pi": {Type: "pi", Binary: binary},
	}}
	report := Check(context.Background(), cfg, CheckOptions{Workspace: dir, Live: true, Providers: assistant.MustRegistry(assistantproviders.Registrations()...)})
	if report.Status != "warning" {
		t.Fatalf("status=%s problems=%v warnings=%v", report.Status, report.Problems, report.Warnings)
	}
	if len(report.Assistants) != 1 || report.Assistants[0].Version != "9.8.7" {
		t.Fatalf("assistants=%+v", report.Assistants)
	}
	if len(report.Hosts) == 0 || report.Hosts[0].StrictAllowed || report.Hosts[0].LiveVerified {
		t.Fatalf("version probe must not promote guarded host: %+v", report.Hosts)
	}
}
