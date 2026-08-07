package domainadapter

import (
	"context"
	"fmt"

	"takt/internal/spec"
	sdk "takt/sdk/domainadapter"
)

const ProtocolV1Alpha1 = sdk.ProtocolV1Alpha1

const (
	DomainSCM     = sdk.DomainSCM
	DomainTracker = sdk.DomainTracker
	DomainCI      = sdk.DomainCI
)

type Declaration = sdk.Declaration
type InvokeRequest = sdk.InvokeRequest
type Result = sdk.Result
type ReconcileRequest = sdk.ReconcileRequest
type ReconcileResult = sdk.ReconcileResult

type Adapter interface {
	Describe(context.Context) (Declaration, error)
	Invoke(context.Context, InvokeRequest) (Result, error)
	Reconcile(context.Context, ReconcileRequest) (ReconcileResult, error)
}

type Resolver interface {
	Resolve(string) (Adapter, error)
}

type Factory struct{ Config *spec.Config }

func (f Factory) Resolve(name string) (Adapter, error) {
	if f.Config == nil {
		return nil, fmt.Errorf("domain adapter %q is unavailable without config", name)
	}
	cfg, ok := f.Config.Adapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown domain adapter: %s", name)
	}
	switch cfg.Transport {
	case "process":
		return &Process{Spec: cfg}, nil
	case "mcp":
		return &MCP{Spec: cfg}, nil
	default:
		return nil, fmt.Errorf("domain adapter %q has unsupported transport %q", name, cfg.Transport)
	}
}

func NormalizeDeclaration(value Declaration) Declaration { return sdk.NormalizeDeclaration(value) }
func ValidateDeclaration(value Declaration) error        { return sdk.ValidateDeclaration(value) }
func ValidateOperation(operation string) error           { return sdk.ValidateOperation(operation) }
func HasCapability(value Declaration, operation string) bool {
	for _, item := range value.Capabilities {
		if item == operation {
			return true
		}
	}
	return false
}
func SupportsReconcile(value Declaration, operation string) bool {
	for _, item := range value.Reconcile {
		if item == operation {
			return true
		}
	}
	return false
}
