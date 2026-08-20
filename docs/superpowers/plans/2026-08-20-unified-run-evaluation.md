# Unified Run Evaluation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the external fixed-stage evaluator with one durable Run whose matrix branches execute an authored DAG and emit immutable assessments.

**Architecture:** First pin terminal Run results and add assessment artifacts/queries. Then add a sequential `matrix` container implemented with the existing `executeGraph`, and make the eval launcher only materialize corpus input, start the workflow once, and evaluate gates from durable Run data.

**Tech Stack:** Go standard library, existing YAML/JSON Schema packages, file Store, existing workflow/runtime/application/appapi/CLI layers.

**Spec:** `docs/superpowers/specs/2026-08-20-unified-run-evaluation-design.md`

## Global constraints

- Keep one scheduler and Store; no case Run or evaluator state machine.
- Only authored `workflow` nodes create child Runs.
- `valid:false` completes assessment; malformed result, missing evidence or persistence failure fails the Run.
- Primary result provenance must be deterministic.
- Assessment never mutates target and pins `target.result_revision`.
- Gate failure changes CLI exit only, after durable reload.
- Preserve legacy report-directory readers.
- Do not access the external `micro-spec-coder` workspace or run live eval.
- No dependency, DB, global registry or plugin framework.
- Every behavior follows observed RED → minimal GREEN.

---

### Task 1: Stable terminal result revision

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/runtime/runner.go`
- Modify: `internal/runtime/cancel.go`
- Modify: `internal/application/operations.go`
- Modify: `schemas/run-state.schema.json`
- Test: `internal/runtime/runner_test.go`
- Test: `internal/application/operations_test.go`
- Test: `internal/schemacontract/schema_registry_test.go`

**Interfaces:**
- Produces `RunState.ResultRevision uint64`.
- It is the latest terminal `run.*` event revision, survives administrative commits, and is cleared before operator retry.

- [ ] **Step 1: Write failing tests**

Start real completed, failed, cancelled and abandoned Runs. Assert `ResultRevision > 0 && ResultRevision <= Revision`. Commit an administrative `worktree.removed` event and assert the value stays unchanged. Retry a failed Run and capture the first running state; assert the old value was cleared.

```go
before := state.ResultRevision
if err := repository.Commit(state, store.Event{Type: "worktree.removed"}); err != nil {
    t.Fatal(err)
}
if state.ResultRevision != before {
    t.Fatalf("result revision changed: %d -> %d", before, state.ResultRevision)
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/runtime ./internal/application -run 'ResultRevision|OperatorRetryClearsResultRevision' -count=1`

Expected: compile failure because the field is missing.

- [ ] **Step 3: Implement minimal transitions**

Add `ResultRevision uint64` with JSON tag `result_revision,omitempty`. Before each terminal Run commit set it to `state.Revision + 1`. Clear it in the shared operator-retry reset. Do not touch it for cleanup/notification/query commits.

- [ ] **Step 4: Add schema and verify GREEN**

Run: `go test ./internal/runtime ./internal/application ./internal/schemacontract -run 'ResultRevision|OperatorRetryClearsResultRevision|RunStateSchema' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/runtime/runner.go internal/runtime/cancel.go internal/application/operations.go schemas/run-state.schema.json internal/runtime/runner_test.go internal/application/operations_test.go internal/schemacontract/schema_registry_test.go
git commit -m "feat(runtime): pin terminal run revisions"
```

### Task 2: Assessment envelope and strict decoder

**Files:**
- Create: `internal/assessment/assessment.go`
- Create: `internal/assessment/assessment_test.go`
- Create: `schemas/assessment.schema.json`
- Modify: `schemas/README.md`
- Modify: `internal/schemacontract/schema_registry_test.go`

**Interfaces:**
- Produces `assessment.Envelope`, `Target`, `Assessor`, `Scope`, `EvidenceRef`, `Decode`, and `Outcome`.
- Consumes existing `validation.Result` and artifact metadata.

- [ ] **Step 1: Write failing strict decoder tests**

Cover one literal valid primary envelope. Reject unknown fields, wrong protocol/type, empty IDs, unsupported role, zero target revision, empty primary case/repeat/evidence, invalid outcome and result/outcome mismatch. Advisory may omit case/repeat/evidence but still requires target/result.

```go
cases := []struct{ status string; valid bool; want string }{
    {store.RunCompleted, true, "true_accept"},
    {store.RunCompleted, false, "false_accept"},
    {store.RunFailed, false, "true_reject"},
    {store.RunFailed, true, "false_reject"},
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/assessment -count=1`

Expected: package/types missing.

- [ ] **Step 3: Implement strict decoding and outcome validation**

Use `json.Decoder.DisallowUnknownFields`, reject trailing values, validate nested result through `validation.Decode`, recompute outcome, and compare it with the envelope. Use concrete structs only.

- [ ] **Step 4: Add offline JSON Schema**

Require exact protocol/type/id/role/target/assessor/result/outcome/evidence/created_at fields and disallow unknown fields recursively. Copy validation-result constraints locally; do not add network references.

- [ ] **Step 5: Verify GREEN and commit**

```bash
go test ./internal/assessment ./internal/schemacontract -run 'Assessment|Outcome|SchemaRegistry' -count=1
git add internal/assessment schemas/assessment.schema.json schemas/README.md internal/schemacontract/schema_registry_test.go
git commit -m "feat: define immutable assessment contract"
```

### Task 3: Assessment node action

**Files:**
- Modify: `internal/spec/spec.go`
- Modify: `internal/workflow/validate.go`
- Modify: `internal/workflow/references.go`
- Modify: `internal/workflow/expand.go`
- Modify: `internal/runtime/actions.go`
- Create: `internal/runtime/assessment.go`
- Create: `internal/runtime/assessment_test.go`
- Modify: `schemas/workflow.schema.json`

**Interfaces:**
- Produces `spec.AssessmentSpec` and the `assessment` node action.
- Artifact type is `assessment`; MIME is `application/vnd.takt.assessment+json`.

- [ ] **Step 1: Write failing workflow/runtime tests**

Use real `store.FS`, a terminal target, deterministic validation output and a real evidence artifact:

```yaml
- id: assess
  depends_on: [validate, evidence]
  assessment:
    role: primary
    target_run_id: run-target
    result_from: $validate.output
    scope: {case_id: case-a, repeat: "1"}
    evidence: [$evidence.artifacts.evaluation-evidence]
```

Assert `valid:false` completes, exactly one strict artifact exists, target bytes are unchanged, and its result revision is pinned. Reject assistant-produced primary, missing evidence, nonterminal target, malformed result and duplicate primary scope.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/workflow ./internal/runtime -run Assessment -count=1`

Expected: unknown action.

- [ ] **Step 3: Add exact spec, authoring and schema**

```go
type AssessmentSpec struct {
    Role        string            `json:"role"`
    TargetRunID string            `json:"target_run_id"`
    ResultFrom  string            `json:"result_from"`
    Scope       map[string]string `json:"scope,omitempty"`
    Evidence    []string          `json:"evidence,omitempty"`
}
```

Require `primary|advisory`, one result reference, nonempty target, and primary scope/evidence. Rewrite its references during composition.

- [ ] **Step 4: Capture using existing Run artifacts**

Load target through the repository, reject nonterminal/zero result revision, decode validation, resolve exact artifact refs, verify SHA-256/size, atomically write canonical JSON under the assessor artifacts directory, and return it in `execResult.Artifacts`. Normal node completion registers it.

- [ ] **Step 5: Enforce provenance, verify GREEN and commit**

Parse `result_from` before render. Primary accepts `bash|script|adapter`; governed workflow only when its selected producer is deterministic. Advisory accepts assistant output.

```bash
go test ./internal/workflow ./internal/runtime ./internal/schemacontract -run 'Assessment|WorkflowSchema' -count=1
git add internal/spec internal/workflow internal/runtime schemas/workflow.schema.json
git commit -m "feat(runtime): record workflow assessments"
```

### Task 4: Assessment query and canonical operation

**Files:**
- Create: `internal/application/assessment.go`
- Create: `internal/application/assessment_test.go`
- Modify: `internal/application/application.go`
- Modify: `internal/appapi/operations.go`
- Modify: `internal/appapi/registry.go`
- Modify: `internal/appapi/registry_test.go`
- Modify: `internal/cli/operations.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `docs/71-canonical-operation-contracts.generated.md`

**Interfaces:**
- Produces `RunService.Assessments(AssessmentQuery)` with target/assessor relation and stale calculation.
- Adds canonical `run.assessment` and MCP `takt.run.assessment`.

- [ ] **Step 1: Write failing application tests**

Create real Run directories and assessment artifacts. Query by target and assessor, filter by role, change target result revision and assert stale, corrupt bytes and assert `assessment_corrupt` rather than silent omission.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/application -run Assessment -count=1`

Expected: method missing.

- [ ] **Step 3: Implement one bounded local scan**

Reuse `ListRunIDs`, `Load`, artifact type, strict decoder and checksum verification. Sort by creation time, assessor Run ID and assessment ID. Add no cache/index.

- [ ] **Step 4: Register once and expose CLI**

Add a schema-first descriptor with `run_id`, `role`, `include_stale`; bind the typed request and add `takt run assessment`. Regenerate canonical docs with the existing renderer.

- [ ] **Step 5: Verify GREEN and commit**

```bash
go test ./internal/application ./internal/appapi ./internal/cli -run 'Assessment|CanonicalOperation' -count=1
git add internal/application internal/appapi internal/cli docs/71-canonical-operation-contracts.generated.md
git commit -m "feat(run): query immutable assessments"
```

### Task 5: Sequential matrix branches in one Run

**Files:**
- Modify: `internal/spec/spec.go`
- Modify: `internal/store/store.go`
- Modify: `internal/workflow/expand.go`
- Modify: `internal/workflow/validate.go`
- Modify: `internal/workflow/references.go`
- Modify: `internal/flowref/flowref.go`
- Modify: `internal/flowref/flowref_test.go`
- Create: `internal/runtime/matrix.go`
- Create: `internal/runtime/matrix_test.go`
- Modify: `internal/runtime/actions.go`
- Modify: `internal/runtime/template.go`
- Modify: `schemas/workflow.schema.json`
- Modify: `schemas/run-state.schema.json`

**Interfaces:**
- Produces `spec.MatrixSpec`, `store.MatrixBranchState`, `$MATRIX.index|total|item`, ordered output and durable resume.

- [ ] **Step 1: Write failing language tests**

Assert matrix references parse only in template surfaces. Reject empty body, nested matrix/loop, more than 1024 items, duplicate canonical items and non-array source.

- [ ] **Step 2: Write failing runtime tests**

Use two JSON items and a body script recording order. Assert one root Run, no child Runs unless body contains `workflow`, ordered results, `/cases[0]/node` paths and immutable snapshots. Simulate failure at `matrix.branch.completed`; resume must not replay earlier branch. Approval resumes the same index.

- [ ] **Step 3: Verify RED**

Run: `go test ./internal/flowref ./internal/workflow ./internal/runtime -run Matrix -count=1`

Expected: types/action/references missing.

- [ ] **Step 4: Add exact spec/state/schema**

```go
type MatrixSpec struct {
    ItemsFrom string `json:"items_from"`
    As string `json:"as,omitempty"`
    Nodes []Node `json:"nodes"`
    OutputNode string `json:"output_node,omitempty"`
}
```

Add branch state with index, raw item, fingerprint, status, node snapshot, output, primary assessment ID and completion time. Store fingerprint/active index/branches on container NodeState.

- [ ] **Step 5: Execute with existing `executeGraph`**

Follow `rewriteLoopGroup` namespacing but preserve `$MATRIX.*`. Canonicalize all items before branch 0, create child states, execute, enforce primary cardinality when declared, structurally copy snapshot, remove active states, commit branch completion and continue.

- [ ] **Step 6: Extend typed root input references**

For `input.format=json`, resolve `$INPUTS.<field>[.<path>]` from `RunState.Input` with existing JSON path code. Preserve `$INPUTS.input|message` for text. `items_from` must be one exact reference.

- [ ] **Step 7: Verify GREEN and commit**

```bash
go test ./internal/flowref ./internal/workflow ./internal/runtime ./internal/schemacontract -run 'Matrix|JSONInputReference|WorkflowSchema|RunStateSchema' -count=1
git add internal/spec internal/store internal/workflow internal/flowref internal/runtime schemas/workflow.schema.json schemas/run-state.schema.json
git commit -m "feat(runtime): execute matrix branches in one run"
```

### Task 6: Dynamic child references and script stdin

**Files:**
- Modify: `internal/spec/spec.go`
- Modify: `internal/workflow/expand.go`
- Modify: `internal/workflow/references.go`
- Modify: `internal/workflow/validate.go`
- Modify: `internal/runtime/child_run.go`
- Modify: `internal/runtime/script.go`
- Modify: `internal/runtime/child_run_test.go`
- Modify: `internal/runtime/script_artifact_test.go`
- Modify: `schemas/workflow.schema.json`

**Interfaces:**
- Produces templated `workflow.path`, `workflow.repository`, `workflow.keep_worktree`, and `script.stdin`.
- Every matrix item is preflighted before branch 0; resume requires identical resolved identity.

- [ ] **Step 1: Write failing tests**

Assert script receives exact rendered stdin. Assert matrix item selects a contained child workflow/repository. Reject path/symlink escape, missing/nonregular workflow, outside repository, definition drift and invalid later item before earlier side effect.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/runtime ./internal/workflow -run 'ScriptStdin|DynamicChild|MatrixPreflight' -count=1`

Expected: stdin empty and dynamic path rejected.

- [ ] **Step 3: Implement stdin and pinned child identity**

Add `Stdin` to ScriptSpec, rewrite/render with NonShell and assign `cmd.Stdin`. Render path/repository before child load, reuse containment, validate workflow/output and persist resolved path/repository/fingerprint. Static definitions retain eager validation.

- [ ] **Step 4: Preserve child infrastructure kinds**

Derive kind from durable child root/node diagnostics instead of coercing all failures to exit. Only genuine failed/exit target is eligible for `allow_failure`; configuration/protocol/timeout remain failures.

- [ ] **Step 5: Verify GREEN and commit**

```bash
go test ./internal/runtime ./internal/workflow ./internal/schemacontract -run 'ScriptStdin|DynamicChild|MatrixPreflight|ChildFailureKind|WorkflowSchema' -count=1
git add internal/spec internal/workflow internal/runtime schemas/workflow.schema.json
git commit -m "feat(workflow): support matrix-selected child runs"
```

### Task 7: Common Run stats, inspection and gates

**Files:**
- Create: `internal/application/run_observation.go`
- Create: `internal/application/run_observation_test.go`
- Modify: `internal/application/operations.go`
- Modify: `internal/appapi/operations.go`
- Modify: `internal/appapi/registry.go`
- Modify: `internal/cli/operations.go`
- Modify: `internal/cli/eval_cmd.go`
- Modify: `internal/tooling/evaluation/inspect.go`
- Modify: `internal/tooling/evaluation/stats.go`
- Modify: `docs/71-canonical-operation-contracts.generated.md`

**Interfaces:**
- Produces `RunService.Stats`, `RunService.Inspect`, canonical `run.stats`/`run.inspect`, and deterministic gate evaluation.
- Legacy directory readers stay in tooling compatibility only.

- [ ] **Step 1: Write failing common-query tests**

Build a Run with two matrix branches and primary assessments. Assert status/stats/inspect agree on technical state, denominator, outcomes, usage/attempts, cause and evidence. Change one target revision and assert stale data is excluded by default.

- [ ] **Step 2: Write failing gate-separation test**

For completed Run, valid rate `1/2`, gate minimum `1`, assert gate failure contains exact numerator/denominator and reloaded status remains completed.

- [ ] **Step 3: Verify RED**

Run: `go test ./internal/application ./internal/cli -run 'RunStats|RunInspect|AssessmentGate' -count=1`

Expected: methods missing.

- [ ] **Step 4: Implement one observation builder**

Load root/descendants/events/artifacts once through existing ports, call `Assessments`, and build shared facts. Stats and Inspect project those facts; do not copy directory-report parsing into application.

- [ ] **Step 5: Register canonical operations and CLI**

Add schema-first descriptors/handlers. Add `takt run status|stats|inspect|assessment`; keep existing aliases. `eval status|stats|inspect` delegates for a safe Run ID, otherwise uses legacy reader.

- [ ] **Step 6: Verify GREEN and commit**

```bash
go test ./internal/application ./internal/appapi ./internal/cli ./internal/tooling/evaluation -run 'RunStats|RunInspect|AssessmentGate|Legacy' -count=1
git add internal/application internal/appapi internal/cli internal/tooling/evaluation docs/71-canonical-operation-contracts.generated.md
git commit -m "feat(run): unify evaluation observations"
```

### Task 8: Ordinary evaluation launcher

**Files:**
- Create: `internal/tooling/evaluation/input.go`
- Create: `internal/tooling/evaluation/input_test.go`
- Modify: `internal/tooling/evaluation/flow.go`
- Modify: `internal/bootstrap/evaluation.go`
- Modify: `internal/tooling/services.go`
- Modify: `internal/cli/eval_cmd.go`
- Modify: `internal/cli/cli_test.go`
- Create: `schemas/evaluation-input.schema.json`
- Modify: `schemas/README.md`

**Interfaces:**
- Produces strict `takt-evaluation-input/v1alpha1`, one StartRequest/run_id, and gate result after reload.
- Reuses safe case/config/profile/Git/SCM preparation.

- [ ] **Step 1: Write failing input tests**

For two cases × repeat 2 assert four ordered items with absolute input/expected/workflow paths, contained repositories, fingerprints, identity and gates. Assert all preflight finishes before Start is called once; preparation error creates no Run.

- [ ] **Step 2: Write failing lifecycle test**

Run fake evaluation workflow. Assert one root Run, four branches, only authored candidate child Runs, no progress/report source of truth, and gate error only after durable completed state.

- [ ] **Step 3: Verify RED**

Run: `go test ./internal/tooling/evaluation ./internal/bootstrap ./internal/cli -run 'EvaluationInput|OrdinaryEvaluationRun' -count=1`

Expected: current path launches one candidate Run per loop iteration.

- [ ] **Step 4: Implement materialization and one start**

Define strict DTO/schema. Refactor current preparation without weakening path/security checks. Prepare every item, invoke no validator/evidence after start, call stable RunService.Start once through bootstrap, reload and evaluate gates via common stats.

- [ ] **Step 5: Preserve old suite dispatch explicitly**

Only exact `version: takt-flow-evaluation/v1alpha1` uses old runner with deprecation diagnostic. New Workflow form requires target/config/cases flags; never guess from filename.

- [ ] **Step 6: Verify GREEN and commit**

```bash
go test ./internal/tooling/evaluation ./internal/bootstrap ./internal/cli ./internal/schemacontract -run 'EvaluationInput|OrdinaryEvaluationRun|LegacyFlowSuite|EvaluationInputSchema' -count=1
git add internal/tooling/evaluation internal/bootstrap internal/cli schemas/evaluation-input.schema.json schemas/README.md
git commit -m "feat(eval): launch evaluation as one ordinary run"
```

### Task 9: Migrate mini-du DAG and Make targets

**Files:**
- Create: `examples/flow-evaluation/mini-du/workflows/evaluate.yaml`
- Create: `examples/flow-evaluation/mini-du/tools/collect-evidence/main.go`
- Create: `examples/flow-evaluation/mini-du/tools/collect-evidence/main_test.go`
- Create: `examples/flow-evaluation/mini-du/tools/validate/main.go`
- Modify: `examples/flow-evaluation/mini-du/feature-development/suite.yaml`
- Modify: `examples/flow-evaluation/mini-du/review/suite.yaml`
- Modify: `examples/flow-evaluation/mini-du/architect/suite.yaml`
- Modify: `Makefile`
- Test: `tests/e2e/evaluation_contracts_test.go`

**Interfaces:**
- Produces authored preparation/candidate/validation/evidence/assessment DAG with no hidden post-run validator.

- [ ] **Step 1: Write failing collector tests**

Create regular/symlink/broken-symlink source. Assert deterministic tar with manifest, diff, bundle and safe source or source-unavailable diagnostic; never follow symlinks or publish partial archive. Add binary-secret failure.

- [ ] **Step 2: Write failing E2E topology test**

Fake assistants, two cases × two repeats: one root Run, four branches, four primary assessments, authored candidate child Runs but no case Runs. Invalid quality leaves root completed and gate non-zero.

- [ ] **Step 3: Verify RED**

Run: `go test ./examples/flow-evaluation/mini-du/... ./tests/e2e -run 'EvidenceArchive|UnifiedEvaluation' -count=1`

Expected: workflow/tools or launcher missing.

- [ ] **Step 4: Implement helpers and authored DAG**

Use stdlib tar/hash/filesystem and Git subprocesses. Validation helper constructs the existing strict request and invokes the oracle. One matrix workflow takes target/corpus/model from input; runtime names no candidate/validator stages.

- [ ] **Step 5: Update Make without live execution**

Keep EVAL_PRESET/EVAL_IDLE_TIMEOUT. Read-only targets accept Run ID and legacy directory. Do not execute live targets during verification.

- [ ] **Step 6: Verify GREEN and commit**

```bash
go test ./examples/flow-evaluation/mini-du/... ./tests/e2e -run 'EvidenceArchive|UnifiedEvaluation' -count=1
go test ./internal/architecture -run MakefileExposesLiveFlowEvaluationTargets -count=1
git add examples/flow-evaluation/mini-du Makefile tests/e2e internal/architecture
git commit -m "feat(eval): migrate mini-du to authored assessment flow"
```

### Task 10: Documentation, version and complete verification

**Files:**
- Modify: `docs/03-specification.md`
- Modify: `docs/05-implementation-status.md`
- Modify: `docs/09-runtime-semantics.md`
- Modify: `docs/13-evaluation-plan.md`
- Modify: `docs/11-implementation-plan.md`
- Modify: `docs/14-backlog-v0.2.md`
- Modify: `ARCHITECTURE_DECISIONS.md`
- Modify: `skills/takt/SKILL.md`
- Modify: `skills/takt/references/workflows.md`
- Modify: `skills/takt/references/evaluation.md`
- Modify: `skills/takt/VERSION`
- Modify: `skills/takt/README.md`
- Modify: `CHANGELOG.md`
- Modify: `README.md`
- Modify: `VERSION`
- Modify: `internal/version/version.go`

**Interfaces:**
- Documents implemented behavior only.
- Version target `0.1.63-alpha`; skill `0.42.0`.

- [ ] **Step 1: Update normative docs, ADR and skill**

Document exact fields, status/assessment/gate semantics, matrix resume, result revision, CLI and legacy window. Mark design IMPLEMENTED only after tests pass. Teach arbitrary matrix DAG and deterministic primary assessment; retain labelled legacy troubleshooting.

- [ ] **Step 2: Update versions/changelog atomically**

Set root/runtime/current docs to 0.1.63-alpha and skill VERSION/README to 0.42.0. Record migration/compatibility.

- [ ] **Step 3: Run formatting and focused suite**

```bash
gofmt -w cmd internal sdk reference tests examples
go test ./internal/assessment ./internal/flowref ./internal/workflow ./internal/runtime ./internal/application ./internal/appapi ./internal/cli ./internal/tooling/evaluation ./internal/schemacontract ./internal/architecture ./examples/flow-evaluation/mini-du/... ./tests/e2e -count=1
```

- [ ] **Step 4: Run full required gates**

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
make check
./scripts/verify.sh
```

Expected: all exit 0; live eval targets are not run.

- [ ] **Step 5: Completion audit and final commit**

Map every spec section to implementation and passing behavior test. Verify new eval execution never calls the fixed CaseRunner/RunFlowValidator/WriteFlowEvidence chain outside explicit legacy dispatch. Verify status contains only user-owned `.takt/` before final commit.

```bash
git add README.md CHANGELOG.md VERSION ARCHITECTURE_DECISIONS.md docs skills internal/version
git commit -m "docs: release unified run evaluation"
```
