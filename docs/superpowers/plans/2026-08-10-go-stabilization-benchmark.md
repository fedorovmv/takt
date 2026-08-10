# Go Stabilization Benchmark Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add and run a five-case production-shaped Go benchmark comparing direct and deterministic-repair strategies on Pi and OpenCode with Qwen 3.6 27B.

**Architecture:** Reuse `takt eval benchmark` unchanged. A nested stdlib-only Go module contains five intentionally failing packages; an external Go validator selects the package declared by the case, rejects out-of-scope/test changes, then emits the existing `takt-validation/v1alpha1` envelope after gofmt/test/race/vet. Static workflow matrices run the same direct and repair strategies separately for Pi and OpenCode.

**Tech Stack:** Go 1.23+, Takt `takt/evaluation/v1alpha1`, Bash only as the opt-in live launcher, Pi 0.83.0, OpenCode 1.18.14, Qwen 3.6 27B.

## Global Constraints

- Do not add a new runner, scheduler, public YAML/JSON field, dependency, or `scripts/test-*.sh`.
- Keep the corpus labelled `production-shaped`; it is not anonymized production data.
- Pi model is `aihub/Qwen/Qwen3.6-27B`; OpenCode model is `aihub-sbt/Qwen/Qwen3.6-27B`.
- OpenCode uses only `opencode run --format json`; its provider-side routing is not claimed observable beyond the requested CLI model.
- Text from the assistant is never success evidence; success is only `quality_node_status=completed && valid=true`.
- Run `repeat=1` before `repeat=3`; do not add cost gates before the first full baseline.
- Any Takt product fix starts with a Go regression test at the shared root cause. Do not refactor unrelated code.
- Never persist credentials, raw provider configuration, or session artifacts.

---

## File Map

**Create under `examples/go-benchmark/`:**

- `README.md` — scope, commands and evidence limits.
- `cases.yaml`, `cases/*.md` — five tasks and labels.
- `workspace/go.mod` — nested module `takt-go-benchmark`, Go 1.23, no dependencies.
- `workspace/internal/{cliargs,opencodeevents,session,terminal,runstore}/*` — intentionally faulty implementation plus deterministic tests.
- `validator/main.go`, `validator/main_test.go` — immutable subject validator and self-check.
- `strategies/baseline-direct.yaml`, `strategies/feedback-repair.yaml` — compared workflows.
- `config.pi.yaml`, `config.opencode.yaml` — exact host/model configs.
- `matrix.pi.yaml`, `matrix.opencode.yaml` — host-isolated matrices.
- `run.sh` — build validator and invoke the existing benchmark command.

**Modify:**

- `.gitignore` — ignore `examples/**/.takt/` live output.
- `docs/05-implementation-status.md`, `docs/13-evaluation-plan.md`, `TEST_RESULTS.md`, `CHANGELOG.md` — record only evidence actually obtained.
- `MANIFEST.sha256` — include the committed corpus, validator, workflows and docs.

No production Go package is scheduled for modification. Such a change is added only after a live run reproduces a Takt defect.

---

### Task 1: Add the five-case standalone Go corpus

**Files:**

- Create: `examples/go-benchmark/workspace/go.mod`
- Create: `examples/go-benchmark/workspace/internal/cliargs/args.go`
- Create: `examples/go-benchmark/workspace/internal/cliargs/args_test.go`
- Create: `examples/go-benchmark/workspace/internal/opencodeevents/events.go`
- Create: `examples/go-benchmark/workspace/internal/opencodeevents/events_test.go`
- Create: `examples/go-benchmark/workspace/internal/session/resume.go`
- Create: `examples/go-benchmark/workspace/internal/session/resume_test.go`
- Create: `examples/go-benchmark/workspace/internal/terminal/classify.go`
- Create: `examples/go-benchmark/workspace/internal/terminal/classify_test.go`
- Create: `examples/go-benchmark/workspace/internal/runstore/store.go`
- Create: `examples/go-benchmark/workspace/internal/runstore/service.go`
- Create: `examples/go-benchmark/workspace/internal/runstore/service_test.go`
- Create: `examples/go-benchmark/cases/01-cli-separator.md`
- Create: `examples/go-benchmark/cases/02-opencode-events.md`
- Create: `examples/go-benchmark/cases/03-exact-resume.md`
- Create: `examples/go-benchmark/cases/04-terminal-precedence.md`
- Create: `examples/go-benchmark/cases/05-persistence-error.md`
- Create: `examples/go-benchmark/cases.yaml`

**Interfaces:**

- Produces: five packages selected by the exact headers `Benchmark-Package: ./internal/<name>`.
- Produces: one failing test contour per package; the root Takt module must remain green because `workspace/go.mod` is a nested module.

- [ ] **Step 1: Create the nested module and intentionally faulty production functions**

Use these exact public seams:

```go
// cliargs
func Inject(args, managed []string) []string

// opencodeevents
type Usage struct { InputTokens, OutputTokens int }
func Summarize(r io.Reader) (Usage, error)

// session
func Resolve(requested string, observed []string) (sessionID string, resumed bool, err error)

// terminal
type Kind string
const (KindExit Kind = "exit"; KindOverflow Kind = "overflow"; KindTimedOut Kind = "timed_out"; KindCancelled Kind = "cancelled")
func Classify(ctxErr error, exitCode int, overflow bool) Kind

// runstore
type State struct { Status string }
type Repository interface { Commit(*State) error }
func Complete(repo Repository, state *State) error
```

Seed one root defect in each implementation:

- `Inject` appends managed flags after the whole argv instead of inserting before `--`;
- `Summarize` sums duplicate `step_finish.part.id` and ignores an `error` event;
- `Resolve` accepts a resumed Session ID different from `requested`;
- `Classify` returns overflow before checking deadline/cancellation;
- `Complete` ignores `Repository.Commit` errors.

- [ ] **Step 2: Add focused tests that express the correct contracts**

The tests must include these assertions:

```go
wantArgs := []string{"host", "begin", "--workspace", "/tmp/work", "--json", "--", "fix bug"}
// Input slices are unchanged.

// Two identical step_finish parts with id "step-1" count once.
// Any error event returns an error even when valid usage follows it.

// requested="ses-123", observed=["ses-new"] returns an error.
// requested="ses-123", observed=["ses-123"] returns resumed=true.

// context.DeadlineExceeded + overflow => timed_out.
// context.Canceled + overflow => cancelled.

// errors.New("disk full") from Commit is returned by Complete unchanged.
```

Use realistic OpenCode records with top-level `type`, `sessionID`, nested `part.id`, `part.type=step-finish`, and `part.tokens.input/output`.

- [ ] **Step 3: Add the five Markdown cases and manifest labels**

Each case starts with exactly one allowlisted header, for example:

```markdown
Benchmark-Package: ./internal/cliargs

Исправь обработку служебных аргументов. Флаги Takt должны добавляться до `--`, а пользовательская часть после `--` должна сохраниться без изменений.

Не изменяй `go.mod`, `*_test.go` и соседние packages. Выполни `go test -count=1 ./internal/cliargs`.
```

`cases.yaml` uses `source: production-shaped` for every case and categories `cli`, `protocol`, `session`, `lifecycle`, `persistence`.

- [ ] **Step 4: Verify every seeded package fails for its intended reason**

Run from `examples/go-benchmark/workspace`:

```bash
for pkg in ./internal/cliargs ./internal/opencodeevents ./internal/session ./internal/terminal ./internal/runstore; do
  if go test -count=1 "$pkg"; then
    echo "unexpected PASS: $pkg" >&2
    exit 1
  fi
done
```

Expected: all five commands fail on their package contract, not on compilation or missing dependencies.

- [ ] **Step 5: Verify the main repository still passes its existing package discovery**

Run: `go test ./... -count=1`

Expected: PASS; the nested intentionally failing module is not part of the root module.

- [ ] **Step 6: Commit the corpus**

```bash
git add examples/go-benchmark/cases.yaml examples/go-benchmark/cases examples/go-benchmark/workspace
git commit -m "test: add production-shaped Go benchmark corpus"
```

---

### Task 2: Build the deterministic external Go validator with TDD

**Files:**

- Create: `examples/go-benchmark/validator/main_test.go`
- Create: `examples/go-benchmark/validator/main.go`

**Interfaces:**

- Consumes: `--case-file`, `--baseline`, `--workspace`.
- Produces: one strict `takt-validation/v1alpha1` JSON object on stdout and exit `0` only for `valid=true`.

- [ ] **Step 1: Write validator tests first**

Define a testable seam:

```go
type options struct { caseFile, baseline, workspace string }
func validate(ctx context.Context, opts options) validationResult
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int
```

Tests must:

1. copy the baseline workspace to a temp directory;
2. prove each untouched case is invalid;
3. replace the one faulty production file with a correct implementation and prove each of the five cases is valid;
4. modify a target `*_test.go` and prove scope validation fails before Go checks;
5. modify a neighboring package and prove scope validation fails;
6. decode stdout and assert exactly one JSON value with the required protocol/type/valid/checks/diagnostics fields.

- [ ] **Step 2: Run the tests and observe the expected compile failure**

Run: `go test ./examples/go-benchmark/validator -count=1`

Expected: FAIL because `validate`, `run`, and the result types do not exist.

- [ ] **Step 3: Implement only the validator needed by the tests**

Use stdlib packages only. The implementation must:

```go
var allowedPackages = map[string]struct{}{
    "./internal/cliargs": {},
    "./internal/opencodeevents": {},
    "./internal/session": {},
    "./internal/terminal": {},
    "./internal/runstore": {},
}
```

- parse exactly one `Benchmark-Package:` line;
- compare baseline and workspace recursively, ignoring `.takt`, and allow changes only to non-test `.go` files inside the selected package;
- require at least one allowed production change;
- run `gofmt -l` on selected package `.go` files;
- run `go test -count=1 <package>`, `go test -race -count=1 <package>`, and `go vet <package>` with `cmd.Dir=workspace`;
- capture command output into bounded diagnostics rather than leaking it before the JSON envelope;
- calculate `valid` from all checks and emit codes `SCOPE_INVALID`, `GOFMT_FAILED`, `GO_TEST_FAILED`, `GO_RACE_FAILED`, or `GO_VET_FAILED`;
- return exit `1` for a validly encoded negative result and exit `2` only for validator usage/internal failure.

- [ ] **Step 4: Run validator tests and static analysis**

Run:

```bash
gofmt -w examples/go-benchmark/validator
go test ./examples/go-benchmark/validator -count=1
go test -race ./examples/go-benchmark/validator -count=1
go vet ./examples/go-benchmark/validator
```

Expected: PASS.

- [ ] **Step 5: Commit the validator**

```bash
git add examples/go-benchmark/validator
git commit -m "feat: add deterministic Go benchmark validator"
```

---

### Task 3: Add direct/repair workflows, exact host configs and launcher

**Files:**

- Create: `examples/go-benchmark/strategies/baseline-direct.yaml`
- Create: `examples/go-benchmark/strategies/feedback-repair.yaml`
- Create: `examples/go-benchmark/config.pi.yaml`
- Create: `examples/go-benchmark/config.opencode.yaml`
- Create: `examples/go-benchmark/matrix.pi.yaml`
- Create: `examples/go-benchmark/matrix.opencode.yaml`
- Create: `examples/go-benchmark/run.sh`
- Modify: `.gitignore`

**Interfaces:**

- Consumes env: `TAKT_GO_BENCHMARK_VALIDATOR`, `TAKT_GO_BENCHMARK_BASELINE`, `TAKT_BENCH_HOST`, `TAKT_REPEAT`, `TAKT_BENCH_OUTPUT`.
- Produces: `examples/go-benchmark/.takt/evals/<host>/benchmark.json` and immutable strategy reports.

- [ ] **Step 1: Add the two workflows**

Both workflows use `assistant: coding-agent`, `model: go-model`, generation node `implement`, and quality node `full-validation`.

The validation payload in both the final node and repair hook is exactly:

```bash
mkdir -p .takt
cat > .takt/evaluation-case.md <<'TAKT_GO_CASE'
${input}
TAKT_GO_CASE
"$TAKT_GO_BENCHMARK_VALIDATOR" \
  --case-file .takt/evaluation-case.md \
  --baseline "$TAKT_GO_BENCHMARK_BASELINE" \
  --workspace .
```

`baseline-direct` uses `session: fresh`, one attempt, and no hook. `feedback-repair` uses `session: resume`, `attempts.max: 3`, includes `${feedback}` in the prompt, and retries on validator hook failure with `session: resume`.

- [ ] **Step 2: Add exact Pi and OpenCode configs**

Pi config:

```yaml
default_assistant: pi
models:
  go-model: {provider: aihub, id: Qwen/Qwen3.6-27B}
assistants:
  pi:
    type: pi
    binary: pi
    session_dir: .takt/pi-sessions
    project_trust: approve
    env: {NODE_OPTIONS: --use-system-ca}
    max_output_bytes: 10485760
```

OpenCode config:

```yaml
default_assistant: opencode
models:
  go-model: {provider: aihub-sbt, id: Qwen/Qwen3.6-27B}
assistants:
  opencode:
    type: opencode
    binary: opencode
    agent: build
    auto_approve: true
    max_output_bytes: 10485760
```

Both full files include `apiVersion: takt/v1alpha1` and `kind: Config`.

- [ ] **Step 3: Add one matrix per host**

Each matrix uses:

```yaml
benchmark:
  id: go-production-shaped-5-v1
  baseline_strategy: baseline-direct
  cases: cases
  case_manifest: cases.yaml
  workspace_template: workspace
  repeat: 3
  quality_node: full-validation
  generation_node: implement
  validator:
    id: takt-go-benchmark-validator
    version: "1"
    path: validator
```

Define only `baseline-direct` and `feedback-repair`; point both strategies at the host-specific config. Do not add gates before baseline evidence.

- [ ] **Step 4: Add the opt-in launcher and ignore output**

`run.sh` must:

- require an existing executable `bin/takt`;
- build `examples/go-benchmark/validator` into `mktemp -d`;
- export its path and the canonical baseline workspace path;
- accept `TAKT_BENCH_HOST=pi|opencode|all` with `all` as default;
- accept `TAKT_REPEAT` with `1` as the safe default;
- call `bin/takt eval benchmark <matrix> --repeat "$TAKT_REPEAT" --output <host-dir> --replace --json`;
- remove only its own `mktemp` directory on exit.

Add `examples/**/.takt/` to `.gitignore`.

- [ ] **Step 5: Validate static definitions without live model calls**

Run:

```bash
go build -o bin/takt ./cmd/takt
bin/takt validate examples/go-benchmark/strategies/baseline-direct.yaml --config examples/go-benchmark/config.pi.yaml --workspace examples/go-benchmark/workspace --json
bin/takt validate examples/go-benchmark/strategies/feedback-repair.yaml --config examples/go-benchmark/config.pi.yaml --workspace examples/go-benchmark/workspace --json
bin/takt validate examples/go-benchmark/strategies/baseline-direct.yaml --config examples/go-benchmark/config.opencode.yaml --workspace examples/go-benchmark/workspace --json
bin/takt validate examples/go-benchmark/strategies/feedback-repair.yaml --config examples/go-benchmark/config.opencode.yaml --workspace examples/go-benchmark/workspace --json
go test ./internal/tooling/evaluation ./examples/go-benchmark/validator -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the benchmark wiring**

```bash
git add .gitignore examples/go-benchmark/strategies examples/go-benchmark/config.*.yaml examples/go-benchmark/matrix.*.yaml examples/go-benchmark/run.sh
git commit -m "feat: wire Go strategy benchmark"
```

---

### Task 4: Document the benchmark and run one-repeat live smoke

**Files:**

- Create: `examples/go-benchmark/README.md`
- Modify after evidence: `TEST_RESULTS.md`

- [ ] **Step 1: Document requirements, commands and evidence limits**

State explicitly that the corpus is production-shaped, direct and repair are compared within each host, `auto_approve` is limited to the trusted copied fixture, outputs are local, and OpenCode resolved provider routing is not observable unless its event stream exposes it.

- [ ] **Step 2: Verify installed host versions and clean environment**

Run:

```bash
pi --version
opencode --version
git status --short
```

Expected: Pi `0.83.0`, OpenCode `1.18.14`, no uncommitted files outside this task.

- [ ] **Step 3: Run Pi smoke**

Run: `TAKT_BENCH_HOST=pi TAKT_REPEAT=1 ./examples/go-benchmark/run.sh`

Expected: matrix report is written even when individual model outcomes are invalid. Any matrix infrastructure error is investigated before continuing.

- [ ] **Step 4: Run OpenCode smoke**

Run: `TAKT_BENCH_HOST=opencode TAKT_REPEAT=1 ./examples/go-benchmark/run.sh`

Expected: same measurement contract as Pi; requested model is `aihub-sbt/Qwen/Qwen3.6-27B`.

- [ ] **Step 5: Classify smoke failures without broad fixes**

- Invalid code or exhausted repair attempts: benchmark outcome; do not change Takt.
- Missing credentials/model/CLI: external blocker; preserve no secret-bearing raw output.
- Wrong workspace, lost feedback/resume, malformed report or misattributed usage: Takt defect; add a focused Go regression test before the minimal shared fix, then rerun only the affected smoke.

- [ ] **Step 6: Record exact smoke evidence and commit docs**

Add versions, requested models, matrix status, infrastructure failures and explicit limitations to `TEST_RESULTS.md`. Do not claim quality statistics from a run that did not write a valid report.

```bash
git add examples/go-benchmark/README.md TEST_RESULTS.md
git commit -m "docs: record Go benchmark smoke"
```

---

### Task 5: Run the three-repeat matrices and triage reproducible Takt defects

**Files:**

- Local only: `examples/go-benchmark/.takt/evals/pi/**`
- Local only: `examples/go-benchmark/.takt/evals/opencode/**`
- Conditional product test/fix: exact shared package identified by a reproduced defect
- Modify after evidence: `TEST_RESULTS.md`, `docs/05-implementation-status.md`, `docs/13-evaluation-plan.md`, `CHANGELOG.md`

- [ ] **Step 1: Run Pi full matrix**

Run: `TAKT_BENCH_HOST=pi TAKT_REPEAT=3 ./examples/go-benchmark/run.sh`

- [ ] **Step 2: Run OpenCode full matrix**

Run: `TAKT_BENCH_HOST=opencode TAKT_REPEAT=3 ./examples/go-benchmark/run.sh`

- [ ] **Step 3: Inspect reports using the existing report data**

Record per host and strategy:

- `success_at_1`, `final_success_rate`, `average_attempts_to_valid`;
- `average_time_to_valid_ms`, input/output tokens and cost when available;
- stable valid/invalid and unstable cases;
- retry/resume counts, execution identity and router/provider limitations;
- diagnostic fingerprints for invalid results.

- [ ] **Step 4: Apply the defect protocol only when evidence points to Takt**

For each candidate defect:

1. reproduce it twice outside the aggregate matrix using the saved case/workspace;
2. identify every caller of the shared function with `rg`;
3. add one failing Go regression test at the shared root cause;
4. run the focused test and confirm the expected failure;
5. implement the smallest fix;
6. run focused test, package race test, and affected live case;
7. commit test+fix separately from benchmark documentation.

If a fact requires a public contract, durable semantics or component-boundary change, stop and return to the design gate instead of patching it silently.

- [ ] **Step 5: Record factual evidence**

Update status/evaluation docs and changelog with actual corpus size, host/model identities, metrics, limitations and any fixes. Keep the synthetic/production distinction explicit.

---

### Task 6: Final verification, manifest and completion commit

**Files:**

- Modify: `MANIFEST.sha256`
- Modify if needed for final factual state: `TEST_RESULTS.md`, `docs/05-implementation-status.md`, `docs/13-evaluation-plan.md`, `CHANGELOG.md`

- [ ] **Step 1: Format all changed Go files**

Run: `gofmt -w cmd internal sdk reference tests examples/go-benchmark/validator examples/go-benchmark/workspace`

- [ ] **Step 2: Run minimum project gates**

Run:

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
./scripts/check-docs.sh
```

Expected: PASS.

- [ ] **Step 3: Update `MANIFEST.sha256` mechanically and verify it**

Include every committed file except `MANIFEST.sha256`, `.git/**`, `bin/**`, ignored `.takt/**` and editor backup files. Then run `./scripts/verify-manifest.sh`.

- [ ] **Step 4: Run full release gates**

Run:

```bash
make check
./scripts/verify.sh
```

Expected: PASS. Live Pi/OpenCode runs remain separately reported evidence and are not moved into the deterministic release gate.

- [ ] **Step 5: Review the final diff for secrets and scope drift**

Run:

```bash
git status --short
git diff --check
git diff --stat HEAD~1
rg -n "(api[_-]?key|token|secret|session[_-]?id)" examples/go-benchmark TEST_RESULTS.md
```

Inspect matches; only schema/field names and redacted explanatory text may remain.

- [ ] **Step 6: Commit final evidence and manifest**

```bash
git add MANIFEST.sha256 CHANGELOG.md TEST_RESULTS.md docs/05-implementation-status.md docs/13-evaluation-plan.md examples/go-benchmark .gitignore
git commit -m "test: record Go stabilization evidence"
```

- [ ] **Step 7: Verify clean handoff**

Run: `git status --short --branch`

Expected: clean `stabilize/live-host-conformance` branch. Do not merge or push without a separate user request.
