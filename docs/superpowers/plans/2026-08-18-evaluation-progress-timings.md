# Evaluation Progress Timings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist phase, provider, and assistant-tool timing observations in `progress.json` and render them from `takt eval status`.

**Architecture:** Keep timing ownership where the timestamps already exist. The evaluation progress tracker accumulates suite phase durations; the bootstrap activity tracker aggregates Pi diagnostic durations and normalized tool lifecycle intervals. The runtime progress callback merges both into one optional `runtime.timings` object, preserving old snapshots and the existing status command boundary.

**Tech Stack:** Go standard library, existing flow progress JSON contract, Go-native tests, JSON Schema.

---

### Task 1: Define timing contract and red tests

**Files:**
- Modify: `internal/tooling/evaluation/flow_progress.go`
- Test: `internal/tooling/evaluation/flow_progress_test.go`
- Modify: `schemas/flow-evaluation-progress.schema.json`

- [x] Add tests that a progress snapshot renders phase and assistant timing rows, round-trips timing fields, and rejects negative timing values.
- [x] Add a test that refreshing runtime counters preserves the tracker-owned phase timing object.
- [x] Run `go test ./internal/tooling/evaluation -run 'FlowProgress' -count=1`; it failed because timing fields and rendering were absent.
- [x] Add `FlowRuntimeTimings`, `FlowPhaseTimings`, and `FlowAssistantTimings` with optional `runtime.timings`, plus `current.phase_started_at`; validate non-negative durations and clone timing state.
- [x] Extend the progress schema with additive timing definitions and the phase start timestamp.
- [x] Re-run the focused tests and keep the failure limited to accumulation/aggregation behavior.

### Task 2: Accumulate evaluation phase durations

**Files:**
- Modify: `internal/tooling/evaluation/flow_progress.go`
- Test: `internal/tooling/evaluation/flow_progress_test.go`

- [x] Add a fake-clock test that advances `prepare → validator_preflight → workflow` and asserts exact millisecond totals.
- [x] Run the test to verify it fails before tracker accumulation exists.
- [x] Make `flowProgressTracker` initialize timings per case, persist `phase_started_at`, and add elapsed time to the old phase before every atomic write and phase transition.
- [x] Keep terminal/failure writes accumulating the active phase and ensure a runtime refresh cannot reset timings.
- [x] Run `go test ./internal/tooling/evaluation -run 'FlowProgress' -count=1` and verify it passes.

### Task 3: Aggregate provider and tool timings

**Files:**
- Modify: `internal/bootstrap/evaluation.go`
- Test: `internal/bootstrap/evaluation_test.go`

- [x] Add a test feeding `pi.stream.started`, `pi.message.completed`, `tool.started`, and `tool.completed` events and assert wait/stream/total/tool milliseconds in the summarized runtime progress.
- [x] Run the focused bootstrap test and verify it failed before aggregation existed.
- [x] Add locked timing totals and tool start timestamps to `flowActivityTracker`; consume provider `wait_ms`, `stream_ms`, and `total_ms` only from their corresponding lifecycle diagnostics and close tool intervals by `CallID`.
- [x] Merge assistant totals into the next progress snapshot while retaining tracker-owned phase totals.
- [x] Run `go test ./internal/bootstrap -run 'Flow.*Timing|SummarizeFlowRuntimeProgress' -count=1` and verify it passes.

### Task 4: Render and document the live contract

**Files:**
- Modify: `internal/tooling/evaluation/flow_progress.go`
- Modify: `internal/tooling/evaluation/stats.go`
- Test: `internal/tooling/evaluation/flow_report_test.go`
- Modify: `internal/bootstrap/evaluation.go`
- Test: `internal/bootstrap/evaluation_test.go`
- Modify: `docs/03-specification.md`
- Modify: `docs/05-implementation-status.md`
- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `VERSION`
- Modify: `internal/version/version.go`

- [x] Extend human status output with a `TIMINGS` section showing phase durations and assistant wait/stream/total/tool durations; old snapshots show `n/a` when timing data is unavailable.
- [x] Make `eval stats` fall back to an explicit `complete=false`, `status=running` partial `EvaluationStats` built from `progress.json` when `report.json` is not present; leave per-case/per-execution arrays empty rather than inventing attribution.
- [x] Update the progress contract documentation and schema references with additive timing semantics, overlap/parallelism rules, and zero-vs-unavailable behavior.
- [x] Reserve product version `0.1.60-alpha` consistently in the root version, runtime version, README, specification, implementation status, and changelog.
- [x] Run `gofmt -w internal/bootstrap/evaluation.go internal/bootstrap/evaluation_test.go internal/tooling/evaluation/flow_progress.go internal/tooling/evaluation/flow_progress_test.go` and the focused tests.

### Review hardening

- [x] Preserve phase and assistant timing totals across multiple cases/repeats and close the previous case phase before `begin`.
- [x] Reject partial, missing-key, and `null` timing objects while keeping snapshots that omit the whole timing property readable.
- [x] Overlay terminal progress status on checkpointed stats, derive `complete` from final run coverage, and surface malformed progress instead of hiding it behind a readable report.
- [x] Include the currently active phase duration in live stats, matching `eval status`.

### Task 5: Full verification

- [x] Run `go test ./... -count=1` (initial run exposed a stale skill compatibility string; E2E passed after updating it).
- [x] Run `go test -race ./... -count=1`.
- [x] Run `go vet ./...`.
- [x] Run `go test ./internal/schemacontract -count=1` and verify the progress schema fixture remains valid.
- [x] Report the pre-existing `make check` E2E timeout separately from feature tests.
