# Provider Availability Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Classify transient provider outages explicitly, retry them durably up to three provider executions without consuming workflow attempts, and keep them out of evaluation quality denominators.

**Architecture:** Pi, OpenCode, and the strict process-assistant decoder produce a provider-neutral `execution.KindProviderUnavailable` error only from explicit transient evidence. The existing scheduler persists provider-scoped retry state and resumes the same assistant session; workflow attempt counters and `attempts_to_valid` remain unchanged. Evaluation preserves raw evidence and usage but reports `infrastructure_error` separately from product accept/reject outcomes.

**Tech Stack:** Go, existing `internal/execution`, `internal/assistant`, Pi RPC adapter, OpenCode NDJSON adapter, file-backed durable Store, JSON schemas, Go unit/contract tests.

**Spec:** `docs/superpowers/specs/2026-08-13-provider-availability-recovery-design.md`; audit: `docs/superpowers/provider-availability-design-unknowns.md`.

## Global Constraints

- Do not add a second executor, provider plugin framework, or transport-specific Run semantics.
- Use the existing `store.RetryState`/scheduler persistence; no untracked sleep loop may own retry timing.
- Provider retry budget is exactly three provider executions total: initial execution plus two retries.
- In this contract, one “provider execution” is one Takt call to `assistant.SessionAdapter.Run`. Pi/OpenCode-owned low-level retries inside that call are adapter telemetry, not Takt durable provider attempts. Takt does not claim a bound on HTTP requests hidden inside those CLIs.
- The three-call bound applies to durably observed adapter results. A host-process crash during an in-flight call may replay the same unacknowledged provider ordinal after recovery; current local runtime is at-least-once across that crash window, not exactly-once.
- Provider retry delays are 2 seconds and 4 seconds by default; a parsed `Retry-After` delay is capped at 60 seconds.
- Provider retry uses the same Session ID and does not increment `NodeState.Attempts`, workflow `attempts.max`, or `attempts_to_valid`.
- If a transient failure result has an empty Session ID, runtime fails it as `protocol` and does not perform a fresh retry.
- Parent context cancellation/deadline wins over provider classification.
- `allow_failure` never accepts `provider_unavailable`.
- Domain adapter side effects and `side_effect: reconcile` semantics are unchanged; provider retry applies only to assistant model transport.
- Raw adapter output and provider messages pass through the existing redactor before persistence.
- Existing errors (`exit`, `start`, `protocol`, `internal`, `timed_out`, `cancelled`, `external_state_unknown`) retain their current semantics.
- Do not modify the user workflow syntax for configuring provider retry in this change.
- Never stage or modify the user’s ignored `.takt/` directory.

## File Map

- `internal/execution/error.go`, `internal/execution/provider.go`: add the provider-unavailable execution kind, retry delay, and the shared explicit-evidence classifier.
- `internal/assistant/protocol.go`, `internal/assistant/process.go`: decode strict process envelopes with `failure_kind=provider_unavailable`.
- `internal/extensions/assistants/pi/pi.go`: classify terminal Pi provider errors and preserve retry-after evidence.
- `internal/extensions/assistants/opencode/opencode.go`: classify structured OpenCode error events and preserve status/retry-after evidence.
- `internal/testsupport/cmd/takt-fake-pi/main.go`, `internal/testsupport/cmd/takt-fake-opencode/main.go`: deterministic outage fixtures.
- `internal/store/store.go`, `schemas/run-state.schema.json`: durable provider retry scope and per-execution provider-attempt ordinal.
- `internal/runtime/assistant_node.go`, `internal/runtime/attempt.go`, `internal/runtime/runner.go`, `internal/runtime/parallel.go`: provider retry state machine, same-session resolution, and execution attribution.
- `internal/application/operations.go`, `internal/application/operations_test.go`: preserve provider retry markers across backoff and in-flight process recovery.
- `internal/bootstrap/evaluation.go`, `internal/tooling/evaluation/runtime_metrics.go`: trace provider retry events and count their durable metrics.
- `internal/tooling/evaluation/evaluation.go`, `flow.go`, `flow_report.go`, tests: infrastructure outcome and denominator changes.
- `schemas/evaluation-report.schema.json`: report fields and `infrastructure_error` outcome.
- `docs/09-runtime-semantics.md`, `docs/10-assistant-adapter-spec.md`, `docs/03-specification.md`, `docs/05-implementation-status.md`, `CHANGELOG.md`: normative documentation.

### Task 1: Add the provider-unavailable error contract

**Files:**
- Modify: `internal/execution/error.go`
- Create: `internal/execution/provider.go`
- Test: `internal/execution/error_test.go`
- Create: `internal/execution/provider_test.go`

**Interfaces:**
- Produce `execution.KindProviderUnavailable Kind = "provider_unavailable"`.
- Extend `execution.Error` only with `RetryAfter time.Duration`; zero means “not supplied”. Provider ordinals are runtime state and must not be stored on adapter errors.
- Add `execution.ProviderUnavailable(err error) bool` using `KindOf`, without string matching.
- Add `execution.IsTransientProviderFailure(status int, message string) bool`. It returns true only for status `429|502|503|504` or the normalized phrases listed below. Adapters decide which structured fields are trusted inputs; this helper never scans agent output itself.

- [ ] **Step 1: Write failing tests.** Add table cases proving `KindOf(&execution.Error{Kind: KindProviderUnavailable})` returns the new kind, `ProviderUnavailable` is true only for that kind, `RetryAfter` is readable through `errors.As` after wrapping, and existing exit/protocol errors return false. Test every accepted status/phrase and negative cases `400`, `401`, `404`, `context length`, `tool failed`, and arbitrary assistant prose.

```go
func TestProviderUnavailableClassification(t *testing.T) {
	err := &Error{Kind: KindProviderUnavailable, RetryAfter: 3 * time.Second}
	if KindOf(err) != KindProviderUnavailable || !ProviderUnavailable(err) {
		t.Fatalf("classification = %s", KindOf(err))
	}
	if ProviderUnavailable(&Error{Kind: KindExit}) {
		t.Fatal("ordinary exit classified as provider outage")
	}
}
```

The phrase table in `provider_test.go` must contain exactly:

```go
[]string{
	"rate limit", "rate_limit", "too many requests", "overloaded",
	"service unavailable", "temporarily unavailable", "temporary unavailable",
	"connection reset", "connection refused", "econnreset", "etimedout",
	"enotfound", "no such host", "temporary failure in name resolution",
	"fetch failed", "socket hang up",
}
```

- [ ] **Step 2: Run the focused tests and verify they fail.**

Run: `go test ./internal/execution -run 'ProviderUnavailable|TransientProvider' -count=1`

Expected: compile failure because the new kind/helper does not exist.

- [ ] **Step 3: Implement the minimal contract.** Add the kind/field/helper and a lowercase `strings.Contains` table in `provider.go`; do not add regexes or retry logic. `Error.Unwrap` remains unchanged because `errors.As` already preserves the outer `*execution.Error`.

- [ ] **Step 4: Run the focused tests.**

Run: `go test ./internal/execution -run 'ProviderUnavailable|TransientProvider' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/execution/error.go internal/execution/error_test.go internal/execution/provider.go internal/execution/provider_test.go
git commit -m "feat: classify provider unavailable failures"
```

### Task 2: Extend the assistant result and strict process protocol

**Files:**
- Modify: `internal/assistant/protocol.go`
- Modify: `internal/assistant/process.go`
- Modify: `schemas/assistant-protocol.schema.json`
- Test: `internal/assistant/contract_test.go`
- Test: `internal/assistant/process_test.go`
- Create: `internal/assistant/testdata/v1alpha2/provider-unavailable.json`

**Interfaces:**
- Do not change `assistant.Result`; the returned `*execution.Error` is the single failure-metadata carrier.
- Add this field to `ProtocolResult` and allow `failure_kind=provider_unavailable` only in v1alpha2 failed results:

```go
RetryAfterMS *int64 `json:"retry_after_ms,omitempty"`
```
- The process adapter must map only explicit `failure_kind=provider_unavailable` to `execution.KindProviderUnavailable`; it must reject unknown failure kinds as protocol errors.
- `retry_after_ms` is valid only with `failure_kind=provider_unavailable`, must be non-negative, and maps to `execution.Error.RetryAfter`. V1alpha1 results containing either new value are protocol errors.
- A provider-unavailable result must return a non-empty `session.id`; otherwise it is a protocol error because same-session retry cannot be satisfied.

- [ ] **Step 1: Add the failing fixture and tests.** Fixture stdout contains one strict v1alpha2 result with `status=failed`, `failure_kind=provider_unavailable`, `retry_after_ms=2500`, `session.id=session-provider-1`, `exit_code=1`; assert error kind, `errors.As(...).RetryAfter == 2500*time.Millisecond`, result session, stdout/stderr, and usage preservation. Add table cases for unknown failure kind, negative retry delay, retry delay on `exit`, missing session, and v1alpha1 use of the new fields; each must be `KindProtocol`.

- [ ] **Step 2: Run the tests before implementation.**

Run: `go test ./internal/assistant -run 'ProviderUnavailable|FailureKind' -count=1`

Expected: FAIL because the envelope field and mapping are absent.

- [ ] **Step 3: Implement strict decode and mapping.** Update the protocol struct/schema and `validateProtocolResult`; in `finishV1Alpha2`, return `execution.Error{Kind: execution.KindProviderUnavailable, RetryAfter: time.Duration(*RetryAfterMS)*time.Millisecond}`. Keep OS exit/envelope exit-code equality checks unchanged.

- [ ] **Step 4: Run focused assistant tests.**

Run: `go test ./internal/assistant -run 'ProviderUnavailable|FailureKind|Process' -count=1`

Expected: PASS, with existing assistant contract tests still green.

- [ ] **Step 5: Commit.**

```bash
git add internal/assistant schemas/assistant-protocol.schema.json
git commit -m "feat: expose provider failure in assistant protocol"
```

### Task 3: Classify Pi and OpenCode provider failures

**Files:**
- Modify: `internal/extensions/assistants/pi/pi.go`
- Modify: `internal/extensions/assistants/opencode/opencode.go`
- Modify: `internal/testsupport/cmd/takt-fake-pi/main.go`
- Modify: `internal/testsupport/cmd/takt-fake-opencode/main.go`
- Test: `internal/extensions/assistants/pi/pi_contract_test.go`
- Test: `internal/extensions/assistants/opencode/opencode_contract_test.go`

**Interfaces:**
- Use `execution.IsTransientProviderFailure` from Task 1 with this exact evidence set:
  - numeric status `429`, `502`, `503`, `504`;
  - error names/messages containing one of the exact normalized phrases in Task 1.
- Do not classify arbitrary assistant text/output. Only Pi `errorMessage`/terminal error records and OpenCode JSON `error` objects plus transport stderr are candidates.
- OpenCode parser must inspect integer or JSON-number `error.data.statusCode`, `error.statusCode`, `error.data.retryAfterMs`, and `error.retryAfterMs`. A negative retry-after is ignored as absent; it never changes an otherwise non-transient failure into a transient one.
- Pi parser must retain `auto_retry_end.finalError` and the terminal assistant `errorMessage`; if no numeric provider retry delay exists, return zero `RetryAfter`. `auto_retry_start.delayMs` is Pi's internal delay and must not become Takt `RetryAfter`.
- Adapters return the new kind only after their own internal retry has finished; successful internal retry remains success.
- Each Pi/OpenCode `Run` call counts as one Takt provider execution. Pi `AutomaticRetries` and `LowLevelRuns` remain in `Result.Structured` and do not alter the Takt ordinal or three-call budget.

- [ ] **Step 1: Add fake outage cases and failing tests.** Add Pi cases `provider-503`, `provider-429`, and `provider-connection-reset`; each must emit a stable non-empty session, terminal error evidence, and `agent_settled`. Add OpenCode JSON errors for status 503/429 and a connection error with a stable `sessionID`. Assert provider-unavailable classification and retained result/session/usage. Assert ordinary `agent-failure`, an unrecognized OpenCode `error-zero-exit`, malformed stream, timeout, cancellation, and HTTP 401 retain their old kinds.

- [ ] **Step 2: Run adapter tests before implementation.**

Run: `go test ./internal/extensions/assistants/pi ./internal/extensions/assistants/opencode -run 'Provider|Retry|Failure|Timeout|Cancel' -count=1`

Expected: FAIL for the new outage cases.

- [ ] **Step 3: Implement Pi classification.** Change `piAgentFailure` to return the terminal message plus transient classification derived only from the last assistant error and retained `auto_retry_end.finalError`. Return `execution.KindProviderUnavailable` only after `agent_settled`; preserve `result.Output`, `Stdout`, `Stderr`, session, structured retry counters, and usage. Do not add an adapter retry loop.

- [ ] **Step 4: Implement OpenCode classification.** Replace `decodedOpenCode.Errors []string` with private structured error evidence `{Message, Status, RetryAfter}` while keeping the messages used for output. Classify only decoded JSON `error` objects and transport stderr tied to a non-zero process/error event; preserve the raw event stream and diagnostic text exactly as today. Do not parse the TUI or add an adapter retry loop.

- [ ] **Step 5: Run adapter tests.**

Run: `go test ./internal/extensions/assistants/pi ./internal/extensions/assistants/opencode -count=1`

Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add internal/extensions/assistants internal/testsupport/cmd/takt-fake-pi internal/testsupport/cmd/takt-fake-opencode
git commit -m "feat: classify Pi and OpenCode provider outages"
```

### Task 4: Add durable provider retry state and execution attribution

**Files:**
- Modify: `internal/store/store.go`
- Modify: `schemas/run-state.schema.json`
- Modify: `internal/runtime/runner.go`
- Modify: `internal/runtime/attempt.go`
- Modify: `internal/runtime/assistant_node.go`
- Modify: `internal/runtime/parallel.go`
- Test: `internal/runtime/runner_test.go`

**Interfaces:**
- Extend `store.RetryState` with `Scope string` (`workflow` default for old state, `provider` for new state) and `ProviderAttempt int`.
- Extend `store.ExecutionState` with `ProviderAttempt int`: ordinal `1..3` inside its workflow attempt; non-assistant executions use zero/omitted.
- Extend `store.NodeState` with `ProviderAttempts int`: total durably recorded `SessionAdapter.Run` results across all workflow attempts for that node. An ordinary successful assistant node therefore has value 1; an initial outage plus two durable results has value 3. The per-execution ordinal resets to 1 when workflow retry starts a new workflow attempt.
- Add schema properties with the same names and constraints; old state without `scope` means workflow scope.
- Add `providerRetryMax = 3`, `providerRetryDelay(providerAttempt int, retryAfter time.Duration) time.Duration`, and `isProviderRetry(err error) bool` in runtime.
- `providerRetryDelay` receives the just-failed ordinal: ordinal 1 defaults to 2s, ordinal 2 defaults to 4s. If `RetryAfter > 0`, use `min(RetryAfter, 60s)` instead of the default; ordinal 3 is exhausted and is never scheduled.
- Implement two explicit paths in `runNode`; do not re-enter `runAttempt` for a provider retry:
  1. The first workflow attempt runs existing before-hooks once and calls the assistant once. Increment `NodeState.ProviderAttempts` for that call. On `provider_unavailable`, record execution ordinal 1, set `NodeState.Status=NodePending`, clear `state.CurrentNode`/remove the ID from `CurrentNodes`, persist `RetryState{Scope:"provider", ProviderAttempt:2}`, and return to the scheduler.
  2. At the top of `runNode`, before the `ns.Attempts < max` loop, detect `Retry.Scope=="provider"`, then call a provider-aware `awaitRetry`. Unlike workflow scope, provider readiness commits `provider.retry.ready` but retains the marker during the adapter call, so session and ordinal remain available. A new `runProviderExecution` calls `execute` and existing result-persistence helpers only. It must not run before-hooks, increment `Attempts`, clear feedback, or apply `allow_failure`. On success, clear the marker and run the existing successful post-execution path (after/before-complete hooks, artifact capture, `node.completed`) once. On another provider error, replace it with the next marker and return to the scheduler. On ordinal 3 failure, commit `provider.retry.exhausted`, clear the marker, then call `finishNodeExecutionError` with `provider_unavailable` and no retry hook. If a retry call returns a non-provider error, clear provider state and pass it to existing `handleFailedAttempt`; only explicit workflow retry policy/hooks may then retry it. Add `KindProviderUnavailable` to the `NodeFailed` mapping in both `recordExecution` and `finishNodeExecutionError`.
- `runProviderExecution` must preserve the previous non-empty `SessionID`; `resolveAssistantNode` in `assistant_node.go` must select `session_mode=resume` from the provider marker/session even though `NodeState.Attempts` remains 1. A returned session mismatch becomes `KindProtocol` and is never retried.
- Add `ProviderAttempt int` to private `execResult`. `executeAssistantAction` sets it to 1 when no provider marker exists, otherwise to `Retry.ProviderAttempt`. `recordExecution` copies it to `ExecutionState.ProviderAttempt` and increments `NodeState.ProviderAttempts` only when it is positive. Command/bash/script/domain executions keep provider ordinal zero.
- Parallel waves are explicit: change `parallelEligible` to accept the current `NodeState`; nodes with `Retry.Scope=="provider"` or `ProviderAttempts>0` are never admitted. If a first-call parallel result is provider-unavailable, `runParallelWave` records it, sets the node pending, increments/schedules provider state, and does not call `finishNodeExecutionError`; the next scheduler cycle executes that node through sequential `runProviderExecution`. Other wave nodes retain their existing completion/failure behavior.
- Provider scheduling requires `result.SessionID != ""`; otherwise convert the failure to `KindProtocol` before recording/scheduling. This applies to Pi, OpenCode, process adapters, and test adapters uniformly.

- [ ] **Step 1: Add failing state/schema tests.** Add tests that marshal/unmarshal `RetryState{Scope:"provider", ProviderAttempt:2}`, `NodeState.ProviderAttempts`, and `ExecutionState.ProviderAttempt`; assert the runtime scope helper interprets old JSON with empty/absent `scope` as workflow scope. Schema keeps `scope` optional with enum `workflow|provider`, and requires `provider_attempt>=2` only when scope is provider.

- [ ] **Step 2: Add a failing runtime fixture.** Create a fake assistant adapter that returns provider-unavailable twice with a stable Session ID and then success; assert request 2 and 3 have `SessionMode=resume` and the same ID. Workflow has `attempts.max:1` and no `retry_on`. Assert final Run completed, `NodeState.Attempts==1`, `NodeState.ProviderAttempts==3`, three execution records with `Attempt==1` and provider ordinals 1/2/3, and accumulated usage. Add a second fixture with two independent assistant nodes in one parallel wave; make one fail provider-first and assert the other completes while the failed node later resumes sequentially.

- [ ] **Step 3: Run the tests before implementation.**

Run: `go test ./internal/runtime -run 'ProviderRetry|ProviderAttempt|RetryState' -count=1`

Expected: FAIL because provider scope and runtime disposition do not exist.

- [ ] **Step 4: Implement the durable state machine.** Reuse `awaitRetry` for the persisted deadline, but branch on `Retry.Scope`; workflow scope keeps its current clear-on-ready behavior, while provider scope retains its marker until the call result is durably handled. Commit one atomic scheduled state/event with `NodePending`, cleared current-node ownership, session, marker, and deadline before waiting. At readiness set `NodeRunning` and `CurrentNode` (or current-wave ownership) in the same `provider.retry.ready` commit. Preserve marker/session/deadline across pause, cancellation, daemon restart, and process recovery. Ensure `pauseRequested`, cancellation, and context deadline are checked during provider backoff.

- [ ] **Step 5: Add failure/guard tests.** Assert three failed provider executions produce Run `failed`, node `failed`, error code `provider_unavailable`, `Attempts==1`, `ProviderAttempts==3`; `allow_failure:true` does not complete it; ordinary exit with `allow_failure` remains accepted; workflow `retry_on` still controls only ordinary execution kinds. Add retry-delay table cases for `(1,0)=2s`, `(2,0)=4s`, `(1,1s)=1s`, `(1,30s)=30s`, `(2,90s)=60s`. Add pause/cancel/context-deadline cases showing no adapter call occurs before `NotBefore` and cancellation wins.

- [ ] **Step 6: Run runtime tests.**

Run: `go test ./internal/runtime -run 'ProviderRetry|ProviderAttempt|RetryState|AllowFailure|RetryBackoff' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit.**

```bash
git add internal/store/store.go schemas/run-state.schema.json internal/runtime
git commit -m "feat: persist provider retries separately from workflow attempts"
```

### Task 5: Preserve provider retry through process recovery

**Files:**
- Modify: `internal/application/operations.go`
- Test: `internal/application/operations_test.go`

**Interfaces:**
- During backoff the provider marker is pending with no current-node ownership. During the retry call the node is running/current and retains the marker.
- `recoverRunState` branches only for a current running node with `Retry.Scope=="provider"`: set it pending, preserve `Attempts`, `ProviderAttempts`, `SessionID`, `Retry.NotBefore`, and `Retry.ProviderAttempt`, and do not append `worker_lost`. Resume may replay that unacknowledged ordinal.
- Active running nodes without the marker retain existing `worker_lost` recovery.

- [ ] **Step 1: Add two recovery regressions.** First persist the valid backoff boundary (`CurrentNode:""`, node pending, provider marker/deadline) and prove recovery waits then resumes the same session. Second persist a crash during the retry call (`CurrentNode:"implement"`, node running with the same marker) and prove recovery normalizes it to pending without `worker_lost`, preserves ordinal/deadline/session, and keeps workflow attempts at 1.

- [ ] **Step 2: Run the recovery test against the Task 4 implementation.**

Run: `go test ./internal/application -run 'ProviderRetryRecovery' -count=1`

Expected: FAIL for the in-flight case because existing recovery appends `worker_lost` and decrements workflow attempts.

- [ ] **Step 3: Implement the narrow recovery branch.** Add it before the generic `worker_lost` branch and match only `node.Retry != nil && node.Retry.Scope == "provider"`; do not infer provider recovery from counters or error strings.

- [ ] **Step 4: Run recovery tests.**

Run: `go test ./internal/application -run 'ProviderRetryRecovery|RecoverInterruptedRun' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/application/operations.go internal/application/operations_test.go
git commit -m "fix: preserve provider retry during recovery"
```

### Task 6: Add provider retry trace and durable event coverage

**Files:**
- Modify: `internal/runtime/runner.go`
- Modify: `internal/bootstrap/evaluation.go`
- Modify: `internal/tooling/evaluation/runtime_metrics.go`
- Test: `internal/runtime/runner_test.go`
- Test: `internal/bootstrap/evaluation_test.go`
- Test: `internal/tooling/evaluation/evaluation_test.go`

**Interfaces:**
- Emit these exact event types and data:
  - `provider.retry.scheduled`: `scope=provider`, `provider_attempt` equal to the just-failed ordinal, `max_provider_attempts=3`, `delay`, `not_before`, `kind`, `fingerprint`;
  - `provider.retry.ready`: `scope=provider`, `provider_attempt` equal to the ordinal about to run, `max_provider_attempts=3`;
  - `provider.retry.exhausted`: `scope=provider`, `provider_attempts=3`, `max_provider_attempts=3`, `kind=provider_unavailable`, `fingerprint`.
- `traceEvaluationEvent` must print `run=<run id>` (the caller supplies it), `node=<node id>`, and `provider_attempt`, `max_provider_attempts`, `delay`, `not_before`, `kind`, and `fingerprint` when present. Example: `provider.retry.scheduled run=run-1 node=implement provider_attempt=1/3 delay=2s kind=provider_unavailable`.
- `traceFlow` must emit human-readable lines exactly matching the design examples, with run/node IDs and attempt counts.
- `applyRuntimeMetricsFromEvents` must count `provider.retry.scheduled` in the existing retry-scheduled total and retain its fingerprint; it must not add provider retries to workflow `Attempts` or `attempts_to_valid`.

- [ ] **Step 1: Add failing event/trace tests.** Run the fake provider-retry workflow with a trace callback; assert ordered event types and trace substrings `provider.retry.scheduled`, `provider.retry.ready`, and `provider.retry.exhausted`.

- [ ] **Step 2: Run before implementation.**

Run: `go test ./internal/runtime ./internal/bootstrap ./internal/tooling/evaluation -run 'ProviderRetry.*Trace|ProviderRetry.*Event|ProviderRetry.*Metric' -count=1`

Expected: FAIL because provider events are not emitted.

- [ ] **Step 3: Implement event data and trace rendering.** Pass the root Run ID into `traceEvaluationEvent`; render the exact fields above. Do not expose provider response bodies beyond already redacted diagnostic fields. Update `runtime_metrics.go` so provider events contribute retry diagnostics without changing workflow-attempt metrics.

- [ ] **Step 4: Run trace tests.**

Run: `go test ./internal/runtime ./internal/bootstrap ./internal/tooling/evaluation -run 'ProviderRetry.*Trace|ProviderRetry.*Event|ProviderRetry.*Metric' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/runtime/runner.go internal/bootstrap/evaluation.go internal/tooling/evaluation/runtime_metrics.go internal/runtime/runner_test.go internal/bootstrap/evaluation_test.go internal/tooling/evaluation/evaluation_test.go
git commit -m "feat: trace durable provider retry lifecycle"
```

### Task 7: Exclude provider outages from evaluation quality denominators

**Files:**
- Modify: `internal/tooling/evaluation/evaluation.go`
- Modify: `internal/tooling/evaluation/flow.go`
- Modify: `internal/tooling/evaluation/flow_report.go`
- Modify: `schemas/evaluation-report.schema.json`
- Test: `internal/tooling/evaluation/flow_report_test.go`
- Test: `internal/tooling/evaluation/evaluation_test.go`
- Test: `internal/tooling/evaluation/flow_test.go`

**Interfaces:**
- Add `ProviderAttempts int` to `RunRecord` and `NodeRecord`, and `ProviderAttempt int` to `ExecutionRecord`; preserve `Attempts` as workflow attempts. `RunRecord.ProviderAttempts` is the sum of node provider calls, `NodeRecord.ProviderAttempts` is that node's total, and `ExecutionRecord.ProviderAttempt` is the ordinal within its workflow attempt.
- Add `InfrastructureErrors int` to `Summary` and `FlowSummary`; JSON key is `infrastructure_errors`.
- Extend `RunRecord.Outcome` enum with `infrastructure_error`.
- Add helper `isProviderUnavailableRecord(record RunRecord) bool` that checks only structured fields: root `ErrorCode`; each node's `ErrorCode`, `Diagnostic.Code`, or `Diagnostic.Kind`; and each execution's matching fields. Never classify from human message text.
- `ClassifyFlowRecord` checks `isProviderUnavailableRecord` before the existing validation guard, sets `Outcome="infrastructure_error"`, `RunPassed=nil`, and leaves `QualityExpected=false`/`Quality=nil` even when validator `valid` is true or false.
- `addFlowSummary` increments `InfrastructureErrors` and returns before `EvaluatedRuns`, `TrueReject`, `FalseReject`, and `ValidationErrors`.
- `addSummary` retains total/duration/usage/diagnostics and increments infrastructure count, but skips `QualityRuns`, `Valid`, `Invalid`, stability, and score aggregation for provider-unavailable records. Do not count their `Quality.Score` or `TimeToValidMS` in `finishFlowReport`.
- `finishFlowReport` calculates quality rates only from `EvaluatedRuns`; completion rate still uses all scheduled records.

- [ ] **Step 1: Write failing aggregation tests.** Add one failed Run with `ErrorCode: "provider_unavailable"` and a completed `valid:false` validator envelope. Assert outcome infrastructure_error, `Flow.EvaluatedRuns == 0`, `Flow.InfrastructureErrors == 1`, `Summary.QualityRuns == 0`, `Summary.Invalid == 0`, and retained `InputTokens`/`OutputTokens`.

- [ ] **Step 2: Run tests before implementation.**

Run: `go test ./internal/tooling/evaluation -run 'ProviderUnavailable|Infrastructure' -count=1`

Expected: FAIL because current code classifies the record as true reject and counts it in quality.

- [ ] **Step 3: Implement classification and aggregation.** Keep validation result attached to the run for audit; do not relabel the durable Store Run status.

- [ ] **Step 4: Update report schema and run schema tests.** Add `infrastructure_errors` as a required non-negative integer in both `summary` and `flowSummary`; add `infrastructure_error` to the run outcome enum. Keep `quality_runs`/`evaluated_runs` semantics unchanged and do not make `provider_attempts` required for old reports.

- [ ] **Step 5: Run evaluation tests.**

Run: `go test ./internal/tooling/evaluation -count=1`

Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add internal/tooling/evaluation schemas/evaluation-report.schema.json
git commit -m "feat: exclude provider outages from eval quality rates"
```

### Task 8: Add real flow regression and continue-after-outage coverage

**Files:**
- Modify: `internal/testsupport/cmd/takt-fake-pi/main.go`
- Modify: `internal/bootstrap/evaluation_test.go`
- Modify: `internal/tooling/evaluation/flow_test.go`

**Interfaces:**
- Extend fake Pi with `--fake-case provider-sequence --fake-state-prefix <path> --fake-failures <n>`. For ordinal `i=1..n`, attempt `os.OpenFile(prefix+"."+strconv.Itoa(i), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)` in order; the first successful create selects that failing invocation. If every sentinel exists, emit the existing success sequence. This uses atomic stdlib file creation, needs no lock/dependency, and is isolated by `t.TempDir()`.
- Add `--fake-case provider-by-prompt`: emit terminal 503 evidence only when the received prompt contains the exact test marker `TAKT_FAKE_PROVIDER_EXHAUSTED`; otherwise emit the existing success sequence. Resume must echo supplied `--session` exactly.
- Add `provider-exhausted` as a simple always-503 fake case for adapter/runtime exhaustion tests.
- The flow test must run two cases in one suite: the outage case is reported as infrastructure_error and the following case still executes.

- [ ] **Step 1: Add failing flow tests.** Create two generated test-owned suites and simple `$ARGUMENTS` prompt workflows. Suite A has one case and config args `provider-sequence`, temp prefix, failures `2`; assert the same session is used across three adapter calls and the case reaches its validator normally. Suite B has two cases and config arg `provider-by-prompt`; first input contains `TAKT_FAKE_PROVIDER_EXHAUSTED`, second does not. Assert both records exist, first is `outcome=infrastructure_error`, second is evaluated normally, `summary.flow.infrastructure_errors==1`, `evaluated_runs==1`, `quality_runs==1`, and quality rates use denominator one.

- [ ] **Step 2: Run before implementation.**

Run: `go test ./internal/bootstrap ./internal/tooling/evaluation -run 'Flow.*ProviderUnavailable|ProviderUnavailable.*Suite' -count=1`

Expected: FAIL because the fake adapter lacks the deterministic outage mode and flow classification is product rejection.

- [ ] **Step 3: Implement only test-fixture behavior and wiring.** Use the fake Pi's existing `--fake-case` argument in a test-owned generated config and an in-test two-case suite; do not modify `examples/` or production workflow/command files, and do not add production-only switches to runtime.

- [ ] **Step 4: Run flow tests.**

Run: `go test ./internal/bootstrap ./internal/tooling/evaluation -run 'Flow.*ProviderUnavailable|ProviderUnavailable.*Suite' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/testsupport/cmd/takt-fake-pi internal/bootstrap/evaluation_test.go internal/tooling/evaluation/flow_test.go
git commit -m "test: cover flow continuation after provider outage"
```

### Task 9: Update normative docs, changelog, and generated schemas

**Files:**
- Modify: `docs/09-runtime-semantics.md`
- Modify: `docs/10-assistant-adapter-spec.md`
- Modify: `docs/03-specification.md`
- Modify: `docs/05-implementation-status.md`
- Modify: `docs/13-evaluation-plan.md`
- Modify: `CHANGELOG.md`
- Modify: `schemas/assistant-protocol.schema.json`
- Modify: `schemas/run-state.schema.json`
- Modify: `schemas/evaluation-report.schema.json`
- Test: `internal/schemacontract/*_test.go`, `internal/schemasubset/*_test.go`, and existing documentation inventory tests

- [ ] **Step 1: Document the exact contract.** State the transient evidence list, three total Takt `SessionAdapter.Run` calls, Pi/OpenCode internal retries excluded from that bound, 2s/4s defaults, direct `Retry-After` override capped at 60s, same Session ID, separate provider/workflow attempts, recovery semantics, event names, and evaluation denominator behavior. Explicitly state that domain side effects do not use this retry. Put adapter/protocol details in `docs/10`, scheduler semantics in `docs/09`, author-visible no-config behavior in `docs/03`, evaluation denominators in `docs/13`, and implementation status in `docs/05`.

- [ ] **Step 2: Update schemas.** Ensure strict JSON schemas accept every new persisted/report/protocol field and reject invalid scope, negative provider attempt, negative retry delay, unknown outcome, and unknown failure kind.

- [ ] **Step 3: Run contract tests.**

Run: `go test ./internal/schemacontract ./internal/schemasubset ./internal/architecture -count=1`

Expected: PASS.

- [ ] **Step 4: Commit.**

```bash
git add docs/03-specification.md docs/05-implementation-status.md docs/09-runtime-semantics.md docs/10-assistant-adapter-spec.md docs/13-evaluation-plan.md CHANGELOG.md schemas
git commit -m "docs: specify provider availability recovery"
```

### Task 10: Full verification and release gate

**Files:**
- Modify: none unless a verification failure identifies a required regression.
- Test: all repository tests and release checks.

- [ ] **Step 1: Format changed Go files.**

Run: `gofmt -w cmd internal sdk reference tests`

Expected: exit 0 and no unrelated formatting changes.

- [ ] **Step 2: Run targeted suites.**

Run: `go test ./internal/execution ./internal/assistant ./internal/extensions/assistants/pi ./internal/extensions/assistants/opencode ./internal/runtime ./internal/application ./internal/bootstrap ./internal/tooling/evaluation -count=1`

Expected: PASS.

- [ ] **Step 3: Run race tests for changed runtime paths.**

Run: `go test -race ./internal/extensions/assistants/pi ./internal/extensions/assistants/opencode ./internal/runtime ./internal/application ./internal/bootstrap ./internal/tooling/evaluation -count=1`

Expected: PASS with no race reports.

- [ ] **Step 4: Run repository checks.**

Run: `go test ./... -count=1`

Expected: PASS, including `tests/e2e`; pre-existing unrelated flakes must be recorded rather than silently reclassified.

- [ ] **Step 5: Run static and schema checks.**

Run: `go vet ./... && git diff --check && ./scripts/verify.sh`

Expected: exit 0. If `scripts/verify.sh` reports a known unrelated environment failure, record the exact command/output and do not call the provider-recovery implementation complete.

- [ ] **Step 6: Review durable evidence.**

Run: `rg -n 'provider\.retry\.(scheduled|ready|exhausted)|provider_unavailable|infrastructure_error' internal docs schemas`

Expected: every contract occurrence has corresponding code/test/docs coverage; no stale fixed-denominator wording remains.

- [ ] **Step 7: Commit verification-only corrections, if any.**

```bash
git status --short
git diff --check
```

Do not stage `.takt/`. A clean worktree means only intended source/docs commits remain.

## Spec Coverage Self-Review

- Provider classification: Tasks 1–3.
- Durable provider scope, same session, 3-call cap, backoff, and workflow-attempt separation: Task 4.
- Pause/restart/daemon recovery: Task 5.
- Durable events and trace: Task 6.
- Evaluation infrastructure outcome and denominator: Task 7.
- Continuation to later cases and real flow regression: Task 8.
- Protocol, runtime, evaluation schemas and normative docs: Task 9.
- Full verification: Task 10.
- Domain side-effect exclusion and parent cancellation precedence are explicit global constraints and regression assertions in Tasks 1, 4, and 9.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-13-provider-availability-recovery.md`. Execute task-by-task with the required subagent-driven-development or executing-plans skill. If implementation discovers that an adapter cannot prove transient status, same-session retry cannot be guaranteed, or denominator exclusion breaks report compatibility, stop and return to the design-unknowns audit instead of changing the contract implicitly.
