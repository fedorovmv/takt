# Takt evaluation analysis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `takt eval analyze`, an advisory read-only LLM investigation of saved flow-evaluation runs that preserves deterministic verdicts and complete executor/session evidence.

**Architecture:** The CLI calls one stable tooling use case. The use case prepares bounded redacted evidence and invokes the fixed `evaluation:analyze` workflow through the existing `application.RunService`/runtime scheduler; it never calls an assistant adapter directly. The assistant uses the dedicated shared Config alias `takt_analyze`, emits a strict `takt-evaluation-analysis/v1alpha1` object, and all output is persisted under a new timestamped analysis directory.

**Tech Stack:** Go, existing Takt runtime/application/tooling boundaries, Pi/OpenCode/process assistant adapters, JSON Schema draft 2020-12, Go contract tests and `tests/e2e`.

**Implementation status:** Complete on `feature/takt-eval-analyze`; live-provider analysis remains intentionally unexecuted.

---

## File map and ownership

Before implementation, keep these boundaries:

| File or directory | Responsibility in this plan |
|---|---|
| `internal/assistant/assistant.go`, `internal/runtime/runner.go`, `internal/store/store.go` | carry typed adapter/session metadata from an assistant result to durable execution records |
| `internal/extensions/assistants/pi/pi.go`, `opencode/opencode.go`, `internal/assistant/process.go`, `internal/assistant/mock.go` | identify the concrete adapter; Pi exposes its session file |
| `internal/tooling/evaluation/flow_evidence.go`, new `flow_executor_evidence.go` | copy redacted sessions before cleanup and write `executor-manifest.json` |
| new `internal/profile/builtin/evaluation/**` | fixed workflow/command used only by the analyzer |
| new `internal/tooling/evaluation/analysis.go`, `analysis_manifest.go`, `analysis_report.go` | selection, manifest preparation, standard workflow invocation callback, persistence and human rendering |
| `internal/tooling/services.go`, `internal/bootstrap/evaluation.go`, `internal/cli/eval_cmd.go` | canonical service, bootstrap wiring and CLI surface |
| `schemas/evaluation-analysis.schema.json`, `schemas/README.md`, `internal/schemacontract/schema_registry_test.go` | machine-readable analysis contract and offline schema coverage |
| `Makefile`, `README.md`, `docs/03-specification.md`, `docs/05-implementation-status.md`, `docs/09-runtime-semantics.md`, `docs/10-assistant-adapter-spec.md`, `docs/13-evaluation-plan.md`, `CHANGELOG.md` | author-facing contract and release traceability |

Do not add an LLM call to `Inspect`, `Stats`, `Compare`, `Report`, or the flow
runner. Do not introduce another scheduler or assistant execution path.

## Global constraints for every task

- Preserve unrelated dirty worktree changes. Stage only files named by the current task.
- Use TDD: add the smallest failing test, run it and observe the expected failure, then implement the minimum change.
- Do not use a real provider/model in tests; use existing fake assistants or a new fake analysis adapter case.
- Every persisted JSON/text value goes through the existing redactor. Known secrets in a non-text session file fail closed.
- A saved original `report.json` must remain byte-for-byte unchanged by analysis, including when analysis fails.
- `takt_analyze` is the Config model alias. The user-facing command and internal workflow are `analyze` and `evaluation:analyze`; do not add a hyphenated Config alias.
- If an implementation discovers that an adapter cannot expose a stable session path, record `unavailable` and continue; never infer a path from an assistant name or arbitrary log text.

### Task 0: Baseline and contract inventory

**Files:** none (read-only), then the plan's test files in later tasks.

- [ ] **Step 1: Record the baseline.**

Run:

```bash
make check
```

Record the exact exit status and any pre-existing failures in the task report. Do not “fix” baseline failures in this feature.

- [ ] **Step 2: Verify the current extension seam.**

Run:

```bash
go test ./internal/assistant ./internal/runtime ./internal/store ./internal/tooling/evaluation ./internal/profile ./internal/schemacontract -count=1
```

Expected: PASS on the baseline tree. Read `internal/assistant/assistant.go`, `internal/runtime/runner.go` (`execResult`, `executeAssistantAction`, `applyExecResult`, `recordExecution`), `internal/store/store.go`, `internal/tooling/evaluation/flow.go`, and `internal/tooling/evaluation/flow_evidence.go` before changing them.

- [ ] **Step 3: Commit only the plan bookkeeping if the execution workflow requires commits.**

```bash
git add docs/superpowers/plans/2026-08-15-takt-eval-analyze.md
git commit -m "docs: plan advisory evaluation analysis"
```

If the user has not authorized commits, leave the plan uncommitted and continue; do not stage unrelated files.

### Task 1: Carry typed adapter and session metadata

**Files:**
- Modify: `internal/assistant/assistant.go`
- Modify: `internal/assistant/process.go`
- Modify: `internal/assistant/mock.go`
- Modify: `internal/extensions/assistants/pi/pi.go`
- Modify: `internal/extensions/assistants/opencode/opencode.go`
- Modify: `internal/runtime/runner.go`
- Modify: `internal/store/store.go`
- Modify: `internal/tooling/evaluation/evaluation.go`
- Modify: `internal/tooling/evaluation/flow_report.go`
- Test: `internal/extensions/assistants/pi/pi_contract_test.go`
- Test: `internal/extensions/assistants/opencode/opencode_contract_test.go`
- Test: `internal/runtime/runner_test.go`

- [ ] **Step 1: Add failing adapter contract assertions.**

Extend `TestPiAdapterContract` with:

```go
if result.Adapter != "pi" || result.SessionPath == "" {
	t.Fatalf("Pi metadata missing: adapter=%q session_path=%q", result.Adapter, result.SessionPath)
}
```

Extend the successful OpenCode contract assertion with `result.Adapter == "opencode"` and assert that `result.SessionPath == ""` (OpenCode must report unavailable rather than inventing a path). Add a runtime test that a fake adapter returning `assistant.Result{Adapter: "fake", SessionPath: "/tmp/session.jsonl"}` produces the same values in `state.Nodes["agent"].Executions[0]` and the latest `NodeState`.

Run:

```bash
go test ./internal/extensions/assistants/pi ./internal/extensions/assistants/opencode ./internal/runtime -run 'PiAdapterContract|OpenCode|Session|Metadata' -count=1
```

Expected: FAIL because `Result.Adapter`/`SessionPath` are not yet defined or persisted.

- [ ] **Step 2: Add the typed fields.**

Add to `assistant.Result`:

```go
Adapter     string // concrete adapter registration: pi, opencode, process, mock
SessionPath string // optional local session file supplied by the adapter
```

Add the same two JSON fields (`adapter`, `session_path`) to `store.ExecutionState` and `store.NodeState`; add corresponding fields to `runtime.execResult`, `evaluation.NodeRecord`, and `evaluation.ExecutionRecord`. Keep `omitempty` on the path and adapter fields. Do not add `SessionPath` to the stable NDJSON protocol envelope; it is local adapter result metadata.

- [ ] **Step 3: Set metadata at every adapter boundary.**

Set `Adapter` to the concrete string in every result path:

- Pi: set `Adapter: "pi"`; after decoding `stateAfter`, set `SessionPath: stateAfter.SessionFile` when non-empty, including provider-failure results that reached final state.
- OpenCode: set `Adapter: "opencode"`; leave `SessionPath` empty because this adapter does not expose a stable local session file.
- process assistant: set `Adapter: "process"`; preserve an empty path unless the process protocol is later extended through a typed local wrapper.
- mock assistant: set `Adapter: "mock"`.

Never parse raw stdout in the runtime to derive these fields.

- [ ] **Step 4: Verify the propagation.**

```bash
gofmt -w internal/assistant internal/extensions/assistants/pi internal/extensions/assistants/opencode internal/runtime internal/store internal/tooling/evaluation
go test ./internal/assistant ./internal/extensions/assistants/pi ./internal/extensions/assistants/opencode ./internal/runtime ./internal/tooling/evaluation -count=1
```

Expected: PASS, with the metadata present in durable execution records and old fixtures still decoding because fields are optional.

- [ ] **Step 5: Commit the focused change.**

```bash
git add internal/assistant internal/extensions/assistants/pi internal/extensions/assistants/opencode internal/runtime internal/store internal/tooling/evaluation
git commit -m "feat: persist assistant adapter session metadata"
```

### Task 2: Persist redacted executor/session evidence before flow cleanup

**Files:**
- Create: `internal/tooling/evaluation/flow_executor_evidence.go`
- Modify: `internal/tooling/evaluation/flow_evidence.go`
- Modify: `internal/tooling/evaluation/inspect.go`
- Test: `internal/tooling/evaluation/flow_evidence_test.go`
- Test: `internal/tooling/evaluation/inspect_test.go`
- Test: `internal/tooling/evaluation/flow_git_test.go`
- Modify: `schemas/README.md`

- [ ] **Step 1: Add failing manifest/copy tests.**

Add `TestWriteFlowEvidenceCopiesRedactedPiSession` that creates a regular JSONL session containing `known-secret`, a `RunState` with one execution whose `Adapter` is `pi` and `SessionPath` points to the fixture, calls `WriteFlowEvidence`, and asserts:

```go
manifest := readJSON(filepath.Join(repeatRoot, "executor-manifest.json"))
got := manifest["executions"].([]any)[0].(map[string]any)
if got["adapter"] != "pi" || got["session_evidence"] != "recorded" || got["session_evidence_path"] != "sessions/implement/attempt-001.jsonl" {
	t.Fatalf("unexpected executor manifest entry: %#v", got)
}
sessionBytes, _ := os.ReadFile(filepath.Join(repeatRoot, "sessions/implement/attempt-001.jsonl"))
if strings.Contains(string(sessionBytes), "known-secret") {
	t.Fatal("session evidence contains a redacted secret")
}
```

Add cases for a missing path (`session_evidence=unavailable`, reason `path_missing`), a symlink (`path_symlink_forbidden`) and a file larger than the session limit (`path_too_large`). Add a test that an unavailable OpenCode path is represented explicitly and does not fail the entire evidence write.

Run:

```bash
go test ./internal/tooling/evaluation -run 'FlowEvidence|Executor|Session' -count=1
```

Expected: FAIL because no executor manifest or session copy exists.

- [ ] **Step 2: Define the durable manifest.**

In `flow_executor_evidence.go`, define:

```go
const FlowExecutorManifestVersion = "takt-evaluation-executor/v1alpha1"

type FlowExecutorManifest struct {
	ReportVersion string                  `json:"report_version"`
	CaseID        string                  `json:"case_id"`
	Repeat        int                     `json:"repeat"`
	Executions    []FlowExecutorExecution `json:"executions"`
}

type FlowExecutorExecution struct {
	RunID                string          `json:"run_id"`
	NodeID               string          `json:"node_id"`
	Attempt              int             `json:"attempt"`
	ProviderAttempt      int             `json:"provider_attempt"`
	Assistant            string          `json:"assistant,omitempty"`
	Adapter              string          `json:"adapter,omitempty"`
	AssistantVersion     string          `json:"assistant_version,omitempty"`
	RequestedModel       *store.ModelRef `json:"requested_model,omitempty"`
	ResolvedModel        *store.ModelRef `json:"resolved_model,omitempty"`
	SessionID            string          `json:"session_id,omitempty"`
	SessionPath          string          `json:"session_path,omitempty"`
	SessionEvidencePath  string          `json:"session_evidence_path,omitempty"`
	SessionEvidence      string          `json:"session_evidence"`
	SessionEvidenceReason string         `json:"session_evidence_reason,omitempty"`
	Resumed              bool            `json:"resumed"`
}
```

Set `report_version` to the constant and sort executions by `run_id`, `node_id`, `attempt`, `provider_attempt`.

- [ ] **Step 3: Copy sessions with bounded, fail-closed rules.**

Implement `writeFlowExecutorManifest(repeatRoot string, item FlowEvidence, redactor *redact.Redactor) error` and call it from `WriteFlowEvidence` after `run.json` is written and before source/worktree cleanup can happen.

Use these fixed limits:

```go
const (
	maxSessionEvidenceBytes = 4 << 20
	maxSessionEvidenceTotal = 32 << 20
)
```

For every `ExecutionState` with a non-empty `SessionPath`, require an absolute regular non-symlink path, reject files over 4 MiB as unavailable, read the bytes, run `redactor.Bytes`, reject a secret-bearing non-UTF-8 file, and write `sessions/<safe-node-id>/attempt-<NNN>-provider-<NNN>.jsonl` atomically. Repeated references to the same source path still get one deterministic destination per execution record. For empty paths write `session_evidence=unavailable` and reason `adapter_did_not_expose_path`. For missing files write `path_missing`; do not fail the whole evaluation. If aggregate copied bytes would exceed 32 MiB, mark remaining paths `aggregate_limit` without copying.

The manifest itself is written through `writeFlowJSON` so paths and metadata are redacted like all other evidence.

- [ ] **Step 4: Expose the manifest to deterministic inspection.**

Extend `InspectionEvidence` with:

```go
ExecutorManifest string `json:"executor_manifest,omitempty"`
```

Set it from `cases/<case>/repeat-<NNN>/executor-manifest.json`. Add `Executor manifest` to human output and include the path in the strict inspection schema fixture.

- [ ] **Step 5: Verify and commit.**

```bash
gofmt -w internal/tooling/evaluation
go test ./internal/tooling/evaluation -run 'FlowEvidence|Executor|Session|Inspect' -count=1
go test ./internal/schemacontract -run Schema -count=1
git diff --check
```

Expected: PASS; an old run without `executor-manifest.json` remains readable with an unavailable evidence path.

```bash
git add internal/tooling/evaluation schemas/README.md
git commit -m "feat: preserve redacted evaluation executor sessions"
```

### Task 3: Add the analysis output contract and built-in read-only workflow

**Files:**
- Create: `schemas/evaluation-analysis.schema.json`
- Create: `internal/profile/builtin/evaluation/profile.yaml`
- Create: `internal/profile/builtin/evaluation/config.example.yaml`
- Create: `internal/profile/builtin/evaluation/workflows/analyze.yaml`
- Create: `internal/profile/builtin/evaluation/commands/analyze.md`
- Modify: `internal/profile/profile_test.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/schemacontract/schema_registry_test.go`
- Modify: `schemas/README.md`

- [ ] **Step 1: Add failing schema/profile tests.**

Add a schema registry fixture with `report_version: takt-evaluation-analysis/v1alpha1`, `status: completed`, one selected case and the exact fields `deterministic`, `analysis`, `model`, `session`, `usage`, and `errors`. Assert that the fixture validates and that an extra top-level property or `primary_class: random` is rejected. Add a profile test that `profile.Init("evaluation", dir)` installs `profile.yaml`, `workflows/analyze.yaml`, and `commands/analyze.md`, then `profile.Resolve("evaluation:analyze", dir)` returns the installed workflow.

Run:

```bash
go test ./internal/schemacontract ./internal/profile ./internal/config -run 'Analysis|Evaluation|Schema|Profile' -count=1
```

Expected: FAIL because the schema and built-in profile do not exist.

- [ ] **Step 2: Create the strict JSON Schema.**

`schemas/evaluation-analysis.schema.json` must use draft 2020-12, `additionalProperties: false` at every object level, and require:

```text
report_version = takt-evaluation-analysis/v1alpha1
output_dir, source_evaluation_dir, status, selected_cases, analyses
```

`status` is exactly `completed|no_cases|failed`. The top-level object also requires `started_at`, `finished_at`, `duration_ms`, and `model` (the resolved `takt_analyze` model). Each case analysis requires `case_id`, `repeat`, `deterministic`, `analysis_status`, `model`, `session`, `usage`, and `evidence_fingerprint`. The `analysis` property is required only when `analysis_status=completed`; otherwise a non-empty `error_code` and `error` are required. Encode that rule with JSON Schema `allOf`: an `if` checking `analysis_status=completed` and a `then` requiring `analysis`, plus an `else` requiring `error_code` and `error`. `analysis_status` is `completed|provider_unavailable|timed_out|protocol|persistence_error|not_run`. The nested `analysis` object uses the approved fields `primary_class`, `failure_mode`, `confidence`, `root_cause`, `causal_chain`, `evidence`, `contributing_factors`, `recommended_actions`, `missing_evidence`, and `disagreement`; `primary_class` and `confidence` use the approved enums. `model` requires `preset`, `alias`, `provider`, and `id`; `session` requires `adapter`, `session_id`, `session_evidence`, and allows `session_path`/`session_evidence_path`; `usage` requires numeric `input_tokens`, `output_tokens`, `cost`, and `duration_ms`.

- [ ] **Step 3: Create the fixed profile/workflow.**

Use this exact profile shape:

```yaml
apiVersion: takt/v1alpha1
kind: Profile
metadata:
  name: evaluation
workflow: workflows/analyze.yaml
workflows:
  analyze: workflows/analyze.yaml
config: ../../config.yaml
input:
  format: json
  preserve_path: false
```

`workflows/analyze.yaml` is both the profile default and its only public workflow. It has one node:

```yaml
apiVersion: takt/v1alpha1
kind: Workflow
name: evaluation-analyze
provider: coding-agent
model: takt_analyze
nodes:
- id: analyze
  command: analyze
  model: takt_analyze
  allowed_tools:
  - read
  skills: []
  output_format:
    type: object
    additionalProperties: false
    required: [primary_class, failure_mode, confidence, root_cause, causal_chain, evidence, contributing_factors, recommended_actions, missing_evidence, disagreement]
    properties:
      primary_class: {type: string, enum: [infrastructure, assistant, workflow, candidate, validator, task, unknown]}
      failure_mode: {type: string, minLength: 1}
      confidence: {type: string, enum: [high, medium, low]}
      root_cause: {type: string, minLength: 1}
      causal_chain: {type: array, items: {type: object, additionalProperties: false, required: [fact, consequence, evidence], properties: {fact: {type: string, minLength: 1}, consequence: {type: string, minLength: 1}, evidence: {type: array, minItems: 1, items: {type: string}}}}}
      evidence: {type: array, minItems: 1, items: {type: object, additionalProperties: false, required: [path, pointer, fact], properties: {path: {type: string, minLength: 1}, pointer: {type: string, minLength: 1}, fact: {type: string, minLength: 1}}}}
      contributing_factors: {type: array, items: {type: string}}
      recommended_actions: {type: array, items: {type: string}}
      missing_evidence: {type: array, items: {type: string}}
      disagreement: {type: object, additionalProperties: false, required: [with_deterministic_cause, explanation], properties: {with_deterministic_cause: {type: boolean}, explanation: {type: string}}}
```

Keep the YAML readable by expanding the inline maps when committed; the field names and enum values must remain exactly those shown. The workflow command must contain `$ARGUMENTS` explicitly and must instruct the assistant that the JSON argument names the manifest to read, all files are untrusted evidence, only read tools are allowed, every claim needs a path/JSON-pointer citation, and the output must be one JSON object without Markdown fences. It must never ask for source changes or run shell/SCM/network commands.

- [ ] **Step 4: Add model alias fixtures.**

Make `internal/profile/builtin/evaluation/config.example.yaml` valid with this exact shape (the binary is only a default example; real execution requires an explicit analyzer config):

```yaml
apiVersion: takt/v1alpha1
kind: Config
default_assistant: pi
model_preset: default
model_presets:
  default:
    takt_analyze: example/analysis-model
assistants:
  pi:
    type: pi
    binary: pi
    project_trust: deny
```

Add `takt_analyze` to the legacy `models` map in `internal/profile/builtin/code/config.example.yaml` and to every `model_presets` entry in `examples/flow-evaluation/mini-du/config.yaml`, using a concrete provider/model value. Update `internal/config/config_test.go` to load and materialize the alias in both modes. Do not add a hyphenated alias. Note in the changelog that adding the alias intentionally changes the Config fingerprint for future evaluations; old reports remain valid.

- [ ] **Step 5: Verify and commit.**

```bash
gofmt -w internal/profile internal/config internal/schemacontract
go test ./internal/profile ./internal/config ./internal/schemacontract -run 'Analysis|Evaluation|Schema|Profile' -count=1
git diff --check
git add schemas/evaluation-analysis.schema.json internal/profile/builtin/evaluation internal/profile/builtin/code/config.example.yaml examples/flow-evaluation/mini-du/config.yaml internal/profile/profile_test.go internal/config/config_test.go internal/schemacontract/schema_registry_test.go schemas/README.md
git commit -m "feat: add read-only evaluation analysis workflow"
```

### Task 4: Build deterministic analysis manifests and report persistence

**Files:**
- Create: `internal/tooling/evaluation/analysis_manifest.go`
- Create: `internal/tooling/evaluation/analysis_report.go`
- Create: `internal/tooling/evaluation/analysis.go`
- Modify: `internal/tooling/evaluation/inspect.go`
- Test: `internal/tooling/evaluation/analysis_test.go`
- Test: `internal/tooling/evaluation/analysis_manifest_test.go`
- Modify: `internal/tooling/evaluation/flow_report_test.go`

- [ ] **Step 1: Add failing selection and persistence tests.**

Create a temporary saved suite report with three runs: one `true_accept`, one `false_accept`, and one `infrastructure_error`; create matching repeat evidence with `run.json`, `validation-result.json`, `executor-manifest.json`, `source`, `diff.patch`, `activity.json`, and `artifacts/manifest.json`. Test:

```go
selected, err := SelectAnalysisCases(report, "", 0)
if err != nil {
	t.Fatal(err)
}
if len(selected) != 2 || selected[0].CaseID != "false-accept" || selected[1].CaseID != "outage" {
	t.Fatalf("unexpected selected cases: %#v", selected)
}
```

The exact rule is: default selection includes every run whose outcome is not `true_accept`; explicit `--case` selects that case regardless of outcome; explicit `repeat` without case is an error. Add a test that missing `executor-manifest.json` is represented in `missing_evidence` for old runs, while malformed `report.json` returns an error before any workspace/model callback.

Run:

```bash
go test ./internal/tooling/evaluation -run 'Analysis|Select|Manifest|Persistence' -count=1
```

Expected: FAIL because the analysis types/functions do not exist.

- [ ] **Step 2: Define the analysis request, callback and report types.**

In `analysis.go`, add:

```go
const AnalysisReportVersion = "takt-evaluation-analysis/v1alpha1"

type AnalysisRunOptions struct {
	OutputDir, ConfigPath string
	ModelPreset, CaseID string
	Repeat int
	Trace func(string)
	Now func() time.Time
	CaseRunner FlowCaseRunner
}

type AnalysisCaseInput struct {
	CaseID string `json:"case_id"`
	Repeat int `json:"repeat"`
	ManifestPath string `json:"manifest_path"`
	EvidenceRoot string `json:"evidence_root"`
}
```

Define `AnalysisCase`, `AnalysisCaseReport` and `AnalysisRunReport` with explicit JSON tags. Use these exact report fields:

```go
type AnalysisDeterministic struct {
	Status     string `json:"status,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	CauseSource string `json:"cause_source,omitempty"`
	Cause      string `json:"cause,omitempty"`
}
type AnalysisModel struct {
	Preset string `json:"preset"`
	Alias string `json:"alias"`
	Provider string `json:"provider"`
	ID string `json:"id"`
}
type AnalysisSession struct {
	Adapter string `json:"adapter"`
	SessionID string `json:"session_id"`
	SessionPath string `json:"session_path,omitempty"`
	SessionEvidence string `json:"session_evidence"`
	SessionEvidencePath string `json:"session_evidence_path,omitempty"`
}
type AnalysisUsage struct {
	InputTokens int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	Cost float64 `json:"cost"`
	DurationMS int64 `json:"duration_ms"`
}
type AdvisoryAnalysis struct {
	PrimaryClass string `json:"primary_class"`
	FailureMode string `json:"failure_mode"`
	Confidence string `json:"confidence"`
	RootCause string `json:"root_cause"`
	CausalChain []AdvisoryCausalLink `json:"causal_chain"`
	Evidence []AdvisoryEvidence `json:"evidence"`
	ContributingFactors []string `json:"contributing_factors"`
	RecommendedActions []string `json:"recommended_actions"`
	MissingEvidence []string `json:"missing_evidence"`
	Disagreement AdvisoryDisagreement `json:"disagreement"`
}
type AdvisoryCausalLink struct {
	Fact string `json:"fact"`
	Consequence string `json:"consequence"`
	Evidence []string `json:"evidence"`
}
type AdvisoryEvidence struct {
	Path string `json:"path"`
	Pointer string `json:"pointer"`
	Fact string `json:"fact"`
}
type AdvisoryDisagreement struct {
	WithDeterministicCause bool `json:"with_deterministic_cause"`
	Explanation string `json:"explanation"`
}
type AnalysisCaseReport struct {
	CaseID string `json:"case_id"`
	Repeat int `json:"repeat"`
	Deterministic AnalysisDeterministic `json:"deterministic"`
	AnalysisStatus string `json:"analysis_status"`
	Analysis *AdvisoryAnalysis `json:"analysis,omitempty"`
	EvidenceFingerprint string `json:"evidence_fingerprint"`
	Model AnalysisModel `json:"model"`
	Session AnalysisSession `json:"session"`
	Usage AnalysisUsage `json:"usage"`
	ErrorCode string `json:"error_code,omitempty"`
	Error string `json:"error,omitempty"`
}
type AnalysisRunReport struct {
	ReportVersion string `json:"report_version"`
	OutputDir string `json:"output_dir"`
	SourceEvaluationDir string `json:"source_evaluation_dir"`
	Status string `json:"status"`
	StartedAt time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMS int64 `json:"duration_ms"`
	Model AnalysisModel `json:"model"`
	SelectedCases []AnalysisCaseRef `json:"selected_cases"`
	Analyses []AnalysisCaseReport `json:"analyses"`
}
```

Define `AnalysisCaseRef` with `case_id` and `repeat`. Do not reuse `SuiteReport` for this schema.

- [ ] **Step 3: Implement deterministic case selection and manifest construction.**

`AnalyzeFlow` must:

1. require a positive `Now` result and non-nil `CaseRunner`;
2. make `ConfigPath` absolute, load it with `config.Load`, materialize `ModelPreset` with `config.MaterializeModels`, and fail with `model alias "takt_analyze" is not defined in preset "<name>"` if the materialized config has no `takt_analyze` model;
3. load `report.json` with `LoadReport` and call `InspectFlowEvaluation` for each selected case;
4. resolve the repeat root using `cases/<case>/repeat-<NNN>` under the canonical output directory;
5. require the report and each `run.json`; include optional evidence paths as `missing_evidence` entries instead of guessing;
6. read and validate `executor-manifest.json` when present;
7. build a bounded manifest containing only relative evidence paths, file sizes and SHA-256 fingerprints;
8. create `<output>/analyses/<UTC timestamp>/cases/<safe-case>/repeat-<NNN>/workspace`, call `profile.Init("evaluation", analysisWorkspace)` before starting the workflow, and write `workspace/evidence-manifest.json` atomically;
9. set `evidence_root` in that manifest to `filepath.Rel(analysisWorkspace, repeatRoot)`, keep every evidence file path relative to `repeatRoot`, and write the input JSON file containing `manifest_path: "evidence-manifest.json"` and that relative `evidence_root`.

Use the existing `safeCaseID`, `relativeEvidencePath`, `hashPath`/`hashFiles` helpers where available. The assistant resolves an evidence file as `filepath.Join(filepath.Dir(manifestPath), evidence_root, relative_path)`; never place the original evaluation workspace path into the assistant input when a relative evidence path exists. The top-level `<analysis>/manifest.json` lists every case workspace and evidence-manifest path.

- [ ] **Step 4: Implement per-case workflow execution and result capture.**

Before the first case, convert `opts.ConfigPath` to an absolute path; a missing path is an error before `profile.Init` or any model call. For each `AnalysisCase`, call `opts.CaseRunner` with `FlowCaseRunRequest{Workspace: analysisWorkspace, Selector: "evaluation:analyze", ConfigPath: absoluteConfigPath, InputValue: inputJSON, ModelPreset: opts.ModelPreset, Trace: opts.Trace}`. The callback is the existing bootstrap `runFlowCase`, so no direct adapter call or second executor is permitted.

Read the terminal `analyze` node from the returned root snapshot. A completed node with normalized JSON output is `analysis_status=completed`; a provider-unavailable, timeout, protocol or other terminal error maps to the exact status enum in the schema and stores the durable error. Do not accept plain assistant text as an analysis result. Persist the per-case `analysis.json` before moving to the next case.

- [ ] **Step 5: Implement immutable report writes and no-case behavior.**

Use an atomic temporary-file-then-rename writer with mode `0644`. Write the top-level `manifest.json` once before the first case with the source evaluation directory, selected case refs, analyzer config fingerprint, selected preset and resolved `takt_analyze` model. Write top-level `report.json` after every case and once at finalization. If no cases are selected, write `status=no_cases`, `selected_cases=[]`, `analyses=[]` and return success without calling `CaseRunner`. If any case fails, continue remaining cases, write the final report, then return a non-nil error containing the first analysis failure.

- [ ] **Step 6: Verify and commit.**

```bash
gofmt -w internal/tooling/evaluation
go test ./internal/tooling/evaluation -run 'Analysis|Select|Manifest|Persistence|FlowReport' -count=1
git diff --check
git add internal/tooling/evaluation/analysis.go internal/tooling/evaluation/analysis_manifest.go internal/tooling/evaluation/analysis_report.go internal/tooling/evaluation/analysis_test.go internal/tooling/evaluation/analysis_manifest_test.go internal/tooling/evaluation/inspect.go internal/tooling/evaluation/flow_report_test.go
git commit -m "feat: prepare and persist evaluation analysis reports"
```

### Task 5: Wire the canonical service, bootstrap runner and CLI

**Files:**
- Modify: `internal/tooling/services.go`
- Modify: `internal/bootstrap/evaluation.go`
- Modify: `internal/cli/eval_cmd.go`
- Test: `internal/bootstrap/evaluation_test.go`
- Test: `internal/cli/cli_test.go`
- Test: `internal/tooling/services_test.go`

- [ ] **Step 1: Add failing service/CLI tests.**

Add a fake `EvaluationEngine` method assertion for `Analyze`, and CLI parsing tests for:

```text
takt eval analyze run --case c --repeat 2 --config analyzer.yaml --model-preset gemini --trace --json
```

Expected request: `OutputDir=run`, `CaseID=c`, `Repeat=2`, `ConfigPath=analyzer.yaml`, `ModelPreset=gemini`, `Trace != nil`. Assert `--repeat 2` without `--case` returns `repeat requires --case`, negative repeat returns `repeat cannot be negative`, and missing positional directory returns the exact usage string.

Run:

```bash
go test ./internal/tooling ./internal/cli ./internal/bootstrap -run 'Analyze|Eval' -count=1
```

Expected: FAIL because `Analyze` is not in the interface/service/CLI.

- [ ] **Step 2: Add the canonical request and interface method.**

In `internal/tooling/services.go`, add:

```go
type EvaluationAnalyzeRequest struct {
	OutputDir, ConfigPath, CaseID string
	Repeat int
	ModelPreset string
	Trace func(string)
}
```

Add `Analyze(context.Context, EvaluationAnalyzeRequest) (any, error)` to `EvaluationEngine` and `EvaluationService`, with the same nil-service guard and error text pattern as `Inspect`.

- [ ] **Step 3: Wire bootstrap without a new executor.**

Implement `evaluationEngine.Analyze` by calling `evaluation.AnalyzeFlow` with `CaseRunner: e.runFlowCase`, `Now: time.Now`, and the request's config/preset/trace values. Extend `runFlowCase` only if needed to accept the analysis selector; preserve its existing detached start, polling, durable reload, event trace and cleanup behavior. The analysis callback must use `Detached: true`, `KeepWorktree: true`, and the existing `pollFlowCase`/`flowSnapshot`; it must never call `runtime.Runner` directly.

- [ ] **Step 4: Add the CLI case.**

Add `analyze` to the top-level usage string. In `evalCmd`, parse:

```go
configPath := fs.String("config", ".takt/config.yaml", "analyzer config path")
modelPreset := fs.String("model-preset", "", "analyzer model preset")
caseID := fs.String("case", "", "analyze one case")
repeat := fs.Int("repeat", 0, "analyze one repeat of the selected case")
trace := fs.Bool("trace", false, "write analysis progress to stderr")
jsonOut := fs.Bool("json", false, "JSON output")
```

Use `interspersed` with `--config`, `--model-preset`, `--case`, `--repeat`, `--trace`, and `--json`. Require exactly one output directory; reject negative repeat and repeat without case. Use `newEvalTrace(os.Stderr, time.Now)` only when `--trace` is set. Call `service.Analyze` and print a partial saved report before returning an error, matching the existing flow command behavior.

- [ ] **Step 5: Verify and commit.**

```bash
gofmt -w internal/tooling internal/bootstrap internal/cli
go test ./internal/tooling ./internal/bootstrap ./internal/cli -run 'Analyze|Eval' -count=1
git diff --check
git add internal/tooling/services.go internal/bootstrap/evaluation.go internal/cli/eval_cmd.go internal/bootstrap/evaluation_test.go internal/cli/cli_test.go internal/tooling/services_test.go
git commit -m "feat: expose takt eval analyze"
```

### Task 6: Add human output, Make shortcut and documentation

**Files:**
- Modify: `internal/tooling/evaluation/analysis_report.go`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `docs/03-specification.md`
- Modify: `docs/05-implementation-status.md`
- Modify: `docs/09-runtime-semantics.md`
- Modify: `docs/10-assistant-adapter-spec.md`
- Modify: `docs/13-evaluation-plan.md`
- Modify: `schemas/README.md`
- Modify: `CHANGELOG.md`
- Test: `internal/tooling/evaluation/analysis_test.go`
- Test: `tests/e2e/evaluation_contracts_test.go`

- [ ] **Step 1: Add failing human-render test.**

Add `TestAnalysisReportStringShowsDeterministicAndAdvisorySections` and assert the human output contains exactly these section labels and values:

```text
ANALYSIS
  Status        completed
  Model         gemini/gemini-3.7-flash-high
CASE implement-basic#1
  Deterministic false_accept validator mini_du_invalid
  Advisory     candidate / missing_artifact high
  Root cause   validator-required implementation.md was absent
  Evidence     cases/implement-basic/repeat-001/validation-result.json#/result/diagnostics/0
```

The renderer must show `UNAVAILABLE` for absent evidence and must not print a second “quality verdict”.

- [ ] **Step 2: Implement human output and Make target.**

Implement `AnalysisRunReport.String()` with deterministic report status first, then model/session identity, then one case block per analysis, then errors. Keep `--json` as the only machine-readable output path.

Add to `Makefile`:

```make
eval-analyze:
	@test -n "$(RUN)" || { echo 'usage: make eval-analyze RUN=.takt/evals/... [CASE=case-id] [REPEAT=1] [EVAL_CONFIG=path] [EVAL_PRESET=name]'; exit 1; }
	@go run ./cmd/takt eval analyze "$(RUN)" $(if $(CASE),--case "$(CASE)") $(if $(REPEAT),--repeat "$(REPEAT)") $(if $(EVAL_CONFIG),--config "$(EVAL_CONFIG)") $(if $(EVAL_PRESET),--model-preset "$(EVAL_PRESET)") --trace --json=false
```

Add `eval-analyze` to `.PHONY`. Do not make the Make target invoke analysis implicitly after `eval-feature`.

- [ ] **Step 3: Document the contract.**

In `docs/03-specification.md`, add the command, `takt_analyze` alias requirement, no-case behavior, timestamped output layout and advisory-only semantics. In `docs/09-runtime-semantics.md` and `docs/10-assistant-adapter-spec.md`, document read-only policy, existing provider retry scope, redaction and typed session metadata. In `docs/13-evaluation-plan.md`, state that analysis is excluded from quality metrics and never changes benchmark identity. Update `docs/05-implementation-status.md`, `schemas/README.md`, `README.md`, and `CHANGELOG.md` with the implemented state and Make example.

- [ ] **Step 4: Verify and commit.**

```bash
gofmt -w internal/tooling/evaluation
go test ./internal/tooling/evaluation ./tests/e2e -run 'Analysis|Evaluation' -count=1
git diff --check
git add internal/tooling/evaluation/analysis_report.go Makefile README.md docs/03-specification.md docs/05-implementation-status.md docs/09-runtime-semantics.md docs/10-assistant-adapter-spec.md docs/13-evaluation-plan.md schemas/README.md CHANGELOG.md tests/e2e/evaluation_contracts_test.go
git commit -m "docs: document advisory evaluation analysis"
```

### Task 7: Add end-to-end safety and failure contracts

**Files:**
- Modify: `internal/testsupport/cmd/takt-fake-pi/main.go`
- Create: `internal/testsupport/cmd/takt-fake-analysis/main.go` only if the existing fake assistant cannot emit strict analysis JSON
- Test: `internal/tooling/evaluation/analysis_test.go`
- Test: `internal/tooling/evaluation/flow_evidence_test.go`
- Test: `tests/e2e/evaluation_contracts_test.go`
- Modify: `internal/architecture/architecture_test.go` only if a new external smoke script is added; prefer no script

- [ ] **Step 1: Add failing integration tests.**

Add a fake analysis adapter case that records the received prompt and policy, reads the manifest path, and returns the exact valid JSON object from Task 3. Add tests for:

1. valid explicit case analysis;
2. default selection of only problem cases;
3. malformed model JSON → `analysis_status=protocol`, saved report, original report unchanged;
4. provider-unavailable analysis → saved `provider_unavailable`, non-zero command result, original report unchanged;
5. `read` allowed and `write`, `edit`, `apply_patch`, `bash`, SCM and network absent from the request policy;
6. Pi session copied and redacted, OpenCode path unavailable;
7. model disagreement retained as advisory `disagreement=true` while deterministic outcome remains unchanged;
8. second invocation creates a different analysis directory and preserves both report files.

Run:

```bash
go test ./internal/tooling/evaluation ./tests/e2e -run 'Analysis|Evaluation' -count=1
```

Expected: FAIL until the fake case, report status mapping and policy/evidence wiring are complete.

- [ ] **Step 2: Implement fake-provider fixtures only as required.**

Extend `internal/testsupport/cmd/takt-fake-pi/main.go` with deterministic cases for a successful analysis JSON result, malformed JSON and provider outage. The fake must emit a stable Session ID and usage and must never write outside the workspace. If an existing fake can already do this, reuse it instead of adding a command.

- [ ] **Step 3: Verify the complete local contract.**

```bash
gofmt -w cmd internal sdk reference tests
go test ./internal/tooling/evaluation ./internal/bootstrap ./internal/cli ./internal/profile ./internal/schemacontract ./tests/e2e -run 'Analysis|Evaluation|Schema|Profile' -count=1
go test ./internal/execution ./internal/extensions/assistants/pi ./internal/runtime -run 'Provider|Session|Retry' -count=1
go vet ./...
git diff --check
```

Expected: all commands PASS; no live provider is contacted.

- [ ] **Step 4: Commit the contract suite.**

```bash
git add internal/testsupport internal/tooling/evaluation internal/bootstrap internal/cli internal/profile internal/schemacontract tests/e2e
git commit -m "test: cover advisory evaluation analysis"
```

### Task 8: Final verification and handoff

**Files:** all files from Tasks 1–7; no new source files.

- [ ] **Step 1: Run formatting and targeted contracts.**

```bash
gofmt -w cmd internal sdk reference tests
go test ./internal/assistant ./internal/config ./internal/execution ./internal/extensions/assistants/pi ./internal/extensions/assistants/opencode ./internal/profile ./internal/runtime ./internal/schemacontract ./internal/tooling/evaluation ./tests/e2e -count=1
go vet ./...
git diff --check
```

- [ ] **Step 2: Run the full required suite.**

```bash
go test ./... -count=1
go test -race ./... -count=1
```

Expected: both exit 0. Existing unrelated unstable tests must be reported with their exact package/test names rather than hidden.

- [ ] **Step 3: Run the repository release gate.**

```bash
make check
./scripts/verify.sh
```

Expected: exit 0, or a documented pre-existing failure with no attribution to this feature.

- [ ] **Step 4: Execute one deterministic local analysis E2E without a real model.**

Use the fake assistant/config fixture and run:

```bash
go test ./tests/e2e -run '^TestEvaluationAnalysisBoundary$' -count=1 -v
```

The test must assert that the output directory contains `report.json`, `manifest.json`, per-case `analysis.json`, evidence citations, session metadata, and no mutation of the source evaluation report.

- [ ] **Step 5: Run a live analysis only after explicit user request.**

```bash
make eval-analyze RUN=.takt/evals/<saved-run> EVAL_CONFIG=<analyzer-config.yaml> EVAL_PRESET=<preset>
```

Before running, verify the selected preset contains `takt_analyze`, credentials are available, and the user explicitly requested a live provider call. Report the exact analysis directory, model/provider, Session ID and analysis status.

## Self-review checklist

- [x] Design requirement “command backed by ordinary flow” is covered by Tasks 3–5 and reuses `runFlowCase`/`application.RunService`.
- [x] Dedicated model is covered by `takt_analyze`, with no role fallback and no invalid hyphenated alias.
- [x] Default all-problem-cases and explicit valid-case selection are covered by Task 4.
- [x] Adapter identity and recorded session paths are carried, copied before cleanup, redacted and surfaced by Tasks 1–2.
- [x] Strict output, evidence citations, confidence and disagreement are covered by Tasks 3–4.
- [x] Advisory-only behavior and byte-for-byte original report preservation are covered by Tasks 4 and 7.
- [x] Read-only policy, prompt-injection boundary, bounded evidence and provider failure persistence are covered by Tasks 3, 4 and 7.
- [x] CLI, Make, docs, schema registry, normal/race/vet/release checks are covered by Tasks 5, 6 and 8.
- [x] No placeholders (`TBD`, `TODO`, “implement later”) remain in the plan.

Plan complete and saved to `docs/superpowers/plans/2026-08-15-takt-eval-analyze.md`. Execute it task-by-task with the required subagent-driven-development or executing-plans skill after user review.
