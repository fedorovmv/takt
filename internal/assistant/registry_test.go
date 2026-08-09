package assistant

import (
	"context"
	"testing"

	"takt/internal/spec"
)

type registryStubAdapter struct{}

func (registryStubAdapter) Run(context.Context, Request) (Result, error) { return Result{}, nil }
func (registryStubAdapter) Capabilities() []string                       { return nil }

func registryStubFactory(spec.AssistantSpec) Adapter { return registryStubAdapter{} }

func TestProviderRegistryRejectsDuplicateIDs(t *testing.T) {
	_, err := NewRegistry(
		ProviderRegistration{ID: "x", Factory: registryStubFactory},
		ProviderRegistration{ID: "x", Factory: registryStubFactory},
	)
	if err == nil {
		t.Fatal("expected duplicate provider registration error")
	}
}

func TestProviderRegistryIsReadOnlyAfterConstruction(t *testing.T) {
	registrations := []ProviderRegistration{{
		ID: "b", DisplayName: "Provider B", Stage: ProviderStageExperimental, Factory: registryStubFactory,
	}, {
		ID: "a", DisplayName: "Provider A", Factory: registryStubFactory,
	}}
	registry, err := NewRegistry(registrations...)
	if err != nil {
		t.Fatal(err)
	}

	// Mutating the caller-owned slice after construction must not mutate the registry.
	registrations[0].DisplayName = "changed"
	registrations[0].Factory = nil
	got, ok := registry.Registration("b")
	if !ok || got.DisplayName != "Provider B" || got.Factory == nil {
		t.Fatalf("registry retained caller-owned mutable state: %#v, ok=%v", got, ok)
	}

	// Registrations returns a snapshot, not mutable registry storage.
	snapshot := registry.Registrations()
	if len(snapshot) != 2 || snapshot[0].ID != "a" || snapshot[1].ID != "b" {
		t.Fatalf("registrations must be deterministic and sorted: %#v", snapshot)
	}
	snapshot[0].DisplayName = "changed again"
	got, _ = registry.Registration("a")
	if got.DisplayName != "Provider A" {
		t.Fatalf("returned snapshot mutated registry: %#v", got)
	}
	if got.Stage != ProviderStageExtension {
		t.Fatalf("unspecified bundled provider stage must default to extension, got %q", got.Stage)
	}
}
