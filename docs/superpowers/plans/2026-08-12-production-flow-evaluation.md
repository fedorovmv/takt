# Production Flow Evaluation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add reproducible post-run evaluation of exact production workflows, with self-contained cases, independent executable validation, false-accept metrics, fake GitHub fixtures, and a first `mini-du` corpus.

**Architecture:** Extend the existing `internal/tooling/evaluation` runner; do not create another executor. Tooling owns suite/case preparation, post-run validation, evidence, aggregation, and gates, while `internal/bootstrap` supplies a narrow callback that executes and cleans up production Runs exclusively through `application.RunService` and `application.WorktreeService`. The first public report remains `takt-evaluation/v1alpha1` with additive flow-evaluation fields so existing report/compare infrastructure stays canonical.

**Tech Stack:** Go standard library, existing `yamlcodec`, existing application/runtime/profile/store boundaries, Git CLI, POSIX shell only for the bundled fake `gh`, Draft 2020-12 JSON Schema, Go unit/contract/E2E tests.

## Global Constraints

- Work in an isolated Git worktree created with `superpowers:using-git-worktrees`; run `make check` before Task 1 and record the result.
- Baseline at plan authoring time: commit `2f0b862`; `make check` passed on 2026-08-12, including `go test ./...`, `go test -race ./...`, E2E, build, and the TypeScript host-integration smoke. The implementation worker must still rerun it in its own worktree.
- Follow TDD for every behavior change: failing focused test, minimal implementation, focused green test, then commit.
- YAML coordinates; code computes; agents decide. Do not add workflow-language fields or expression operators.
- Reuse `internal/tooling/evaluation`, `validation.Decode`, `RunService`, `WorktreeService`, `profile.Init`, `redact`, and existing report/compare code.
- `internal/tooling/evaluation` must not instantiate `runtime.Runner`, `store.FS`, or `gitworktree` for production Run lifecycle.
- Cases and repeats execute sequentially by lexicographic case ID and repeat number; `v1alpha1` has no parallel flag.
- `valid:false` is a measured result with validator exit `0`; validator exit/timeout/protocol/overflow is infrastructure error.
- Run success is never inferred from agent text: `accepted` means only `Run.status == completed`.
- A missing model slot, invalid suite/case, absent validator, or incomplete SCM fixture fails before the first assistant call.
- All source case trees reject `.git` and symlinks; cleanup targets only canonical paths created under the current evidence root.
- A selector may use only a profile present in the case or a built-in profile materialized into it; evaluation never falls back to the user's home profile directory.
- Fake `gh` is a reproducibility fixture, not a security boundary; it must not claim to sandbox arbitrary network clients.
- Text evidence is redacted before persistence; binary evidence containing a known secret fails closed.
- `make check` and `scripts/verify.sh` stay deterministic and never require Pi/OpenCode credentials or network.
- If implementation contradicts `{{workspace}}`, detached Run, `KeepWorktree`, durable `ExecutionWorkspace`, or a selected production DAG, stop and return to design review per design §21.
- Do not mention private or corporate projects in public Takt files.

## Literal Execution Contract for GPT-5.6-luna

- Execute tasks in numeric order. Do not start a later task while the current task's focused tests are red.
- Every task ends in the listed commit. If a listed file does not need a change because the red test passes through an existing facility, omit that file from `git add` and record the omission under **Deviation Log**; do not add a no-op abstraction.
- `Expected: FAIL` means the new test must fail for the named missing behavior, not because a fixture, import, or unrelated test is broken. Fix fixture/build errors before writing production code.
- Do not rename public JSON/YAML fields, status strings, error codes, CLI flags, evidence paths, or Go types specified in this plan.
- Do not add fields or fallback behavior not specified here. In particular, do not add case setup scripts, parallel cases, generic SCM adapters, a validator plugin interface, `--replace`, or a second execution/store path.
- A callback error with a durable snapshot is recorded and handled according to the exact matrix in Task 8. An error without a durable Run ID is an infrastructure error and stops the suite after persisting the partial report.
- If a required signature is impossible because the current code contradicts a fact in **Design Unknowns Audit**, stop. Append the fact, source, affected task, and clean worktree state to **Deviation Log** and return to design review; do not improvise a different component boundary.

## Exact Path Layout

For evidence root `R`, case ID `C`, and one-based repeat `N`, use exactly:

```text
R/report.json
R/workspaces/C/repeat-NNN/control/
R/workspaces/C/repeat-NNN/baseline/
R/workspaces/C/repeat-NNN/origin.git/       # SCM cases only
R/cases/C/repeat-NNN/run.json
R/cases/C/repeat-NNN/validation-request.json
R/cases/C/repeat-NNN/validation-result.json
R/cases/C/repeat-NNN/validator.stderr
R/cases/C/repeat-NNN/diff.patch
R/cases/C/repeat-NNN/artifacts/manifest.json
R/cases/C/repeat-NNN/artifacts/files/
R/cases/C/repeat-NNN/scm/
```

`NNN` is `fmt.Sprintf("%03d", repeat)`. The control checkout contains the copied task input at `.takt/eval/input.md`, the effective config at `.takt/config.yaml`, and materialized profiles at `.takt/profiles/`. The baseline is a regular read-only-by-contract directory, not a Git worktree. Reject an evidence root that already exists or overlaps the suite directory, any case directory, or the invocation workspace root itself. No `--replace` path exists. A default timestamp collision returns `output already exists`; the user reruns later or passes a new `--output`. Do not append randomness.

## File Structure

New focused files under `internal/tooling/evaluation`:

- `flow_suite.go` — strict suite/expectation loading and path resolution.
- `flow_case.go` — self-contained case discovery, safe copy, profile/config preparation, and fingerprints.
- `flow_git.go` — deterministic Git fixture, baseline snapshot, branches, and local bare remote.
- `flow_validator.go` — validator request protocol and bounded process execution.
- `flow_report.go` — flow record fields, classification, summary, gates, and compare helpers.
- `flow_evidence.go` — redacted evidence writing and containment-checked cleanup.
- `flow.go` — sequential orchestration only.
- `flow_scm.go` — closed-world GitHub fixture preparation.
- `fixtures/fake-gh` — canonical embedded fake GitHub command used by production eval and tests.

Existing files changed at stable boundaries:

- `internal/tooling/services.go` — flow-evaluation request/service method and narrow case lifecycle callback types.
- `internal/bootstrap/evaluation.go` — application-only Run/approval/cancel/worktree-cleanup callback.
- `internal/cli/eval_cmd.go` — `eval flow` and later `eval flow init` parsing.
- `internal/tooling/evaluation/evaluation.go`, `compare.go`, schemas — additive report/summary/compare fields.
- `internal/profile/profile.go` — one read-only `IsBuiltin` helper; no registry or mutable global.
- `tests/e2e/evaluation_contracts_test.go` — black-box CLI contracts.

Public examples live under `examples/flow-evaluation/mini-du/`; they do not enter `make check` with a real model.

---

## Slice 1 — Flow-evaluation mechanism

### Task 1: Define and validate the suite/case contracts

**Files:**
- Create: `internal/tooling/evaluation/flow_suite.go`
- Create: `internal/tooling/evaluation/flow_suite_test.go`
- Create: `schemas/flow-evaluation-suite.schema.json`
- Create: `schemas/evaluation-validator-request.schema.json`
- Modify: `schemas/README.md`
- Modify: `internal/schemacontract/schema_registry_test.go`

**Interfaces:**
- Produces: `LoadFlowSuite(path string) (*FlowSuite, error)` and `LoadFlowExpectation(path string) (*FlowExpectation, error)`.
- Produces exact public version constants `FlowSuiteVersion = "takt-flow-evaluation/v1alpha1"` and `FlowValidatorProtocol = "takt-evaluation-validator/v1alpha1"`.
- `FlowExpectation.Oracle` is `json.RawMessage`; Takt validates only the top-level `takt` envelope and leaves oracle data to the validator.

- [ ] **Step 1: Write failing strict-loader tests.**

  Add table tests for: the full accepted suite, missing workflow/config/cases/validator, unknown field suggestion, invalid duration/output budget/rate, unsupported GitHub mode/require, default gates, explicit-gates-replace-defaults, and expectation unknown top-level field.

  ```go
  func TestLoadFlowSuiteStrictContract(t *testing.T) {
      path := writeFlowTestFile(t, "suite.yaml", `version: takt-flow-evaluation/v1alpha1
  workflow: code:feature-development
  config: config.yaml
  cases: {directory: cases}
  approvals: {default: approved}
  external: {github: {mode: fixture, require: repository}}
  validator:
    id: mini-du
    version: "1"
    command: [go, run, ../../validator]
    path: ../../validator
    timeout: 2m
    max_output_bytes: 1048576
  gates:
    validation_error_rate: {max: 0}
  `)
      suite, err := LoadFlowSuite(path)
      if err != nil { t.Fatal(err) }
      if suite.Workflow != "code:feature-development" || suite.Validator.Timeout != 2*time.Minute {
          t.Fatalf("suite=%+v", suite)
      }
  }
  ```

- [ ] **Step 2: Run the focused test and confirm red.**

  Run `go test ./internal/tooling/evaluation -run 'FlowSuite|FlowExpectation' -count=1`.
  Expected: build failure because `LoadFlowSuite` and types do not exist.

- [ ] **Step 3: Add the minimal strict types and loader.**

  Use these exact shapes; do not rename or merge fields:

  ```go
  type FlowSuite struct {
      Version   string             `json:"version"`
      Workflow  string             `json:"workflow"`
      Config    string             `json:"config"`
      Cases     FlowCasesSpec      `json:"cases"`
      Approvals FlowApprovalsSpec  `json:"approvals,omitempty"`
      External  FlowExternalSpec   `json:"external,omitempty"`
      Validator FlowValidatorSpec  `json:"validator"`
      Gates     *FlowGates         `json:"gates,omitempty"`
      SuitePath, SuiteDir, ResolvedWorkflow, ResolvedConfig, ResolvedCases string `json:"-"`
      Source []byte `json:"-"`
  }

  type FlowCasesSpec struct {
      Directory string `json:"directory"`
  }

  type FlowApprovalsSpec struct {
      Default string `json:"default,omitempty"`
  }

  type FlowExternalSpec struct {
      GitHub *FlowGitHubSpec `json:"github,omitempty"`
  }

  type FlowGitHubSpec struct {
      Mode    string `json:"mode"`
      Require string `json:"require"`
  }

  type FlowValidatorSpec struct {
      ID             string        `json:"id"`
      Version        string        `json:"version"`
      Command        []string      `json:"command"`
      Path           string        `json:"path"`
      TimeoutText    string        `json:"timeout"`
      MaxOutputBytes int           `json:"max_output_bytes"`
      Timeout        time.Duration `json:"-"`
      ResolvedCommand []string     `json:"-"`
      ResolvedPath string          `json:"-"`
  }

  type FlowThreshold struct {
      Min *float64 `json:"min,omitempty"`
      Max *float64 `json:"max,omitempty"`
  }

  type FlowCountThreshold struct {
      Max *int `json:"max,omitempty"`
  }

  type FlowGates struct {
      ValidationErrorRate FlowThreshold      `json:"validation_error_rate,omitempty"`
      ValidRate           FlowThreshold      `json:"valid_rate,omitempty"`
      FalseAcceptRate     FlowThreshold      `json:"false_accept_rate,omitempty"`
      FalseRejectRate     FlowThreshold      `json:"false_reject_rate,omitempty"`
      FlowCompletionRate  FlowThreshold      `json:"flow_completion_rate,omitempty"`
      UnstableCases       FlowCountThreshold `json:"unstable_cases,omitempty"`
  }

  type FlowExpectationTakt struct {
      ApprovalAnswer string `json:"approval_answer,omitempty"`
  }

  type FlowExpectation struct {
      Takt   FlowExpectationTakt `json:"takt,omitempty"`
      Oracle json.RawMessage     `json:"oracle"`
  }
  ```

  Because `json.RawMessage` cannot distinguish an absent field after decode,
  inspect the normalized top-level object before typed decode and require a
  present, non-null `oracle`. `takt` may be absent; when present it is strict.
  Reject an `approval_answer` containing NUL; preserve all other bytes including
  spaces because approval value is user data, not a token.

  Decode with `yamlcodec.Unmarshal`. Keep authored path strings unchanged for the
  portable report. If `workflow` resolves to an existing regular file relative to
  suite dir, store its absolute canonical path in `ResolvedWorkflow`; otherwise
  require no path separator and treat it as a profile selector, leaving
  `ResolvedWorkflow` empty. Store other absolute canonical paths in `Resolved*`;
  preserve exact source bytes for identity. Resolve `config`, `cases.directory`,
  `validator.path`, and an argv[0] containing `/` against the suite directory;
  leave a bare executable name such as `go` unchanged in `ResolvedCommand` for
  `exec.LookPath`. Require non-empty validator ID/version,
  non-empty command/argv[0], `timeout > 0`, `max_output_bytes > 0`, exactly one of
  `min|max` for each present rate gate, only `max` for unstable cases, and all
  rates in `[0,1]`. Apply the exact defaults below only when the `gates` key is
  absent; an explicit empty `gates: {}` has no gates.

  ```go
  FlowGates{
      ValidationErrorRate: FlowThreshold{Max: floatPtr(0)},
      FlowCompletionRate:  FlowThreshold{Min: floatPtr(1)},
      ValidRate:           FlowThreshold{Min: floatPtr(1)},
  }
  ```

  Derive `SuiteName` from `filepath.Base(filepath.Dir(suitePath))`, sanitize it with
  the existing case-ID rules, and reject an empty result. The default output root
  is `<invocation-workspace>/.takt/evals/<SuiteName>/<UTC timestamp>` where the
  timestamp format is `20060102T150405.000000000Z`; inject `Now` and
  `InvocationWorkspace` through the Task 7 `FlowRunOptions` for deterministic
  tests rather than adding global clock/workspace state.

- [ ] **Step 4: Add both Draft 2020-12 schemas and registry documentation.**

  The suite schema must use `additionalProperties:false` at every Takt-owned
  object, require the fields above, constrain rates to `[0,1]`, and allow only
  `fixture` plus `repository|pull_request`. The validator-request schema must
  require `protocol_version`, `type`, `case_id`, `repeat`, `workspace`,
  `baseline_workspace`, `expected_path`, and `run`; `run` requires `id`, `status`,
  and `artifacts_dir`; `external_state` is optional and permits only `scm_dir`.
  In `flow_suite_test.go`, compile both schemas with the already installed
  `github.com/santhosh-tekuri/jsonschema/v6`, validate one accepted and one rejected
  document for each, and assert the schema `$id` is local/offline like existing
  schema contracts. Registry test continues to enforce README/reference hygiene.

- [ ] **Step 5: Run focused contracts and commit.**

  Run:

  ```bash
  go test ./internal/tooling/evaluation -run 'FlowSuite|FlowExpectation' -count=1
  go test ./internal/schemacontract -count=1
  ```

  Commit:

  ```bash
  git add internal/tooling/evaluation/flow_suite.go internal/tooling/evaluation/flow_suite_test.go schemas/flow-evaluation-suite.schema.json schemas/evaluation-validator-request.schema.json schemas/README.md internal/schemacontract/schema_registry_test.go
  git commit -m "feat: define flow evaluation suite contract"
  ```

### Task 2: Discover and safely copy self-contained cases

**Files:**
- Create: `internal/tooling/evaluation/flow_case.go`
- Create: `internal/tooling/evaluation/flow_case_test.go`

**Interfaces:**
- Consumes: `FlowSuite` from Task 1.
- Produces: `DiscoverFlowCases(suitePath string, suite *FlowSuite, onlyID string) ([]FlowCase, error)`.
- Produces: `CopyFlowCaseWorkspace(src, dst string) error`, `CopyFlowTree(src, dst string) error`, and `FingerprintFlowCase(FlowCase) (string, error)`.

- [ ] **Step 1: Write failing discovery/containment tests.**

  Cover required regular `input.md`/`expected.yaml`, required non-empty
  `workspace/`, lexical ordering, exact `--case`, unknown ID plus ordered valid
  IDs, the regex `[A-Za-z0-9][A-Za-z0-9._-]*`, `.`/`..`, case-fold collisions,
  `.git`, every symlink, reserved `.takt/eval|runs|worktrees|evals`, executable-mode
  fingerprint changes, and fingerprint changes for input/expected/workspace/SCM.

  ```go
  func TestDiscoverFlowCasesIsOrderedAndSelfContained(t *testing.T) {
      root := makeFlowCaseTree(t, []string{"z-last", "a-first"})
      cases, err := DiscoverFlowCases(filepath.Join(root, "suite.yaml"), &FlowSuite{Cases: FlowCasesSpec{Directory: "cases"}}, "")
      if err != nil { t.Fatal(err) }
      if got := []string{cases[0].ID, cases[1].ID}; !reflect.DeepEqual(got, []string{"a-first", "z-last"}) {
          t.Fatalf("order=%v", got)
      }
  }
  ```

- [ ] **Step 2: Run focused tests and confirm red.**

  Run `go test ./internal/tooling/evaluation -run 'DiscoverFlowCases|CopyFlowCase|FingerprintFlowCase' -count=1`.
  Expected: missing symbols.

- [ ] **Step 3: Implement the safe case boundary.**

  Use the exact record shape:

  ```go
  type FlowCase struct {
      ID, Root, InputPath, ExpectedPath, WorkspacePath, SCMPath, Fingerprint string
      Expectation *FlowExpectation
  }
  ```

  Treat the directory name as the public case ID; do not sanitize it. Sort IDs
  bytewise with `sort.Strings`; reject two IDs equal under `strings.ToLower` to
  make the corpus portable to case-insensitive filesystems. Walk all four source
  surfaces with `filepath.WalkDir`; reject every
  `entry.Type()&os.ModeSymlink != 0`, every `.git` component, and the reserved
  `.takt` subtrees above. `CopyFlowTree` creates directories `0755`, copies only
  regular files, and preserves file permission bits. `CopyFlowCaseWorkspace`
  calls it only for `FlowCase.WorkspacePath`; Task 3 separately copies `input.md`
  to `.takt/eval/input.md`. Do not reuse old `copyTree`, which skips all `.takt`
  and is wrong for the committed eval tools added in Task 10.

  Fingerprint a canonical sequence of `surface/relative/path`, four octal mode
  digits, and bytes for `input.md`, `expected.yaml`, `workspace/**`, and optional
  `scm/**`, each separated by NUL. Reuse the existing SHA-256 `hashFiles` pattern;
  do not introduce a hashing abstraction.

- [ ] **Step 4: Run focused tests and commit.**

  Run `go test ./internal/tooling/evaluation -run 'FlowCase|DiscoverFlowCases|CopyFlowCase|FingerprintFlowCase' -count=1`.

  Commit:

  ```bash
  git add internal/tooling/evaluation/flow_case.go internal/tooling/evaluation/flow_case_test.go
  git commit -m "feat: add self contained flow evaluation cases"
  ```

### Task 3: Prepare profile, effective config, Git baseline, and clean repeats

**Files:**
- Create: `internal/tooling/evaluation/flow_git.go`
- Create: `internal/tooling/evaluation/flow_git_test.go`
- Modify: `internal/tooling/evaluation/flow_case.go`
- Modify: `internal/tooling/evaluation/flow_case_test.go`
- Modify: `internal/profile/profile.go`
- Modify: `internal/profile/profile_test.go`

**Interfaces:**
- Produces: `profile.IsBuiltin(name string) bool` backed only by the existing embedded FS.
- Produces: `profile.SelectorParts(selector string) (profileName, workflowName string)` as the exported spelling of existing private `splitSelector`; `Resolve` uses it too.
- Produces: `PrepareFlowRepeat(ctx context.Context, suite *FlowSuite, item FlowCase, repeat int, evidenceRoot, hostPath string) (*PreparedFlowRepeat, error)`.
- Produces a baseline snapshot that is not a Git worktree and remains read-only to the validator.

- [ ] **Step 1: Write failing tests for preparation order.**

  Assert: selector parsing distinguishes profile selector from workflow path;
  built-in `code` is materialized; suite config overwrites `profile.Init` config;
  `input.md` is copied to `.takt/eval/input.md`; `.takt/profiles/code` and input are
  in the initial commit; initial tree is clean; baseline equals the exact committed
  agent-input checkout excluding `.git`; repeat 2 does not see repeat 1 changes;
  an external/non-built-in selector is not silently initialized; and missing
  each slot required by the selected table entry fails before the callback (feature
  and comprehensive do not require routing; architect does).

  ```go
  func TestPrepareFlowRepeatCommitsProfileBeforeBaseline(t *testing.T) {
      prepared, err := PrepareFlowRepeat(context.Background(), suite, item, 1, t.TempDir(), os.Getenv("PATH"))
      if err != nil { t.Fatal(err) }
      requireGitClean(t, prepared.ControlWorkspace)
      requireGitTracked(t, prepared.ControlWorkspace, ".takt/profiles/code/workflows/feature-development.yaml")
      requireFileContains(t, filepath.Join(prepared.ControlWorkspace, ".takt/config.yaml"), "fake-implementation")
  }
  ```

- [ ] **Step 2: Run focused tests and confirm red.**

  Run `go test ./internal/profile ./internal/tooling/evaluation -run 'IsBuiltin|PrepareFlowRepeat|FlowGit' -count=1`.

- [ ] **Step 3: Add `profile.IsBuiltin` without a registry.**

  ```go
  func IsBuiltin(name string) bool {
      name = strings.TrimSpace(name)
      if name == "" { return false }
      _, err := fs.Stat(builtins, "builtin/"+name+"/profile.yaml")
      return err == nil
  }
  ```

  Rename private `splitSelector` to exported `SelectorParts` without changing its
  trim/Cut behavior; update `Resolve` and profile tests. Evaluation must not copy
  selector parsing.

- [ ] **Step 4: Implement deterministic repeat preparation.**

  ```go
  type PreparedFlowRepeat struct {
      CaseID, ControlWorkspace, BaselineWorkspace, ConfigPath, InputValue string
      ProfileFingerprint, HostPATHHash string
      BaseCommit, HeadCommit, BareRemote string
      Repeat int
  }
  ```

  Parse a profile selector with `profile.SelectorParts`; a selector with an
  existing file at its suite-resolved path is a workflow path and is never passed
  to `profile.Init`. Copy `case/workspace/` into the exact **Exact Path Layout** control path and
  copy `input.md` to `.takt/eval/input.md`; materialize a missing profile only when
  `profile.IsBuiltin(profileName)` is true. If a non-built-in profile is absent
  from `<control>/.takt/profiles/<name>/profile.yaml`, fail preparation; do not let
  `profile.Resolve` fall back to `~/.takt/profiles`. Copy suite config bytes to
  `.takt/config.yaml` **after** `profile.Init`, then load that copied file with
  `config.Load`. In Task 3 there is no env overlay yet; Task 10 mutates the loaded
  config and rewrites `.takt/config.yaml` before Git commit.

  Use this exact closed table only for the three first-corpus selectors; a path or
  another selector is allowed by the public contract and receives no eval-specific
  slot heuristic, because application preflight remains authoritative:

  ```go
  var productionFlowModelSlots = map[string][]string{
      "code:feature-development":       {"implementation", "review"},
      "code:comprehensive-pr-review": {"implementation", "review"},
      "code:architect":                 {"implementation", "review", "routing"},
  }
  ```

  This table is an eval-corpus preflight only. `RunService.Start` remains the
  authority for the full expanded graph and must reject any other missing model
  reference. Task 11 inventory tests lock this table to the current definitions.

  Initialize Git with branch `main` and local config
  `user.name=Takt Eval`, `user.email=eval@takt.invalid`. For every Git commit set
  `GIT_AUTHOR_DATE=2000-01-01T00:00:00Z` and
  `GIT_COMMITTER_DATE=2000-01-01T00:00:00Z`; commit message is
  `takt eval baseline`. Run `git add --all`, commit, assert
  `git status --porcelain` is empty, then copy the committed checkout at `HEAD`
  to `baseline/` without `.git`. The validator contract forbids writing baseline;
  do not add chmod/permission restoration merely to enforce this trusted-local
  boundary. Fingerprint baseline before and after each validator invocation and
  report `baseline_modified` if it changes. Compute
  `ProfileFingerprint` over `.takt/profiles/<name>` with `hashPath`, and
  `HostPATHHash=sha256(hostPath)`. Do not create a permission-management type.

  `PreparedFlowRepeat.InputValue` is selector-specific because the exact current
  production contracts differ:

  ```text
  code:feature-development -> <control>/.takt/eval/input.md path
  code:comprehensive-pr-review -> byte-for-byte input.md content
  code:architect           -> byte-for-byte input.md content
  all other selector/path values -> <control>/.takt/eval/input.md path
  ```

  The comprehensive and architect content must be one strict JSON object containing at least
  `repository` (string), `pull_request` (positive integer), `fixes_permitted`
  (boolean); comprehensive additionally requires non-empty unique
  `validation_commands` (array of strings). This exception is required by the
  exact DAGs: both reach `review-intake`, which parses PR JSON, and comprehensive's
  `script.runtime: validation` calls `json.Unmarshal(state.Input)`. Passing the
  Markdown-preserved path would prepend a source-file header and break those
  contracts. Do not change profile input semantics or production workflows to hide
  this fact. Pass `InputValue` unchanged to `RunService.Start.Input`.

- [ ] **Step 5: Verify effective config without duplicating model resolution.**

  Do not refer to the Slice 3 example configs from this task; they do not exist
  yet. Unit-test the slot table with temporary configs containing or omitting
  each key. Task 8 proves application preflight still runs before the first
  assistant call, and Task 11 proves the table matches the selected definitions.

- [ ] **Step 6: Run focused tests and commit.**

  Run:

  ```bash
  go test ./internal/profile -run IsBuiltin -count=1
  go test ./internal/tooling/evaluation -run 'PrepareFlowRepeat|FlowGit|ModelSlot' -count=1
  ```

  Commit:

  ```bash
  git add internal/profile/profile.go internal/profile/profile_test.go internal/tooling/evaluation/flow_git.go internal/tooling/evaluation/flow_git_test.go internal/tooling/evaluation/flow_case.go internal/tooling/evaluation/flow_case_test.go
  git commit -m "feat: prepare reproducible flow evaluation workspaces"
  ```

### Task 4: Add the strict post-run validator process

**Files:**
- Create: `internal/tooling/evaluation/flow_validator.go`
- Create: `internal/tooling/evaluation/flow_validator_test.go`

**Interfaces:**
- Produces: `RunFlowValidator(ctx context.Context, spec FlowValidatorSpec, req FlowValidationRequest, suiteDir string) FlowValidationExecution`.
- Produces: `PreflightFlowValidator(ctx context.Context, spec FlowValidatorSpec, caseID string, baseline, expectedPath, suiteDir string) (FlowValidationExecution, string, error)`; it runs the same protocol against the baseline and returns the SHA-256 of canonical validator metadata for benchmark identity.
- Produces status `completed|error`, error codes `validator_start|validator_exit|validator_timeout|validator_cancelled|validator_protocol|baseline_modified`, raw bounded stdout/stderr, duration, and decoded `*validation.Result`.

- [ ] **Step 1: Write the validator outcome matrix as failing tests.**

  Use helper processes to cover: valid true + exit 0, valid false + exit 0,
  malformed/empty/multiple JSON + exit 0, valid envelope + exit 7, timeout,
  cancellation, stdout overflow, stderr overflow, baseline mutation, exact request fields, and a
  preflight request with `run.id=preflight`, `run.status=not_started`, and
  `workspace==baseline_workspace`.

  ```go
  func TestRunFlowValidatorTreatsInvalidAsMeasuredResult(t *testing.T) {
      got := runFlowValidatorFixture(t, `{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":false}`, 0)
      if got.Status != "completed" || got.Result == nil || got.Result.Valid {
          t.Fatalf("execution=%+v", got)
      }
  }
  ```

- [ ] **Step 2: Run focused tests and confirm red.**

  Run `go test ./internal/tooling/evaluation -run FlowValidator -count=1`.

- [ ] **Step 3: Implement one argv process with one shared output budget.**

  Define:

  ```go
  type FlowValidationRequest struct {
      ProtocolVersion string                    `json:"protocol_version"`
      Type            string                    `json:"type"`
      CaseID          string                    `json:"case_id"`
      Repeat          int                       `json:"repeat"`
      Workspace       string                    `json:"workspace"`
      Baseline        string                    `json:"baseline_workspace"`
      ExpectedPath    string                    `json:"expected_path"`
      Run             FlowValidationRun         `json:"run"`
      ExternalState   *FlowValidationExternal   `json:"external_state,omitempty"`
  }

  type FlowValidationRun struct {
      ID           string `json:"id"`
      Status       string `json:"status"`
      ArtifactsDir string `json:"artifacts_dir"`
  }

  type FlowValidationExternal struct {
      SCMDir string `json:"scm_dir"`
  }

  type FlowValidationExecution struct {
      Status    string             `json:"status"`
      ErrorCode string             `json:"error_code,omitempty"`
      Error     string             `json:"error,omitempty"`
      Stdout    []byte             `json:"-"`
      Stderr    []byte             `json:"-"`
      Duration  time.Duration      `json:"-"`
      Result    *validation.Result `json:"result,omitempty"`
  }
  ```

  Require `ProtocolVersion=FlowValidatorProtocol`, `Type="validation_request"`,
  `Repeat >= 0`, absolute existing workspace/baseline/expected paths, and non-empty
  run ID/status before process start. Permit empty `ArtifactsDir` only for
  `run.id=="preflight"`; authoritative requests require an absolute path, but the
  directory may not exist when a failed/waiting Run emitted no artifacts. Use `exec.CommandContext`, one JSON value plus
  trailing newline on stdin, `cmd.Dir=suiteDir`, inherited environment, and the
  existing `assistant.NewOutputBudget` plus two
  `assistant.NewLimitedBuffer` writers so stdout+stderr share one byte budget.
  Decode stdout only with `validation.Decode`. Map cancellation caused by the
  caller to `validator_cancelled`, an internal `context.WithTimeout` deadline to
  `validator_timeout`, `exec.ExitError` to `validator_exit`, start errors to
  `validator_start`, and overflow or Decode failure to `validator_protocol`.
  Any non-zero process exit remains `status:error` even when stdout is a valid
  envelope. Fingerprint baseline with `hashPath` immediately before and after the
  process; any change overrides product output with `status:error`,
  `error_code=baseline_modified`. Never put stderr into `validation.Result`.

  `PreflightFlowValidator` uses `Repeat:0`, `run.id="preflight"`,
  `run.status="not_started"`, empty `artifacts_dir`, and
  `workspace==baseline_workspace`. It accepts either `valid:true` or `valid:false`
  when process/protocol status is `completed`: product validity is irrelevant
  before the agent. Canonicalize metadata with `encoding/json.Marshal` and hash it;
  the hash of a nil map is the hash of JSON `null`. A process/protocol error stops
  before the callback. This is the only self-check lifecycle; do not add
  `--self-check`.

  Both functions execute `spec.ResolvedCommand`; content identity fingerprints
  `spec.ResolvedPath`. Authored command/path remain unchanged in the portable
  report.

- [ ] **Step 4: Run focused tests and commit.**

  Run `go test ./internal/tooling/evaluation -run FlowValidator -count=1`.

  Commit:

  ```bash
  git add internal/tooling/evaluation/flow_validator.go internal/tooling/evaluation/flow_validator_test.go
  git commit -m "feat: add post run flow validator protocol"
  ```

### Task 5: Extend canonical reports, classification, gates, and compare

**Files:**
- Create: `internal/tooling/evaluation/flow_report.go`
- Create: `internal/tooling/evaluation/flow_report_test.go`
- Modify: `internal/tooling/evaluation/evaluation.go`
- Modify: `internal/tooling/evaluation/compare.go`
- Modify: `internal/tooling/evaluation/evaluation_test.go`
- Modify: `schemas/evaluation-report.schema.json`
- Modify: `schemas/evaluation-compare.schema.json`
- Modify: `internal/schemacontract/schema_registry_test.go`

**Interfaces:**
- Add optional `mode:"flow"`, `validation`, `outcome`, and nullable `run_passed` to `RunRecord`; add optional top-level `mode:"flow"` to `SuiteReport`.
- Add total/evaluated/completion/outcome/validation-error counters and nullable rates to `Summary`.
- Add optional `path_sha256` and `oracle_metadata_sha256` to `EnvironmentIdentity`; old reports omit both.
- Produce `ClassifyFlowRecord(*RunRecord)` and `ApplyFlowGates(FlowGates, Summary) []GateResult`.

- [ ] **Step 1: Write failing classification and denominator tests.**

  Cover the four outcome cells, validation error with no outcome, paused with no outcome, `evaluated_runs==0` null rates, measured zero rates, stable/unstable using evaluated repeats only, and default/explicit gates.

  ```go
  func TestClassifyFlowRecordFalseAccept(t *testing.T) {
      record := RunRecord{Status: store.RunCompleted, Validation: &FlowValidationRecord{Status:"completed", Result:&validation.Result{Valid:false}}}
      ClassifyFlowRecord(&record)
      if record.Outcome != "false_accept" || record.RunPassed == nil || *record.RunPassed { t.Fatalf("record=%+v", record) }
  }
  ```

- [ ] **Step 2: Run focused tests and confirm red.**

  Run `go test ./internal/tooling/evaluation -run 'FlowRecord|FlowSummary|FlowGate|CompareFlow' -count=1`.

- [ ] **Step 3: Add flow fields without changing old report semantics.**

  ```go
  type FlowValidationRecord struct {
      Status       string             `json:"status"`
      ErrorCode    string             `json:"error_code,omitempty"`
      Error        string             `json:"error,omitempty"`
      Result       *validation.Result `json:"result,omitempty"`
      DurationMS   int64              `json:"duration_ms"`
  }

  type FlowCleanupRecord struct {
      Status    string   `json:"status"`
      Error     string   `json:"error,omitempty"`
      Paths     []string `json:"paths,omitempty"`
  }

  type FlowSummary struct {
      EvaluatedRuns       int `json:"evaluated_runs"`
      FlowCompleted       int `json:"flow_completed"`
      TrueAccept          int `json:"true_accept"`
      FalseAccept         int `json:"false_accept"`
      TrueReject          int `json:"true_reject"`
      FalseReject         int `json:"false_reject"`
      ValidationErrors    int `json:"validation_errors"`
      ValidRate           *float64 `json:"valid_rate"`
      FalseAcceptRate     *float64 `json:"false_accept_rate"`
      FalseRejectRate     *float64 `json:"false_reject_rate"`
      FlowCompletionRate  *float64 `json:"flow_completion_rate"`
      ValidationErrorRate *float64 `json:"validation_error_rate"`
  }
  ```

  Extend `EnvironmentIdentity` exactly:

  ```go
  PATHSHA256           string `json:"path_sha256,omitempty"`
  OracleMetadataSHA256 string `json:"oracle_metadata_sha256,omitempty"`
  ```

  Add these exact `RunRecord` fields:

  ```go
  Mode       string                `json:"mode,omitempty"`
  Validation *FlowValidationRecord `json:"validation,omitempty"`
  Outcome    string                `json:"outcome,omitempty"`
  RunPassed  *bool                 `json:"run_passed,omitempty"`
  Cleanup    *FlowCleanupRecord    `json:"cleanup,omitempty"`
  ```

  Embed `Flow *FlowSummary `json:"flow,omitempty"`` in `Summary` to keep old
  required fields stable. A flow record sets existing `QualityExpected=true` and
  `Quality=validation.Result` only when validation completed, so score/checks/
  diagnostics/cost aggregation is reused. Do not invent a synthetic
  `QualityNodeStatus`.

  `ClassifyFlowRecord` uses this exact matrix:

  ```text
  validation != completed             => outcome="", run_passed=null
  completed run + valid               => true_accept,  run_passed=true
  completed run + invalid             => false_accept, run_passed=false
  any other run status + invalid      => true_reject,  run_passed=false
  any other run status + valid        => false_reject, run_passed=false
  ```

  Never call it for `pausing|paused`; those records have validation error
  `run_paused` and no outcome.

  Change `qualitySucceeded` to dispatch by `record.Mode`: old workflow eval keeps
  `quality_node_status==completed && quality.valid`; flow eval requires
  `validation.status==completed && validation.result.valid`. In `addSummary`, flow
  validation errors do not increment `QualityRuns`; therefore existing quality
  totals use exactly `evaluated_runs` as denominator. In `finishReport`, flow mode
  keeps `SuccessAt1` and `AverageAttemptsToValid` as `null`, uses post-validator
  completion for `AverageTimeToValidMS`, and makes existing `FinalSuccessRate`
  equal the flow `valid_rate`.

  Change `RunRecord.AttemptsToValid` from `int` to `*int` with JSON tag
  `json:"attempts_to_valid"`: old quality-node mode always stores an explicit
  pointer, including pointer-to-zero when invalid; flow mode stores nil. Update old report tests
  and `evaluation-report.schema.json` to accept `integer|null`; measured old values
  remain integers.

  Calculate the flow denominators literally from design §10.2. `Summary.Total` is
  every repeat for which the callback was started. `FlowCompleted` counts
  `RunCompleted` regardless of validator result. Quality rates are nil when
  `EvaluatedRuns==0`; completion and validation-error rates are nil only when
  `Summary.Total==0`. Measured zero uses a non-nil pointer to `0`.

  Existing `recordFromState` and `addSummary` currently sum public node usage into
  record totals. For flow mode, add `recordFromFlowStates`: copy root
  `state.Usage` directly into record token/cost totals (zero when nil), and never
  increment those totals while walking root/descendant nodes. Node records still
  retain per-node/per-execution usage for identity breakdown. Old mode keeps
  `recordFromState` unchanged. This prevents double-counting expanded and child
  execution usage.

  `record.Attempts`, `Truncated`, `Resumed`, and mixed-identity nodes count actual
  action records only: skip hidden/internal/public-container state entries whose
  `Executions` is empty; include any state with non-empty `Executions` and every
  ordinary public leaf. For `LoopIterations`, emit immutable node records under
  `<runID>/<loop-node-id>[NNN]/<body-node-id>` from each snapshot and do not also
  count `LoopPrevious` (it duplicates the latest snapshot). Include governed child
  states. Add focused tests for expanded subworkflow, loop history, and child Run
  so counts are neither hidden nor duplicated.

- [ ] **Step 4: Extend compare with flow metrics and transitions.**

  Add optional `flow` metrics block containing `valid_rate`,
  `false_accept_rate`, `false_reject_rate`, `flow_completion_rate`, and
  `validation_error_rate`, each as `MetricComparison`. Add nullable
  `baseline_outcome`/`candidate_outcome` to each case comparison. If exactly one
  report has `mode:"flow"`, reject comparison. Require equal benchmark
  fingerprints exactly as today. Paired valid/invalid aggregation continues to
  use `qualitySucceeded`, so a validator error pairs as invalid but remains
  distinguishable through the two outcome fields.

  Update `schemas/evaluation-report.schema.json` for top-level/run `mode`, the two
  environment hashes, validation/outcome/run_passed/cleanup, flow summary, and
  nullable attempts. Update `schemas/evaluation-compare.schema.json` only for the
  new flow metrics and outcome fields; compare reports do not embed environment.
  Extend `internal/schemacontract/schema_registry_test.go` with full report and
  compare fixture validation through `jsonschema/v6`; keep the existing lightweight
  registry walk. This prevents additive fields from drifting beyond the schemas.

- [ ] **Step 5: Update schemas and run contracts.**

  Run:

  ```bash
  go test ./internal/tooling/evaluation -run 'FlowRecord|FlowSummary|FlowGate|CompareFlow|ReportSchema' -count=1
  go test ./internal/schemacontract -count=1
  ```

- [ ] **Step 6: Commit.**

  ```bash
  git add internal/tooling/evaluation/flow_report.go internal/tooling/evaluation/flow_report_test.go internal/tooling/evaluation/evaluation.go internal/tooling/evaluation/compare.go internal/tooling/evaluation/evaluation_test.go schemas/evaluation-report.schema.json schemas/evaluation-compare.schema.json internal/schemacontract/schema_registry_test.go
  git commit -m "feat: report flow evaluation outcomes"
  ```

### Task 6: Add redacted evidence and safe cleanup

**Files:**
- Create: `internal/tooling/evaluation/flow_evidence.go`
- Create: `internal/tooling/evaluation/flow_evidence_test.go`

**Interfaces:**
- Produces `WriteFlowEvidence(root string, item FlowEvidence, redactor *redact.Redactor) error`.
- Produces `CleanupFlowRepeat(root string, paths FlowCleanupPaths) error` for eval-owned control/baseline directories only; production managed worktree cleanup remains the callback's responsibility.

- [ ] **Step 1: Write failing evidence and destructive-boundary tests.**

  Assert atomic JSON writes, redacted state/request/result/stderr/diff, normalized
  report paths, artifact manifest/provenance/hash, changed textual artifact
  redaction, changed binary artifact fail-closed, cleanup refusal for source
  suite/case/invocation workspace, canonical symlink escape refusal, exact-member
  cleanup, and preservation on `KeepWorkspaces` or `run_paused`.

- [ ] **Step 2: Run focused tests and confirm red.**

  Run `go test ./internal/tooling/evaluation -run 'FlowEvidence|CleanupFlow' -count=1`.

- [ ] **Step 3: Implement minimal evidence writing.**

  Define the exact transport records:

  ```go
  type FlowEvidence struct {
      CaseID string
      Repeat int
      States []*store.RunState
      Request FlowValidationRequest
      Validation FlowValidationExecution
      Diff []byte
      Artifacts []store.ArtifactRef
      ArtifactDirs map[string]string
      PreparedHeadCommit string
      SCMDir string
  }

  type FlowCleanupPaths struct {
      ControlWorkspace, BaselineWorkspace, BareRemote string
      Created []string
      Keep, Paused bool
  }
  ```

  Use only the explicit layout from **Exact Path Layout**. `validation-result.json`
  contains `FlowValidationRecord` derived from execution
  (status/error/result/duration_ms, without stdout); `validator.stderr` contains bounded
  stderr; `run.json` contains an object with root run ID and the ordered full
  durable `states` supplied by the callback.
  `diff.patch` is produced by `git -C <execution-workspace> diff --binary HEAD --`
  followed by `git -C <execution-workspace> diff --binary
  <prepared-head-commit>..HEAD --`. Pass `PreparedFlowRepeat.HeadCommit` in
  `FlowEvidence`; do not use the base-branch commit for a PR case. The mini-du
  validator still performs the authoritative filesystem comparison and detects
  untracked files; `diff.patch` is debugging evidence, not the oracle.

  Artifact evidence uses:

  ```text
  artifacts/manifest.json
  artifacts/files/<source-relative-path>
  ```

  Walk every `ArtifactDirs[runID]` recursively because production Markdown commands write
  files such as `implementation.md`, `pr.md`, and `summary.md` directly to
  `$ARTIFACTS_DIR`; those files are not necessarily `state.Artifacts`. Prefix each
  evidence source path with `<runID>/` to prevent child/root collisions. Reject
  symlinks and non-regular files. Join a walked absolute path to `ArtifactRef.Path`
  when present. Sort by source-relative path. Validate every registered source
  against its durable SHA-256 and size before copying. For unregistered files set
  `registered:false`, preserve only source-relative path/mode/size/hash, and never
  invent producer metadata. If registered MIME is textual by
  `redact.TextualMIME`, or an unregistered file is valid UTF-8 without NUL, apply
  `redactor.Bytes`, hash persisted redacted bytes, and mark `redacted:true` when
  changed. For other files, if `redactor.Bytes` reports a known-secret match,
  return an infrastructure error before writing that artifact. The manifest
  retains registered provenance when available plus `source_path`,
  `evidence_path`, persisted `sha256`, `registered`, and `redacted`.

  For JSON/text evidence, marshal first, run the whole byte slice through
  `redactor.Bytes`, validate redacted JSON again, write `<name>.tmp`, call
  `file.Sync`, close, and rename. This follows the existing report atomic-rename
  pattern and adds durability only for required evidence. Normalize absolute
  paths in `report.json` to evidence-root-relative paths; per-repeat request and
  run evidence intentionally retains local absolute paths for debugging as design
  §9.2 permits.

- [ ] **Step 4: Implement containment-checked cleanup and commit.**

  Return immediately when `Keep||Paused`. Canonicalize root and every existing
  target; require `pathContains(root,target)`, reject `target==root`, and require
  exact string membership in `Created` before `os.RemoveAll`. `Created` is filled
  by Task 3/9 only with the control, baseline, and bare-remote paths from **Exact
  Path Layout**; never derive cleanup targets from report JSON or validator input.

  Run `go test ./internal/tooling/evaluation -run 'FlowEvidence|CleanupFlow' -count=1`.

  Commit:

  ```bash
  git add internal/tooling/evaluation/flow_evidence.go internal/tooling/evaluation/flow_evidence_test.go
  git commit -m "feat: persist flow evaluation evidence safely"
  ```

### Task 7: Orchestrate sequential flow cases around a narrow lifecycle callback

**Files:**
- Create: `internal/tooling/evaluation/flow.go`
- Create: `internal/tooling/evaluation/flow_test.go`
- Modify: `internal/tooling/evaluation/runtime_metrics.go`
- Modify: `internal/tooling/evaluation/evaluation_test.go`

**Interfaces:**
- Produces `RunFlow(ctx context.Context, FlowRunOptions) (*SuiteReport, error)`.
- Produces `FlowGateFailureError{Report *SuiteReport}` with error text `flow evaluation gates failed`.
- Callback owns only production Run lifecycle:

  ```go
  type FlowCaseRunner func(context.Context, FlowCaseRunRequest) (FlowCaseRunResult, error)
  ```

- [ ] **Step 1: Write failing orchestration tests with a fake callback.**

  Verify lexical case/repeat order; exact input/selector/config/approval;
  validator preflight before callback; authoritative validator after
  completed/failed/cancelled/abandoned/waiting; `missing_workspace`; no validator
  for paused or caller cancellation; full state/events consumed without Store;
  continuation across product/validation failures; stop on preparation/callback-
  without-snapshot/evidence error; report before cleanup; cleanup error retained;
  no cleanup under `--keep-workspaces`; identity drift; default and explicit gate
  failure only after report persistence.

- [ ] **Step 2: Run focused tests and confirm red.**

  Run `go test ./internal/tooling/evaluation -run 'RunFlow' -count=1`.

- [ ] **Step 3: Add service request and callback types.**

  ```go
  type FlowCaseRunRequest struct {
      Workspace, Selector, ConfigPath, InputValue, ApprovalAnswer string
  }

  type FlowCaseRunResult struct {
      States []*store.RunState
      Events []store.Event
      Artifacts []store.ArtifactRef
      ArtifactDirs map[string]string
      ContextCancelled bool
      Cleanup func(context.Context) (*store.RunState, error)
  }

  type FlowRunOptions struct {
      SuitePath, CaseID, OutputDir, InvocationWorkspace string
      Repeat int
      KeepWorkspaces bool
      Now func() time.Time
      HostPATH string
      CaseRunner FlowCaseRunner
  }
  ```

  Add `Mode string `json:"mode,omitempty"`` to `SuiteReport` in Task 5; Task 7
  sets `Mode:"flow"`, `Workflow:suite.Workflow`, `Config:suite.Config`,
  `CasesDir:suite.Cases.Directory`, and `OutputDir` relative to invocation workspace
  when possible. Do not put temporary control/baseline paths in top-level report
  identity. Construct one `redact.NewFromConfig` from each prepared effective config
  and pass it to evidence writing; never persist secret values from host PATH/env.

  These types live in package `evaluation`; Task 8's bootstrap engine converts
  tooling requests to them. Put callback/clock/PATH in `FlowRunOptions`, not
  package globals or registries. `CaseRunner` is required. The callback always
  receives `KeepWorktree=true` implicitly; do not add a bool that can disable the
  design invariant. `Cleanup` is nil when no managed worktree exists.

  Set callback `Selector` to `suite.ResolvedWorkflow` when non-empty, otherwise to
  authored `suite.Workflow`. This is the only selector/path normalization point.
  Set callback `ApprovalAnswer` to
  `item.Expectation.Takt.ApprovalAnswer` when non-empty, otherwise
  `suite.Approvals.Default`; an empty result means leave waiting.

- [ ] **Step 4: Implement the sequential orchestration loop.**

  Use one `for case` and one nested `for repeat`. Perform these exact stages:

  1. prepare repeat and preflight validator;
  2. call `CaseRunner`; require `len(result.States)>0 && result.States[0]!=nil`
     before appending a
     record or incrementing `Summary.Total`; callback error with no snapshot is a
     pre-Run infrastructure/authoring failure and stops with no synthetic record;
  3. when a snapshot exists, create exactly one record and increment total even if
     callback also returns a terminal/cancellation error;
  4. require `States[0]` as root and build the root `RunRecord` from all full
     snapshots before appending it; root
     state supplies status/timestamps/fingerprints and already-aggregated usage;
     collect node/execution identity/attempt/truncation/resume records from every
     `States` entry, prefixing descendant node keys as `<runID>/<nodeID>`; do not
     add child `state.Usage` to root totals again; apply runtime event metrics with a
     new `applyRuntimeMetricsFromEvents(record,state,events,"")` helper;
  5. if `ContextCancelled`, do not start validator; persist state and return
     `ctx.Err()` after cleanup attempt;
  6. if status is `pausing|paused`, set validation error `run_paused`, persist
     evidence, skip both cleanup functions, write partial report, and stop suite;
  7. otherwise, if `ExecutionWorkspace` is empty or absent on disk, set validation
     error `missing_workspace`; else invoke authoritative validator;
  8. classify, append, and aggregate; write evidence before any cleanup;
  9. unless `KeepWorkspaces`, call callback `Cleanup`, then
     `CleanupFlowRepeat`; store `cleanup.status=completed|error|skipped` and exact
     diagnostics; cleanup errors are infrastructure errors but do not discard the
     measured outcome;
  10. atomically rewrite `report.json` after every repeat and once after final
      summary/gates.

  For no-answer `waiting`, bootstrap cleanup performs cancel→durable reload→remove
  only after waiting evidence is durable; saved record stays waiting. Paused Runs
  are never cleaned. A product invalid result or validator runtime error continues
  to the next repeat. Preparation, callback-without-snapshot, evidence, identity
  drift, and cleanup errors stop after partial report. `FlowGateFailureError`
  contains the report and is returned only after final report persistence.

  Build benchmark identity with existing `hashJSON` over: exact suite source
  bytes SHA-256; ordered `{case_id,fingerprint}`; validator ID/version/content
  fingerprint; validation protocol; HostPATH hash; effective repeat count;
  preflight metadata hash; GOOS/GOARCH/Go version. The validator content
  fingerprint is `hashPath(suite.Validator.ResolvedPath)` and is calculated before the
  callback. Every case preflight metadata hash must equal the first one; mismatch
  is `oracle_identity_drift` and stops before the next callback. Build strategy identity from durable
  `WorkflowFingerprint`, `ConfigFingerprint`, and `CommandsFingerprint`; add the
  materialized profile-tree fingerprint from `PreparedFlowRepeat` so profile
  manifest/tools are covered. Every repeat must produce the same strategy
  identity; mismatch is `strategy_identity_drift`, not a mixed experiment.

  Set report `Environment.PATHSHA256` from the captured host PATH and
  `Environment.OracleMetadataSHA256` from the common preflight metadata hash. Never
  persist the host PATH value.

  `time_to_valid_ms` for a valid flow is
  `state.UpdatedAt-state.CreatedAt + validation.Duration`; clamp negative to zero.
  Do not use a synthetic node-completed event. `attempts_to_valid` remains nil.

- [ ] **Step 5: Run focused tests and commit.**

  Run:

  ```bash
  go test ./internal/tooling/evaluation -run 'RunFlow|FlowOrder|FlowPaused' -count=1
  ```

  Commit:

  ```bash
  git add internal/tooling/evaluation/flow.go internal/tooling/evaluation/flow_test.go internal/tooling/evaluation/runtime_metrics.go internal/tooling/evaluation/evaluation_test.go
  git commit -m "feat: orchestrate production flow evaluations"
  ```

### Task 8: Wire detached application execution and expose `takt eval flow`

**Files:**
- Modify: `internal/application/service.go`
- Modify: `internal/application/service_test.go`
- Modify: `internal/tooling/services.go`
- Create: `internal/tooling/services_test.go`
- Modify: `internal/bootstrap/evaluation.go`
- Create: `internal/bootstrap/evaluation_test.go`
- Modify: `internal/cli/eval_cmd.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `tests/e2e/evaluation_contracts_test.go`

**Interfaces:**
- Application adds one tooling-oriented read-only query `RunService.EvaluationSnapshot(runID string) (*EvaluationSnapshot,error)` returning root plus descendant durable clones, full event journals, artifacts, and artifact directories; it is not registered as CLI/MCP/appapi.
- Bootstrap callback creates the per-case canonical app with package-local `New(caseWorkspace, caseConfig)` and then uses only its `application.RunService.Start/GetRun/Answer/Cancel/EvaluationSnapshot` and `application.WorktreeService.Remove`.
- Tooling adds `EvaluationService.Flow(context.Context, FlowEvaluationRequest) (any,error)` and corresponding `EvaluationEngine.Flow` method.
- CLI: `takt eval flow <suite.yaml> [--case ID] [--repeat N] [--output DIR] [--keep-workspaces] [--json]`.

- [ ] **Step 1: Write the failing application snapshot contract.**

  Add a workflow with one expanded subworkflow and two assistant attempts. Assert
  `GetRun` hides compiled nodes, while `EvaluationSnapshot` contains them and their
  execution identity. Add one governed child Run and assert breadth-first states,
  all root/child events beyond the 1000-item public page limit, sorted durable
  artifacts, claim-token clearing, and no shared mutable maps/slices with the
  Store value.

  Use the exact application type:

  ```go
  type EvaluationSnapshot struct {
      Root         *store.RunState
      States       []*store.RunState
      Events       []store.Event
      Artifacts    []store.ArtifactRef
      ArtifactDirs map[string]string
  }
  ```

  Implementation is one consumer-owned query on existing `RunQueryStore`. Load
  root, then breadth-first `ChildRunIDs`, with a seen set; missing linked child is
  an error after Run observation has stopped. Deep-clone every state through JSON
  marshal/unmarshal; clear every `External.ClaimToken`; append
  `ReadEvents(id,0,0)` for each state; clone/dedupe/sort all artifacts by
  producer-run/ID; set each `ArtifactDirs[id]=s.store.ArtifactsDir(id)`. Sort events
  by time, then RunID, then revision. `Root` points to `States[0]`. Do not expose
  `RunStore`, a path constructor, or a generic raw-state method.

- [ ] **Step 2: Run the application test and confirm red.**

  Run `go test ./internal/application -run EvaluationSnapshot -count=1`.
  Expected: build failure for the missing type/method.

- [ ] **Step 3: Implement the minimal snapshot query and rerun its test.**

  Run `go test ./internal/application -run EvaluationSnapshot -count=1`.
  Expected: PASS.

- [ ] **Step 4: Write failing bootstrap lifecycle tests.**

  Create tiny workflow fixtures for completed, failed, waiting/answer, waiting
  without answer, node timeout, caller cancellation, paused, and managed worktree.
  Assert detached start, exact approval node, `KeepWorktree`, full snapshot/events,
  failed state returned without converting to callback error, and cleanup only
  after the caller invokes the closure. Assert missing model/command fails before a
  Run ID and before the fake assistant marker is written.

- [ ] **Step 5: Write failing tooling-service, CLI, and black-box contracts.**

  `services_test.go` uses a fake engine and proves exact request forwarding plus nil
  engine error. In E2E, create one self-contained path-workflow case (no built-in
  profile dependency) and a helper-process validator. Run `takt eval flow` with a
  fake process assistant; require `report_version=takt-evaluation/v1alpha1`,
  `mode=flow`, one `true_accept`, saved report/evidence, and a second gate-failing
  case returning non-zero after report persistence. Set
  `worktree.enabled:true` in the path-workflow fixture so this test actually proves
  retained-worktree oracle ordering.

- [ ] **Step 6: Run focused tests and confirm red.**

  Run:

  ```bash
  go test ./internal/tooling -run FlowEvaluationService -count=1
  go test ./internal/bootstrap -run FlowEvaluation -count=1
  go test ./internal/cli -run EvalFlow -count=1
  go test ./tests/e2e -run FlowEvaluation -count=1
  ```

- [ ] **Step 7: Add exact service request types and engine dispatch.**

  Add to `internal/tooling/services.go`:

  ```go
  type FlowEvaluationRequest struct {
      SuitePath, CaseID, OutputDir, InvocationWorkspace string
      Repeat int
      KeepWorkspaces bool
  }
  ```

  Add `Flow(context.Context, FlowEvaluationRequest) (any,error)` to
  `EvaluationEngine` and `EvaluationService`. `evaluationEngine.Flow` captures
  host PATH once with `os.Getenv("PATH")`, passes `time.Now` and the request fields
  to `evaluation.RunFlow`, and supplies its case runner method. Add the fake engine
  for this new test in `internal/tooling/services_test.go`; no other current fake
  implements `EvaluationEngine`. Do not add an optional secondary interface. If
  host PATH is empty, return authoring error before preparation; do not create an
  assistant PATH containing only fake bin.

- [ ] **Step 8: Implement detached polling in bootstrap.**

  In `evaluationEngine.runFlowCase`, call package-local
  `New(req.Workspace, req.ConfigPath)` exactly once. Start with:

  ```go
  application.StartRequest{
      Selector: req.Selector, Input: req.InputValue,
      ConfigPath: req.ConfigPath, Detached: true, KeepWorktree: true,
  }
  ```

  `Start` errors before non-empty Run ID return an empty result and error. Poll
  `GetRun` every 50ms. On `waiting` with a non-empty answer, require
  `state.Waiting != nil` and call `Answer(ctx,runID,state.Waiting.NodeID,answer)`;
  the same answer applies to each encountered approval. Return waiting immediately
  when answer is empty. Return terminal completed/failed/cancelled/abandoned and
  pausing/paused observations after calling `EvaluationSnapshot`.

  On caller context cancellation, create
  `cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)`;
  call `Cancel(cleanupCtx,runID,"flow evaluation context cancelled")`; poll
  `GetRun` until terminal or cleanup deadline; call `EvaluationSnapshot`; return it
  with `ContextCancelled:true` and `ctx.Err()`. If durable state still cannot be
  loaded, return the original cancellation error with no snapshot.

  Cleanup closure captures the same case app/Run ID and creates its own
  `context.WithTimeout(context.WithoutCancel(ctx),30*time.Second)`. If observed
  status is waiting, call `Cancel(cleanupCtx,runID,"flow evaluation cleanup")`, poll until
  terminal, then call `WorktreeService.Remove(cleanupCtx,runID,true)`. For an
  already-terminal state call Remove directly. Never call Remove for
  pausing/paused. If no managed worktree exists, return the latest durable state
  without calling Remove. This sequencing is mandatory because current
  `WorktreeService.Remove` rejects running/waiting Runs.

- [ ] **Step 9: Add CLI parsing without a second flag-heavy interface.**

  Create the root app as today, but set `InvocationWorkspace` from
  `filepath.Abs(".")`. Parse an interspersed suite path plus only `--case`,
  `--repeat`, `--output`, `--keep-workspaces`, and `--json`; require repeat > 0.
  `eval flow init` returns `eval flow init is not available until the authoring
  slice is installed`. Existing `eval run|benchmark|task-benchmark|report|compare`
  parsing and defaults remain unchanged. If service returns non-nil report plus
  error, print report first exactly like benchmark, then return the error.

- [ ] **Step 10: Run Slice 1 checks and commit.**

  Run:

  ```bash
  go test ./internal/application -run EvaluationSnapshot -count=1
  go test ./internal/tooling -run FlowEvaluationService -count=1
  go test ./internal/bootstrap -run FlowEvaluation -count=1
  go test ./internal/cli -run EvalFlow -count=1
  go test ./tests/e2e -run FlowEvaluation -count=1
  go test ./internal/tooling/evaluation -count=1
  ```

  Commit:

  ```bash
  git add internal/application/service.go internal/application/service_test.go internal/tooling/services.go internal/tooling/services_test.go internal/bootstrap/evaluation.go internal/bootstrap/evaluation_test.go internal/cli/eval_cmd.go internal/cli/cli_test.go tests/e2e/evaluation_contracts_test.go
  git commit -m "feat: expose production flow evaluation"
  ```

**Slice 1 review checkpoint:** inspect the eight commits, run `go test ./internal/application ./internal/tooling/evaluation ./internal/tooling ./internal/bootstrap ./internal/cli ./tests/e2e -run 'Flow|EvaluationSnapshot|Evaluation' -count=1`, and verify `internal/tooling/evaluation` contains neither `runtime.New(` nor `store.FS{`, and the only production app construction for flow cases is package-local `bootstrap.New`.

---

## Slice 2 — Local Git remote and fake GitHub

### Task 9: Add strict SCM fixture loading and Git branch/remote preparation

**Files:**
- Create: `internal/tooling/evaluation/flow_scm.go`
- Create: `internal/tooling/evaluation/flow_scm_test.go`
- Modify: `internal/tooling/evaluation/flow_git.go`
- Modify: `internal/tooling/evaluation/flow_git_test.go`

**Interfaces:**
- Produces strict `LoadFlowSCMFixture(caseRoot string, require string) (*FlowSCMFixture,error)`.
- Extends `PrepareFlowRepeat` to create base/head commits, apply text patch with `git apply`, create local bare `origin`, and checkout the agent-input head.

- [ ] **Step 1: Write failing SCM fixture tests.**

  Cover `repository` versus `pull_request` required files, unknown fields,
  repository/ref/PR validation, binary/non-UTF-8 patch rejection, absolute/`../` path
  patch rejection, `git apply --check`, distinct base/head SHAs, local bare origin,
  pushed refs, exact head checkout, clean tree, and repeat-local remote paths.

- [ ] **Step 2: Run focused tests and confirm red.**

  Run `go test ./internal/tooling/evaluation -run 'FlowSCM|FlowGitRemote|HeadPatch' -count=1`.

- [ ] **Step 3: Implement closed-world fixture types.**

  ```go
  type FlowSCMFixture struct {
      Repository FlowSCMRepository
      PullRequest *FlowSCMPullRequest
      HeadPatch string
  }

  type FlowSCMRepository struct { Repository, BaseBranch, HeadBranch string }
  type FlowSCMPullRequest struct {
      Number int
      Title, Base, Head, State, CIStatus string
      FixesPermitted bool
  }
  ```

  Keep the YAML tags from design §7.1 exactly: `repository`, `base_branch`,
  `head_branch`; and `number`, `title`, `base`, `head`, `state`, `ci_status`,
  `fixes_permitted`. Require repository to match
  `[A-Za-z0-9._-]+/[A-Za-z0-9._-]+`; branch refs are non-empty, differ, contain no
  whitespace, `..`, `~^:?*[\\`, leading/trailing `/`, or `.lock`; PR number > 0;
  PR base/head equal repository base/head; state is `OPEN|CLOSED|MERGED`; CI status
  is `passed|failed|pending`. `repository` requires only `repository.yaml`;
  `pull_request` requires all three files. Decode with `yamlcodec.Unmarshal`.

  Require UTF-8 `head.patch`, reject NUL and the markers `GIT binary patch`,
  `Binary files`, `--- /`, `+++ /`, and any parsed `a/` or `b/` path containing a
  `..` component. Then use `git apply --check --whitespace=error-all` as the
  authoritative parser. Do not implement a patch parser.

- [ ] **Step 4: Build the deterministic history and local remote.**

  Refactor Task 3 preparation into three private functions only:
  `prepareFlowControl`, `prepareFlowSCM`, and `commitFlowBaseline`; do not expose a
  lifecycle interface. For repository mode, commit the prepared control tree as
  base with message `takt eval base`, create and checkout head, and make no second
  commit. For pull-request mode, commit base, checkout head, apply the patch, and
  commit `takt eval pull request head`. Use the fixed author/committer environment
  from Task 3 for both commits. Create bare remote at the exact **Exact Path Layout** path with
  `git init --bare`, add it as `origin`, and push
  `<base>:refs/heads/<base>` plus `<head>:refs/heads/<head>`. Checkout head and
  assert clean.

  The agent-input `baseline/` is copied only after this history exists. Record
  `BaseCommit`, `HeadCommit`, and `BareRemote` in `PreparedFlowRepeat`; include the
  two commit IDs and a SHA-256 of the bare remote absolute path in the prepared
  case identity. The portable report must not expose the absolute remote path.

- [ ] **Step 5: Run focused tests and commit.**

  ```bash
  go test ./internal/tooling/evaluation -run 'FlowSCM|FlowGitRemote|HeadPatch' -count=1
  git add internal/tooling/evaluation/flow_scm.go internal/tooling/evaluation/flow_scm_test.go internal/tooling/evaluation/flow_git.go internal/tooling/evaluation/flow_git_test.go
  git commit -m "feat: prepare flow evaluation scm fixtures"
  ```

### Task 10: Bundle a stateful fake `gh` and effective assistant environment

**Files:**
- Create: `internal/tooling/evaluation/fixtures/fake-gh`
- Create: `internal/tooling/evaluation/fixtures.go`
- Modify: `internal/tooling/evaluation/flow_scm.go`
- Modify: `internal/tooling/evaluation/flow_scm_test.go`
- Modify: `internal/tooling/evaluation/flow_case.go`
- Delete: `scripts/fixtures/fake-gh`
- Modify: `tests/e2e/external_boundaries_test.go`

**Interfaces:**
- `flow_scm.go` embeds the canonical script with `//go:embed fixtures/fake-gh` and writes it mode `0755` before baseline commit.
- Effective assistant env is applied to every configured assistant in the copied config and uses `{{workspace}}` paths.

- [ ] **Step 1: Write failing fake-`gh` contract tests.**

  Execute the embedded script for the closed supported argv forms:
  `issue view NUMBER [--json ...]` (retained for existing profile E2E),
  `repo view [--json ...]`, `pr view NUMBER [--json ...]`,
  `pr list [--json ...]`, and `pr create` with arbitrary title/body/draft flags. Test unsupported group/
  subcommand, missing fixture/state env, shell-escaped call log, sequential PR
  numbers, and two isolated state dirs. Assert repository, refs, PR number/state,
  and URL come from fixture, never `acme/repo#1`.

- [ ] **Step 2: Run focused tests and confirm red.**

  Run `go test ./internal/tooling/evaluation -run 'FakeGH|AssistantEnvOverlay' -count=1`.

- [ ] **Step 3: Move the canonical fixture and make it stateful.**

  `flow_scm.go` converts the strict YAML fixture with `encoding/json` into
  ready-to-serve JSON before baseline commit. The Bash script neither parses nor
  constructs JSON and requires no `jq`.
  It reads only:

  ```text
  FAKE_GH_FIXTURE_DIR/repo-view.json
  FAKE_GH_FIXTURE_DIR/issue-view.json       # optional; existing non-eval E2E
  FAKE_GH_FIXTURE_DIR/issue-number          # optional
  FAKE_GH_FIXTURE_DIR/pr-view.json
  FAKE_GH_FIXTURE_DIR/pr-list.json
  FAKE_GH_FIXTURE_DIR/pr-number             # pull-request mode
  FAKE_GH_FIXTURE_DIR/pr-url-prefix
  FAKE_GH_STATE_DIR/pr-count
  FAKE_GH_STATE_DIR/calls.log
  ```

  `repo-view.json`, optional `issue-view.json`, and `pr-view.json` are one object
  each; `pr-list.json` is
  a one-element array for an open fixture PR and `[]` for repository-only mode.
  `flow_scm.go` writes decimal `pr-number` and `pr-url-prefix` as
  `https://example.test/<owner>/<repo>/pull/`; the script reads this sixth plain
  file for `pr create`. Existing E2E helper writes decimal `issue-number` and
  `pr-number`, both `1`. `issue view`/`pr view` compare argv NUMBER byte-for-byte
  with the plain file and exit 2 on mismatch; no JSON parsing is needed. Every
  invocation appends one shell-escaped argv line to
  `calls.log`. Unsupported commands print diagnostics to stderr and exit `2`.
  `pr create` increments `pr-count` starting at fixture PR number + 1 (or 1 for
  repository mode) and prints the prefix plus number. The script invokes only
  shell builtins, `mkdir`, and `cat`; it invokes no network client.

- [ ] **Step 4: Commit tools/fixtures before baseline and keep state untracked.**

  Write `.takt/eval/bin/gh` and `.takt/eval/scm-fixture/*` before `git add`; do not
  create `.takt/evals/scm` until process runtime. Clone each `AssistantSpec.Env`
  map before mutation. If a key already exists with a different value, return an
  authoring error instead of overriding it. Add these exact values:

  ```go
  assistant.Env["PATH"] = "{{workspace}}/.takt/eval/bin:" + hostPath
  assistant.Env["FAKE_GH_FIXTURE_DIR"] = "{{workspace}}/.takt/eval/scm-fixture"
  assistant.Env["FAKE_GH_STATE_DIR"] = "{{workspace}}/.takt/evals/scm"
  ```

  Capture host PATH once per suite and include only its SHA-256 in environment identity.

  Serialize the effective `spec.Config` as deterministic indented JSON to
  `.takt/config.yaml` (JSON is valid input for `yamlcodec.Unmarshal`) and reload it
  with `config.Load` before commit. Do not add a YAML marshal helper. Task 10 must
  not infer a project validation command; the mini-du workspace Makefile added in
  Task 13 provides the existing profile tool's `make check` contract.

- [ ] **Step 5: Update existing fake-SCM E2E users and commit.**

  Add `internal/tooling/evaluation/fixtures.go` with one exported test-support
  function `FakeGHFixture() []byte`; it returns a copy of the embedded bytes.
  Update `tests/e2e/external_boundaries_test.go` with one local
  `installFakeGH(t, fakeBin, stateDir) []string`: it writes the returned bytes to
  `<fakeBin>/gh`, writes fixed JSON files under sibling `gh-fixture/` matching the
  old `acme/repo`, issue 1, PR 1 behavior, and returns env containing PATH,
  `FAKE_GH_FIXTURE_DIR`, and `FAKE_GH_STATE_DIR`. Both current callers use that
  helper. Do not expose the embed variable, do not keep two fixture files, and do
  not modify the architecture test: current source inspection does not name this
  path.

  Run:

  ```bash
  go test ./internal/tooling/evaluation -run 'FakeGH|AssistantEnvOverlay' -count=1
  go test ./tests/e2e -run 'ExternalBoundaries|PlanToPR' -count=1
  ```

  Commit:

  ```bash
  git add internal/tooling/evaluation/fixtures/fake-gh internal/tooling/evaluation/fixtures.go internal/tooling/evaluation/flow_scm.go internal/tooling/evaluation/flow_scm_test.go internal/tooling/evaluation/flow_case.go tests/e2e/external_boundaries_test.go
  git rm scripts/fixtures/fake-gh
  git commit -m "feat: add reproducible fake github boundary"
  ```

### Task 11: Prove all three exact production flows with fake assistants

**Files:**
- Modify: `tests/e2e/evaluation_contracts_test.go`
- Modify: `internal/testsupport/cmd/takt-fake-code-agent/main.go`
- Modify: `internal/tooling/evaluation/flow_case_test.go`

**Interfaces:**
- Exact selectors: `code:feature-development`, `code:comprehensive-pr-review`, `code:architect`.
- Production DAG inventory must include `review-acceptance-gate`; changes to selected topology trigger design review.

- [ ] **Step 1: Add failing production-flow E2E fixtures.**

  Build three temporary self-contained suites/cases with one config using an
  absolute `takt-fake-code-agent` argv, models
  `implementation|review|routing`, and
  assistant env `FAKE_FLOW_EVAL_SMOKE=1`. Feature uses GitHub `repository`; review
  and architect use `pull_request`. Validator helper returns valid only when the
  execution workspace exists, `go test ./...` passes, and expected artifacts/SCM
  call log exist. Comprehensive and architect inputs are strict PR JSON;
  comprehensive additionally includes `validation_commands:["go test ./..."]`;
  feature input is Markdown.
  Configure all behavior through copied assistant env; never call
  `os.Setenv` in the E2E process.
  Assert:

  - feature record/events contain completed `implement`, `validate-agent`,
    deterministic `validate`, `create-pr`, `summary`; local origin contains the
    head ref; fake-gh log contains `pr create`; evidence contains `implementation.md`,
    `validation.md`, `pr.md`, `pr-url.txt`, `summary.md`; oracle ran before cleanup;
  - comprehensive record/events contain completed root `review`, root `summary`,
    and expanded `review-acceptance-gate`; oracle target equals the prepared head
    checkout; fake-gh log contains `pr view`;
  - architect observes exactly one waiting approval `approve`, applies `approved`,
    then contains completed `sweep`, `plan`, `implement`, expanded smart-review
    children, and `summary`; oracle sees the retained managed worktree.

- [ ] **Step 2: Add the production DAG inventory assertion.**

  Load the materialized workflow closure with existing `workflow.Load` and assert
  these exact source-level lists, in YAML order:

  ```text
  feature-development.yaml:
    implement, validate-agent, validate, create-pr, summary

  comprehensive-pr-review.yaml:
    review, summary
  review-block.yaml:
    scope, perspectives, reviews, synthesize, fixes, validate,
    post-review-validation-commands, review-acceptance-gate
  review-perspective.yaml:
    review

  architect.yaml:
    sweep, approve, plan, implement, review, summary
  smart-review-block.yaml:
    scope, classify, reviews, synthesize, fixes, validate
  review-perspective.yaml:
    review
  ```

  Walk command front matter plus workflow defaults with the existing workflow/
  command loaders and assert the Task 3 slot table exactly: feature
  `{implementation,review}`, comprehensive `{implementation,review}`, architect
  `{implementation,review,routing}`. Do not use `GetRun` or duplicate compilation.
  A topology/slot mismatch triggers design review, not an automatic table update.

- [ ] **Step 3: Run tests and confirm the missing fixture behaviors.**

  Run `go test ./tests/e2e ./internal/tooling/evaluation -run 'ProductionFlowEvaluation|FlowInventory' -count=1`.

- [ ] **Step 4: Add the exact fake-agent smoke behavior.**

  Leave all existing behavior unchanged unless `FAKE_FLOW_EVAL_SMOKE=1`. In that
  mode add only these phrase handlers to `handleGeneric`:

  ```text
  "Implement the requested change"       -> write smoke.txt and implementation.md
  "Validate the current implementation"  -> run `go test ./...`, write validation.md
  "Create or update a pull request"       -> inspect status, conditionally commit,
                                               push origin HEAD, gh pr create,
                                               write pr.md and pr-url.txt
  "Summarize the completed workflow"      -> write summary.md
  "Perform an architectural sweep"        -> write architecture.md
  "Create a concrete implementation plan" -> write plan.md
  ```

  For the PR handler run `git status --porcelain`; when non-empty run `git add
  --all` and `git commit -m "fixture flow evaluation"`; when empty skip both. Then
  always push and call fake gh. Do not infer behavior from `git commit` error text.

  Resolve workspace only from `TAKT_WORKSPACE`. Generic command prompts contain
  rendered backtick-enclosed absolute artifact paths; add
  `renderedArtifactPath(prompt, baseName)` which requires exactly one
  backtick-enclosed absolute path ending in `/<baseName>`. Do not guess the Run
  artifacts directory from workspace or Run ID. Existing phased commands continue
  to use their `ARTIFACT_PATH:` marker and fake-gh calls. For `review-intake`, add
  a regexp for exact JSON field `"pull_request": <positive integer>` and pass that
  number to `gh pr view` instead of the current hard-coded `1`; fail if the field is
  absent in flow-eval smoke mode. Existing phased review
  handlers are reused unchanged. Every
  output-format node emits its current exact JSON envelope; generic command nodes
  may emit a concise string. Do not modify production workflow or command files.

- [ ] **Step 5: Run Slice 2 checks and commit.**

  ```bash
  gofmt -w internal/testsupport/cmd/takt-fake-code-agent/main.go tests/e2e/evaluation_contracts_test.go internal/tooling/evaluation/flow_case_test.go
  go test ./tests/e2e ./internal/tooling/evaluation -run 'ProductionFlowEvaluation|FlowInventory' -count=1
  go test ./internal/tooling/evaluation -run 'FlowSCM|FakeGH' -count=1
  git add tests/e2e/evaluation_contracts_test.go internal/testsupport/cmd/takt-fake-code-agent/main.go internal/tooling/evaluation/flow_case_test.go
  git commit -m "test: prove production flows through evaluation"
  ```

**Slice 2 review checkpoint:** verify no real network remote is configured, fake `gh` is present in the committed managed-worktree base, mutable SCM state is absent from baseline, and the exact production definitions—not copies—were loaded.

---

## Slice 3 — `mini-du` validator and real corpus

### Task 12: Build and self-test the portable `mini-du` oracle

**Files:**
- Create: `examples/flow-evaluation/mini-du/validator/main.go`
- Create: `examples/flow-evaluation/mini-du/validator/main_test.go`
- Create: `examples/flow-evaluation/mini-du/README.md`

**Interfaces:**
- Validator consumes `takt-evaluation-validator/v1alpha1` JSON from stdin and emits one `takt-validation/v1alpha1` envelope with exit `0` for valid or invalid product.
- Candidate contract: `mini-du [-s] [-k] PATH...`.

- [ ] **Step 1: Write failing validator self-tests.**

  Construct baseline/candidate workspaces and assert: strict request/expectation
  decode; preflight metadata; build failure; delegation source detection; empty,
  nested, multiple, Unicode, space, symlink and hardlink scenarios; `-s`; `-k`;
  missing path; mixed valid/missing paths; exit/stdout/stderr mismatch; forbidden
  changed path; missing artifact/SCM effect; changed committed file; and one passing
  reference candidate. Every product failure must emit one valid envelope and exit
  0; malformed request or unavailable host oracle must write stderr and exit 2.

- [ ] **Step 2: Run focused tests and confirm red.**

  Run `go test ./examples/flow-evaluation/mini-du/validator -count=1`.

- [ ] **Step 3: Implement the standard-library validator.**

  Keep the validator in the root Takt Go module so `go test ./...` runs its tests;
  do not create `examples/flow-evaluation/mini-du/go.mod`. Each case workspace has
  its own nested `go.mod`, which Go excludes from the root package walk. Decode the
  request with `json.Decoder.DisallowUnknownFields`, require the exact protocol,
  and parse `expected.yaml` with `yamlcodec.Unmarshal` into:

  ```go
  type miniDUOracle struct {
      AllowedPaths []string `json:"allowed_paths"`
      Scenarios []string `json:"scenarios"`
      RequiredArtifacts []string `json:"required_artifacts,omitempty"`
      RequirePR bool `json:"require_pr,omitempty"`
      RequirePush bool `json:"require_push,omitempty"`
      ForbiddenIdentifiers []string `json:"forbidden_identifiers,omitempty"`
      ForbiddenPackages []string `json:"forbidden_packages,omitempty"`
  }
  ```

  Reject unknown fields, empty allowed/scenario lists, path escape, and unknown
  scenario names. Supported scenario IDs are exactly:

  ```text
  empty, nested, multiple, unicode, spaces, symlink, hardlink,
  summary, kibibytes, missing, mixed-missing
  ```

  Build `./cmd/mini-du` with `go build -o <temp>/mini-du`; scan every candidate
  `.go` file with `go/parser` and reject imports `os/exec` plus call sites named
  `exec.Command|exec.CommandContext` and string literals equal `du` or ending
  `/du`. Generate each scenario under `os.MkdirTemp`; file contents are fixed
  repeated bytes, symlink target is relative, and hardlink is created with
  `os.Link`. Invoke oracle and candidate with `exec.CommandContext` argv, never a
  shell. Always set `LC_ALL=C`, `LANG=C`, and `BLOCKSIZE=1024`; invoke oracle with
  `-k` plus candidate-requested `-s`; invoke candidate with the scenario's public
  args, including `-k` only for `kibibytes`. In this first portable contract both
  default and `-k` numeric output is KiB rounded like host `du -k`; `-k` is an
  explicit accepted flag, not a different unit. Compare exit code plus normalized
  output lines. Normalize only path separators to `/`, replace each temporary root
  with `<ROOT>`, and sort lines only for the `multiple` scenario. Do not normalize
  numeric block counts.

  Compare baseline/workspace by relative path, content, and executable bit while
  excluding exactly `.git`, `.takt/runs`, `.takt/worktrees`, and `.takt/evals`.
  Match allowed paths with `path.Match` against slash paths; implement `/**` as a
  prefix match because `path.Match` has no recursive glob. Require artifacts by
  basename under `run.artifacts_dir`; require PR when `calls.log` contains one
  shell-escaped `pr create` line; require push by resolving `origin` and comparing
  its head ref to candidate HEAD. Filesystem comparison—not `git diff`—must see
  committed and uncommitted changes.

- [ ] **Step 4: Fingerprint the host oracle.**

  Resolve `du` with `exec.LookPath`, `filepath.EvalSymlinks`, hash file contents,
  and capture the first non-empty line from `du --version`; if that exits non-zero,
  capture the first non-empty line from `du 2>&1` and its exit code. Put exact JSON
  metadata keys `oracle_path`, `oracle_sha256`, and `oracle_signature` in every
  result. Perform this self-check before product checks,
  including when `run.status=not_started`; infrastructure failure exits non-zero.
  The runner's preflight therefore proves the host oracle before the model and
  fingerprints the metadata without a second protocol.

- [ ] **Step 5: Run tests and commit.**

  ```bash
  go test ./examples/flow-evaluation/mini-du/validator -count=1
  git add examples/flow-evaluation/mini-du
  git commit -m "feat: add mini du differential validator"
  ```

### Task 13: Add feature-development `mini-du` cases

**Files:**
- Modify: `.gitignore`
- Create: `examples/flow-evaluation/mini-du/config.pi.example.yaml`
- Create: `examples/flow-evaluation/mini-du/config.opencode.example.yaml`
- Create: `examples/flow-evaluation/mini-du/feature-development/suite.yaml`
- Create in each of `implement-basic`, `implement-multiple-paths`, and
  `implement-symlink-and-hardlink` under
  `examples/flow-evaluation/mini-du/feature-development/cases/<id>/`:
  `input.md`, `expected.yaml`, `scm/repository.yaml`,
  `workspace/go.mod`, `workspace/Makefile`, `workspace/cmd/mini-du/main.go`,
  `workspace/internal/du/du.go`, and `workspace/internal/du/du_test.go`
- Modify: `examples/flow-evaluation/mini-du/validator/main_test.go`

**Interfaces:**
- All three cases use exact selector `code:feature-development`, GitHub fixture `require:repository`, and the Task 12 validator.
- All suites point to `../config.yaml`; add
  `/examples/flow-evaluation/mini-du/config.yaml` to root `.gitignore`. README tells
  users to copy exactly one host-specific example. Do not commit `config.yaml` and
  do not create a config with two active assistants.

- [ ] **Step 1: Add failing corpus-manifest tests.**

  Load the suite/cases with production loaders; assert exact selector, config,
  validator, fixture mode, case IDs, and expected schema. Require every skeleton
  to build but return `ErrNotImplemented`. Copy in one shared test-only reference
  `du.go` and prove all three expectations pass the Task 12 validator without
  weakening their hidden scenarios.

- [ ] **Step 2: Run focused tests and confirm red.**

  Run `go test ./examples/flow-evaluation/mini-du/validator -run FeatureCorpus -count=1`.

- [ ] **Step 3: Add the three minimal skeleton workspaces and expectations.**

  Write both example configs with all three model slots pointing to the same
  user-editable model, exactly one assistant, and max output 10 MiB. Pi uses
  `type: pi`, `project_trust: approve`; OpenCode uses
  `type: opencode`, `args: [--pure]`, `agent: build`, and `auto_approve: true`.

  Each workspace is module `example.test/mini-du`, targets Go `1.23`, contains a
  `Makefile` with exact target `check:\n\tgo test ./...`, has a CLI
  calling `du.Run(args, stdout, stderr)`, and has `internal/du/du.go` returning the
  sentinel `ErrNotImplemented`. `du_test.go` pins existing help/unknown-flag
  behavior but does not contain the hidden differential scenarios. Inputs state
  only the public user contract. Each repository fixture is
  `repository: example/mini-du`, `base_branch: main`, and a unique
  `head_branch: eval/<case-id>`. Expectations use allowed paths
  `cmd/mini-du/**`, `internal/du/**`, `go.mod`, `go.sum`, `Makefile`; require artifacts
  `implementation.md`, `validation.md`, `pr.md`, `pr-url.txt`, `summary.md`;
  `require_pr:true`; `require_push:true`; and scenarios:

  ```text
  implement-basic: empty,nested,summary,missing
  implement-multiple-paths: multiple,unicode,spaces,mixed-missing
  implement-symlink-and-hardlink: symlink,hardlink,kibibytes
  ```

  Suite uses workflow `code:feature-development`, config `../config.yaml`, cases
  `cases`, GitHub `fixture/repository`, validator command
  `[go, run, ../../validator]`, path `../../validator`, timeout `2m`, output budget
  `1048576`, and explicit gates only `validation_error_rate.max: 0` for honest live
  baseline collection.

- [ ] **Step 4: Run deterministic loader/oracle checks and commit.**

  ```bash
  go test ./examples/flow-evaluation/mini-du/validator -run FeatureCorpus -count=1
  go test ./internal/tooling/evaluation -run FlowSuite -count=1
  git add .gitignore examples/flow-evaluation/mini-du
  git commit -m "test: add mini du feature flow corpus"
  ```

### Task 14: Add review and architect `mini-du` cases

**Files:**
- Create: `examples/flow-evaluation/mini-du/comprehensive-pr-review/suite.yaml`
- Create in each of `review-hardlink-bug`, `review-path-with-spaces`, and
  `review-unrelated-change`: `input.md`, `expected.yaml`, `workspace/go.mod`,
  `workspace/Makefile`, `workspace/cmd/mini-du/main.go`, `workspace/internal/du/du.go`,
  `workspace/internal/du/du_test.go`, `scm/repository.yaml`,
  `scm/pull-request.yaml`, and `scm/head.patch`
- Create: `examples/flow-evaluation/mini-du/architect/suite.yaml`
- Create in each of `remove-single-implementation-factories`,
  `collapse-redundant-layers`, and `preserve-behavior-during-simplification`:
  `input.md`, `expected.yaml`, `workspace/go.mod`,
  `workspace/Makefile`, `workspace/cmd/mini-du/main.go`, `workspace/internal/du/du.go`, and
  `workspace/internal/du/du_test.go`, `scm/repository.yaml`,
  `scm/pull-request.yaml`, and `scm/head.patch`
- Create only in `remove-single-implementation-factories`:
  `workspace/internal/factory/factory.go`
- Create only in `collapse-redundant-layers`:
  `workspace/internal/application/service.go`,
  `workspace/internal/domain/usage.go`, and
  `workspace/internal/adapters/fs/adapter.go`
- Create only in `preserve-behavior-during-simplification`:
  `workspace/internal/registry/registry.go` and
  `workspace/internal/strategy/strategy.go`
- Modify: `examples/flow-evaluation/mini-du/validator/main_test.go`
- Modify: `examples/flow-evaluation/mini-du/README.md`

**Interfaces:**
- Review cases seed one known behavioral/scope defect in the PR head.
- Architect head patches create the behaviorally correct overengineered baseline and use baseline/candidate equivalence plus bounded structural checks only.

- [ ] **Step 1: Write failing corpus self-tests.**

  Load both suites and all six cases with production loaders. Prove each review
  agent-input head is invalid for exactly its named diagnostic and becomes valid
  after the expected minimal repair. Prove each architect agent-input head is
  behaviorally valid but violates exactly its bounded smell, while a small
  reference simplification preserves behavior and passes. Assert source
  `workspace/` is the base tree and `baseline_workspace` after SCM preparation is
  the patched head; never validate architect smell against the unpatched base.

- [ ] **Step 2: Run focused tests and confirm red.**

  Run `go test ./examples/flow-evaluation/mini-du/validator -run 'ReviewCorpus|ArchitectCorpus' -count=1`.

- [ ] **Step 3: Add review cases.**

  The review suite uses `code:comprehensive-pr-review`, config `../config.yaml`,
  GitHub `fixture/pull_request`, approval omitted, and the Task 12 validator with
  only `validation_error_rate.max:0`. Repository is `example/mini-du`, base `main`,
  unique head `eval/<case-id>`, PR number `17|18|19`, state `OPEN`, CI `passed`,
  fixes permitted true. `review-hardlink-bug` head patch double-counts inode links;
  `review-path-with-spaces` splits paths incorrectly; `review-unrelated-change`
  includes a working product edit plus `notes/unrelated.txt`. Inputs are strict JSON
  with `repository`, positive `pull_request`, `fixes_permitted:true`, and
  `validation_commands:["go test ./..."]`; they do not name the defect.
  Expectations restrict product paths and require `review-report.md`,
  `validation-report.md`, `post-review-validation-commands.json`, `summary.md`,
  plus the matching differential scenarios. The unrelated case's allowed paths do
  not include `notes/**`, so success requires removing that head change.

  The committed mini-du Makefile supplies `make check` to the profile's shell
  `tools/validate`; `assistant.env` is not visible to deterministic bash nodes.
  The deterministic `script: {runtime: validation}` reads only the raw input field above. If the
  exact production smoke does not execute that command, return to design review
  rather than changing the workflow.

- [ ] **Step 4: Add architect cases.**

  Architect suite uses `code:architect`, config `../config.yaml`,
  `approvals.default: approved`, GitHub `fixture/pull_request`, the Task 12
  validator, and only `validation_error_rate.max:0`. Give PR numbers `27|28|29`.
  Source workspace contains a minimal correct mini-du; head patch adds respectively:
  `factory.NewUsageService` wrapping one implementation; pass-through
  `internal/application`, `internal/domain`, `internal/adapters/fs`; and
  package-global `registry.DefaultStrategy` plus one strategy. The patched head
  must still pass every differential scenario before the flow runs.

  Architect `input.md` is strict JSON with `repository`, positive `pull_request`,
  and `fixes_permitted:true`; it omits `validation_commands` because
  smart-review-block has no `script.runtime: validation` node.

  Expectations list exact forbidden identifiers/packages and allow only the files
  needed to collapse them; require `architecture.md`, `plan.md`,
  `implementation.md`, smart-review artifacts, and `summary.md`. Validator first
  compares behavior to patched baseline, then requires forbidden identifiers or
  packages absent. No line-count, package-count, LLM score, or subjective smell
  heuristic is permitted.

- [ ] **Step 5: Run corpus checks and commit.**

  ```bash
  go test ./examples/flow-evaluation/mini-du/validator -count=1
  git add examples/flow-evaluation/mini-du
  git commit -m "test: add mini du review and architect corpus"
  ```

**Slice 3 manual checkpoint:** build `bin/takt`, copy one host-specific config
example to the ignored local `config.yaml`, then run exactly one available adapter:

```bash
go build -o bin/takt ./cmd/takt
cp examples/flow-evaluation/mini-du/config.opencode.example.yaml examples/flow-evaluation/mini-du/config.yaml
./bin/takt eval flow examples/flow-evaluation/mini-du/feature-development/suite.yaml --case implement-basic --repeat 1
```

Use the Pi example instead only when OpenCode is unavailable. Do not rerun to
improve the result. Record exact binary versions, requested model, output path,
report outcome, and command exit. If binary/credentials/provider are unavailable,
record those exact missing prerequisites as a skip in `TEST_RESULTS.md`; deterministic
commits remain allowed.

---

## Slice 4 — Authoring convenience, docs, and release gate

### Task 15: Add `eval flow init`, publish contracts, and run the release gate

**Files:**
- Create: `internal/tooling/evaluation/flow_init.go`
- Create: `internal/tooling/evaluation/flow_init_test.go`
- Modify: `internal/tooling/services.go`
- Modify: `internal/tooling/services_test.go`
- Modify: `internal/bootstrap/evaluation.go`
- Modify: `internal/cli/eval_cmd.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `tests/e2e/evaluation_contracts_test.go`
- Modify: `docs/03-specification.md`
- Modify: `docs/13-evaluation-plan.md`
- Modify: `docs/05-implementation-status.md`
- Modify: `docs/12-document-map.md`
- Modify: `skills/takt/SKILL.md`
- Create: `skills/takt/references/evaluation.md`
- Modify: `schemas/README.md`
- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `TEST_RESULTS.md`

**Interfaces:**
- CLI: `takt eval flow init <workflow-selector> --output <directory>`.
- Produces only `suite.yaml`, `cases/example/input.md`, `cases/example/expected.yaml`, and `cases/example/workspace/README.md`; it never generates validator logic.

- [ ] **Step 1: Write failing init unit and E2E tests.**

  Assert the four exact generated paths/content, selector byte preservation,
  refusal when output exists (empty or non-empty), no executable validator, suite
  strict-load after user creates `validator` and `config.yaml`, and exact next-step
  message.

- [ ] **Step 2: Run focused tests and confirm red.**

  Run:

  ```bash
  go test ./internal/tooling/evaluation -run FlowInit -count=1
  go test ./internal/cli -run EvalFlowInit -count=1
  go test ./tests/e2e -run FlowEvaluationInit -count=1
  ```

- [ ] **Step 3: Implement the minimal scaffold.**

  Add `InitFlowSuite(workflowSelector, output string) error` in package evaluation;
  add `FlowInit(context.Context, workflowSelector, output string) (any,error)` to
  `EvaluationEngine` and `EvaluationService`, with forwarding coverage in
  `services_test.go`; bootstrap returns
  `map[string]any{"output":absOutput,"created":true}` after calling the helper.
  Service/engine methods are named `FlowInit`. Use `os.Lstat` and fail if output
  exists, then `os.MkdirAll`/`os.WriteFile` with directory `0755`, files `0644`.
  Generated suite is exactly:

  ```yaml
  version: takt-flow-evaluation/v1alpha1
  workflow: <selector>
  config: config.yaml
  cases:
    directory: cases
  validator:
    id: replace-me
    version: "1"
    command: [./validator]
    path: ./validator
    timeout: 2m
    max_output_bytes: 1048576
  gates:
    validation_error_rate: {max: 0}
  ```

  `expected.yaml` is `oracle: {}` plus newline; `input.md` is
  `Describe the task for the selected production workflow.` plus newline;
  workspace README is `Replace this directory with the complete initial repository
  for this case.` plus newline. CLI prints:
  `created <abs-output>; add config.yaml, implement ./validator, and replace the
  example case before running takt eval flow <abs-output>/suite.yaml`.
  Do not generate executable code, inheritance, setup, or teardown.

- [ ] **Step 4: Update the public contract trail.**

  In `docs/03-specification.md` document every suite/request/report field and CLI
  flag/status/error code from Tasks 1/4/5/8. In `docs/13-evaluation-plan.md` document
  denominators, sequential order, evidence path, fake-SCM trust limit, and the
  mini-du corpus. In `README.md` link one minimal user journey. Update document map,
  implementation status, changelog, and schema README with exact new files. Mark
  Slice facts only after focused tests pass. Keep `docs/09-runtime-semantics.md`
  unchanged: this plan adds an application query but no Run semantics. If runtime
  semantics changed, stop for design review.

- [ ] **Step 5: Update the authoring skill references.**

  Read `skills/takt/SKILL.md` fully before editing. Add one routing bullet to the
  main skill: read `references/evaluation.md` only when creating/running/debugging
  eval suites. Put the complete flow-eval authoring journey in that new reference;
  do not make every workflow author read it. Include one self-contained case
  example and the rule that executable validation—not agent text—owns correctness.

- [ ] **Step 6: Run focused docs/schema/CLI tests and commit feature/docs.**

  ```bash
  go test ./internal/tooling/evaluation -run 'FlowInit|FlowSuite|FlowReport' -count=1
  go test ./internal/cli -run EvalFlow -count=1
  go test ./tests/e2e -run FlowEvaluation -count=1
  go test ./internal/schemacontract -count=1
  git diff --check
  ```

  Commit all implementation/docs files with:

  ```bash
  git add .gitignore internal/tooling/evaluation/flow_init.go internal/tooling/evaluation/flow_init_test.go internal/tooling/services.go internal/tooling/services_test.go internal/bootstrap/evaluation.go internal/cli/eval_cmd.go internal/cli/cli_test.go tests/e2e/evaluation_contracts_test.go schemas docs skills examples/flow-evaluation/mini-du/README.md README.md CHANGELOG.md TEST_RESULTS.md
  git commit -m "docs: publish production flow evaluation"
  ```

- [ ] **Step 7: Run the full required verification from a clean diff.**

  ```bash
  gofmt -w cmd internal sdk reference tests
  go test ./... -count=1
  go test -race ./... -count=1
  go vet ./...
  make check
  ./scripts/verify.sh
  git diff --check
  ```

  Expected: all commands exit `0`; TypeScript smoke reports `PASS` when the compiler is installed, otherwise only the repository's existing explicit skip policy is accepted.

- [ ] **Step 8: Record final evidence and commit only the record if changed.**

  Add exact commands/results and live Pi/OpenCode outcome or explicit credentials/provider skip to `TEST_RESULTS.md`. Then run `git status --short` and commit only the evidence update:

  ```bash
  git add TEST_RESULTS.md
  git commit -m "test: record production flow evaluation evidence"
  ```

## Final Review Checklist

- [ ] No second executor, store, scheduler, plugin framework, or generic SCM adapter was added.
- [ ] Exact production selectors and definitions are evaluated.
- [ ] Profile/config/tools are committed before managed-worktree creation.
- [ ] `valid:false` and validator infrastructure errors remain distinct.
- [ ] `false_accept` and `false_reject` denominators match the design.
- [ ] Paused state preserves resumable workspace and is not classified as reject.
- [ ] Cases/repeats are sequential and isolated.
- [ ] Worktree cleanup uses `WorktreeService`; eval-directory cleanup is containment checked.
- [ ] Report/compare remain the canonical existing formats with additive flow fields.
- [ ] Fake GitHub is bundled, state-isolated, network-free itself, and not described as a sandbox.
- [ ] `mini-du` oracle identity distinguishes incompatible host `du` implementations.
- [ ] Real model outcomes are saved honestly or explicitly skipped; deterministic gates never require them.
- [ ] `make check` and `scripts/verify.sh` pass.

## Design Unknowns Audit

**Gate: READY.** Audit rerun against commit `2f0b862` after the plan was made
literal for GPT-5.6-luna. There are no open P0 or P1 items.

| ID | Finding | Resolution locked in this plan |
|---|---|---|
| U-01 | `GetRun` hides compiled nodes, so it cannot provide per-attempt model/usage for exact production DAGs. | Task 8 adds one read-only `EvaluationSnapshot` application query; tooling never receives Store. |
| U-02 | Public `Events` pages at 1000 and events do not contain every execution identity. | Snapshot returns all events with `ReadEvents(...,0)` plus full durable state. |
| U-03 | `WorktreeService.Remove` rejects waiting Runs. | Evidence first; cleanup closure performs cancel → terminal reload → force remove. |
| U-04 | Direct `$ARTIFACTS_DIR` Markdown files are not all registered as `ArtifactRef`. | Task 6 walks the bounded Run artifacts directory and preserves registered provenance when present. |
| U-05 | `profile.PrepareInput` preserves Markdown paths, but review-intake and comprehensive deterministic validation require raw JSON `state.Input`. | Task 3 passes a stable copied path for feature and strict byte-for-byte JSON for comprehensive/architect; Task 8 forwards `InputValue` unchanged. |
| U-06 | `{{workspace}}` is the managed execution worktree. | Task 10 commits fake-gh and immutable SCM fixtures before managed-worktree creation. |
| U-07 | Architect's nested smart review reads a PR. | Architect corpus uses `fixture/pull_request`; no real GitHub fallback or smoke-only production behavior. |
| U-08 | Existing evaluation runtime metrics accept a Store, violating the new application-only path. | Task 7 factors event-slice metrics; old mode keeps its repository adapter. |
| U-09 | Effective config needs an env overlay, but Takt has no YAML marshal helper. | Task 10 writes deterministic indented JSON, which the strict YAML/JSON loader already accepts. |
| U-10 | Production `tools/validate` needs a deterministic project command; assistant env does not reach bash nodes. | Every mini-du workspace contains `Makefile: check -> go test ./...`; the existing profile tool selects it. |
| U-11 | Comprehensive review `script.runtime: validation` requires non-empty `validation_commands` in raw JSON input. | Review corpus inputs include the exact field; Task 3 validates it before callback. |
| U-12 | Existing fake-gh E2E still needs `issue view` and previously supplied no fixture directory. | Task 10 retains that closed command via ready JSON and migrates both callers through one helper/env. |
| U-13 | `profile.Resolve` normally falls back to `~/.takt/profiles`, which makes an eval host-dependent. | Task 3 requires the profile in case control workspace or materializes a built-in before application resolution. |
| U-14 | Comprehensive/architect use governed child Runs; root public/full state alone omits child executions and direct artifacts. | `EvaluationSnapshot` traverses durable descendants; Task 7 aggregates execution records without double-counting root usage; Task 6 copies every run artifact dir. |

### Contract passed to implementation

Follow every accepted decision, assumption, and guard above. Stop implementation
and return to `design-unknowns` if code contradicts the application snapshot
boundary, detached Run lifecycle, `KeepWorktree`, durable `ExecutionWorkspace`,
`{{workspace}}`, or any exact production DAG/slot inventory. Record the observed
fact and source, contradicted decision, affected contract/component, commits already
made, and safe worktree state. A local mechanical deviation is allowed only when
it changes no public contract, data model, trust boundary, failure semantics, or
P1 decision; append it below before continuing.

## Deviation Log

Start empty. Each implementation deviation is one row; never delete prior rows.

| Task | Observed fact and source | Planned instruction | Applied mechanical change | Verification |
|---|---|---|---|---|
