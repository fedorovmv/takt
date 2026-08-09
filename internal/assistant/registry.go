package assistant

import (
	"context"
	"fmt"
	"sort"

	"takt/internal/spec"
)

// ProviderStage describes the compatibility promise of a provider integration.
// The provider-neutral assistant protocol remains stable regardless of the stage
// of a concrete bundled integration.
type ProviderStage string

const (
	ProviderStageStable       ProviderStage = "stable"
	ProviderStageExtension    ProviderStage = "extension"
	ProviderStageExperimental ProviderStage = "experimental"
)

// ProviderRegistration is immutable metadata plus the factory for one concrete
// assistant provider. Registrations are declared by extension packages and are
// assembled into a Registry only by a composition root.
type ProviderRegistration struct {
	ID           string
	DisplayName  string
	Stage        ProviderStage
	Factory      ProviderFactory
	ProbeVersion func(context.Context, spec.AssistantSpec, string) (string, error)
}

// Registry is an immutable provider lookup built from registrations. It has no
// package-global state: construction copies registrations and later callers can
// only read snapshots.
type Registry struct {
	byID map[string]ProviderRegistration
	ids  []string
}

func NewRegistry(registrations ...ProviderRegistration) (Registry, error) {
	byID := make(map[string]ProviderRegistration, len(registrations))
	ids := make([]string, 0, len(registrations))
	for _, registration := range registrations {
		if registration.ID == "" {
			return Registry{}, fmt.Errorf("assistant provider registration id is required")
		}
		if registration.Factory == nil {
			return Registry{}, fmt.Errorf("assistant provider %q factory is required", registration.ID)
		}
		if _, exists := byID[registration.ID]; exists {
			return Registry{}, fmt.Errorf("assistant provider %q is registered more than once", registration.ID)
		}
		if registration.Stage == "" {
			registration.Stage = ProviderStageExtension
		}
		byID[registration.ID] = registration
		ids = append(ids, registration.ID)
	}
	sort.Strings(ids)
	return Registry{byID: byID, ids: ids}, nil
}

func MustRegistry(registrations ...ProviderRegistration) Registry {
	registry, err := NewRegistry(registrations...)
	if err != nil {
		panic(err)
	}
	return registry
}

func (r Registry) Factory(id string) (ProviderFactory, bool) {
	registration, ok := r.byID[id]
	if !ok {
		return nil, false
	}
	return registration.Factory, true
}

func (r Registry) Registration(id string) (ProviderRegistration, bool) {
	registration, ok := r.byID[id]
	return registration, ok
}

func (r Registry) Registrations() []ProviderRegistration {
	out := make([]ProviderRegistration, 0, len(r.ids))
	for _, id := range r.ids {
		out = append(out, r.byID[id])
	}
	return out
}

func (r Registry) Resolve(id string, assistantSpec spec.AssistantSpec) (Adapter, bool) {
	factory, ok := r.Factory(id)
	if !ok {
		return nil, false
	}
	return factory(assistantSpec), true
}
