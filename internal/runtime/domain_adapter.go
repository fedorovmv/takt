package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"takt/internal/domainadapter"
	"takt/internal/execution"
	"takt/internal/spec"
	"takt/internal/store"
)

func (r *Runner) runDomainAdapter(ctx context.Context, state *store.RunState, node spec.Node, local map[string]store.NodeState, feedback, artifacts string) (execResult, error) {
	rendered, err := renderTemplate(node.Adapter.Input, state, local, feedback, artifacts)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render domain adapter input", Err: err}
	}
	raw := json.RawMessage(strings.TrimSpace(rendered))
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if !json.Valid(raw) {
		return execResult{}, &execution.Error{Kind: execution.KindProtocol, Op: "validate domain adapter input", Err: fmt.Errorf("adapter input must be valid JSON")}
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return execResult{}, &execution.Error{Kind: execution.KindProtocol, Op: "validate domain adapter input", Err: fmt.Errorf("adapter input must be a JSON object")}
	}

	resolver := r.Adapters
	if resolver == nil {
		resolver = domainadapter.Factory{Config: r.Config}
	}
	adapter, err := resolver.Resolve(node.Adapter.Name)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve domain adapter", Err: err}
	}
	declaration, err := adapter.Describe(ctx)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindStart, Op: "discover domain adapter capabilities", Err: err}
	}
	if !domainadapter.HasCapability(declaration, node.Adapter.Operation) {
		return execResult{}, &execution.Error{Kind: execution.KindProtocol, Op: "domain adapter preflight", Err: fmt.Errorf("adapter %s/%s does not support operation %s", node.Adapter.Name, declaration.Domain, node.Adapter.Operation)}
	}

	operation := &store.DomainOperationState{
		Adapter: node.Adapter.Name, Domain: declaration.Domain, Operation: node.Adapter.Operation,
		Capabilities:       append([]string(nil), declaration.Capabilities...),
		ReconcileSupported: domainadapter.SupportsReconcile(declaration, node.Adapter.Operation),
	}
	if prior := state.Nodes[node.ID].DomainOperation; prior != nil {
		operation.IdempotencyKey = prior.IdempotencyKey
		operation.Receipt = prior.Receipt
		operation.ReconcileStatus = prior.ReconcileStatus
	}
	if node.SideEffect != nil {
		operation.SideEffectMode = node.SideEffect.Mode
		if operation.IdempotencyKey == "" {
			key := strings.TrimSpace(node.SideEffect.IdempotencyKey)
			if key != "" {
				key, err = renderTemplate(key, state, local, feedback, artifacts)
				if err != nil {
					return execResult{DomainOperation: operation}, &execution.Error{Kind: execution.KindInternal, Op: "render adapter idempotency key", Err: err}
				}
				operation.IdempotencyKey = strings.TrimSpace(key)
			}
			if operation.IdempotencyKey == "" {
				operation.IdempotencyKey = state.ID + ":" + node.ID
			}
		}
		if operation.SideEffectMode == "reconcile" && !operation.ReconcileSupported {
			return execResult{DomainOperation: operation}, &execution.Error{Kind: execution.KindProtocol, Op: "domain adapter preflight", Err: fmt.Errorf("operation %s requires reconciliation but adapter does not declare it", node.Adapter.Operation)}
		}
	}

	request := domainadapter.InvokeRequest{RunID: state.ID, NodeID: node.ID, Attempt: state.Nodes[node.ID].Attempts, Workspace: state.Workspace, Domain: declaration.Domain, Operation: node.Adapter.Operation, Input: raw, IdempotencyKey: operation.IdempotencyKey, SideEffectMode: operation.SideEffectMode}

	// A previous attempt with unknown side-effect state must be reconciled
	// before another invocation. This makes even an explicitly configured retry
	// safe instead of blindly repeating the mutation.
	if operation.SideEffectMode == "reconcile" && operation.ReconcileStatus == "unknown" {
		result, done, err := r.reconcileDomainOperation(ctx, adapter, request, operation)
		if done || err != nil {
			return result, err
		}
	}

	return r.invokeDomainOperation(ctx, adapter, request, operation)
}

func (r *Runner) invokeDomainOperation(ctx context.Context, adapter domainadapter.Adapter, request domainadapter.InvokeRequest, operation *store.DomainOperationState) (execResult, error) {
	value, err := adapter.Invoke(ctx, request)
	if err != nil {
		if operation.SideEffectMode == "reconcile" {
			operation.ReconcileStatus = "unknown"
			result, done, recErr := r.reconcileDomainOperation(ctx, adapter, request, operation)
			if done || recErr != nil {
				return result, recErr
			}
		}
		return execResult{DomainOperation: operation}, &execution.Error{Kind: execution.KindStart, Op: "invoke domain adapter", Err: err}
	}
	if value.Receipt != "" {
		operation.Receipt = value.Receipt
	}
	switch value.Status {
	case "completed":
		operation.ReconcileStatus = "applied"
		return domainExecResult(value.Output, operation), nil
	case "failed":
		return domainExecResult(value.Output, operation), &execution.Error{Kind: execution.KindExit, ExitCode: 1, Op: "domain adapter operation", Err: fmt.Errorf("%s: %s", value.ErrorCode, value.Error)}
	case "unknown":
		if operation.SideEffectMode != "reconcile" {
			return domainExecResult(value.Output, operation), &execution.Error{Kind: execution.KindExternalUnknown, Op: "domain adapter operation", Err: fmt.Errorf("adapter returned unknown state for a non-reconciled operation")}
		}
		operation.ReconcileStatus = "unknown"
		result, _, recErr := r.reconcileDomainOperation(ctx, adapter, request, operation)
		return result, recErr
	default:
		return execResult{DomainOperation: operation}, &execution.Error{Kind: execution.KindProtocol, Op: "domain adapter operation", Err: fmt.Errorf("unsupported result status %q", value.Status)}
	}
}

func (r *Runner) reconcileDomainOperation(ctx context.Context, adapter domainadapter.Adapter, request domainadapter.InvokeRequest, operation *store.DomainOperationState) (execResult, bool, error) {
	value, err := adapter.Reconcile(ctx, domainadapter.ReconcileRequest{RunID: request.RunID, NodeID: request.NodeID, Workspace: request.Workspace, Domain: request.Domain, Operation: request.Operation, Input: request.Input, IdempotencyKey: request.IdempotencyKey, Receipt: operation.Receipt})
	if err != nil {
		operation.ReconcileStatus = "unknown"
		return execResult{DomainOperation: operation}, true, &execution.Error{Kind: execution.KindExternalUnknown, Op: "reconcile domain adapter side effect", Err: err}
	}
	operation.ReconcileStatus = value.Outcome
	if value.Receipt != "" {
		operation.Receipt = value.Receipt
	}
	switch value.Outcome {
	case "applied":
		return domainExecResult(value.Output, operation), true, nil
	case "not_applied":
		// The external fact check proved no mutation happened. One fresh invoke
		// with the same idempotency key is therefore safe.
		result, invokeErr := adapter.Invoke(ctx, request)
		if invokeErr != nil {
			operation.ReconcileStatus = "unknown"
			return execResult{DomainOperation: operation}, true, &execution.Error{Kind: execution.KindExternalUnknown, Op: "invoke domain adapter after not_applied reconciliation", Err: invokeErr}
		}
		if result.Receipt != "" {
			operation.Receipt = result.Receipt
		}
		if result.Status == "completed" {
			operation.ReconcileStatus = "applied"
			return domainExecResult(result.Output, operation), true, nil
		}
		if result.Status == "failed" {
			return domainExecResult(result.Output, operation), true, &execution.Error{Kind: execution.KindExit, ExitCode: 1, Op: "domain adapter operation", Err: fmt.Errorf("%s: %s", result.ErrorCode, result.Error)}
		}
		operation.ReconcileStatus = "unknown"
		return domainExecResult(result.Output, operation), true, &execution.Error{Kind: execution.KindExternalUnknown, Op: "domain adapter operation", Err: fmt.Errorf("side-effect state remains unknown after safe retry")}
	case "unknown":
		return domainExecResult(value.Output, operation), true, &execution.Error{Kind: execution.KindExternalUnknown, Op: "domain adapter operation", Err: fmt.Errorf("external side-effect state is unknown; retry is blocked until reconciliation succeeds")}
	default:
		return execResult{DomainOperation: operation}, true, &execution.Error{Kind: execution.KindProtocol, Op: "reconcile domain adapter side effect", Err: fmt.Errorf("unsupported reconcile outcome %q", value.Outcome)}
	}
}

func domainExecResult(raw json.RawMessage, operation *store.DomainOperationState) execResult {
	raw = bytes.TrimSpace(raw)
	output := ""
	if len(raw) > 0 && string(raw) != "null" {
		output = string(raw)
	}
	return execResult{Output: output, Stdout: output, ExitCode: 0, DomainOperation: operation}
}
