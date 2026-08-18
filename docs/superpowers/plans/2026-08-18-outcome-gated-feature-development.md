# Outcome-gated Feature Development Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `code:feature-development` stop on review failures, perform at most one repair with independent revalidation, reject duplicate PR creation, and evaluate the complete mini-du contract with validator version 3.

**Architecture:** Keep the existing scheduler and workflow language. Review agents write one strict verdict line into durable Markdown; inline deterministic POSIX shell nodes parse it, `when` selects one repair node, and an all-done gate admits only initial `PASS` or `REPAIR -> revalidation PASS`. The hidden mini-du oracle remains outside the flow and gains two missing deterministic scenarios.

**Tech Stack:** Go 1.26, Takt YAML workflows, POSIX shell/awk, JSON Schema Draft 2020-12, Go E2E fixtures.

**Spec:** `docs/superpowers/specs/2026-08-17-outcome-gated-feature-development-design.md`

**Implementation status (2026-08-18):** Tasks 1–4 are implemented and the
focused/full verification gates pass. The live Pi smoke in Task 4 Step 5 was
not run because this checkout has no generated `mini-du/config.yaml` or
evaluation credentials; the deterministic fixtures and saved-baseline bundle
checks remain green.

## Global Constraints

- No new runtime primitive, YAML field, expression operator, dependency, or generic plugin abstraction.
- Verdict artifacts contain exactly one `verdict: PASS|REPAIR|BLOCKED` control line; Markdown outside that line never controls the DAG.
- One repair node only; no retry, resume, or loop for repair/revalidation.
- `create-pr` may allow ordinary assistant exit to reach its result gate, but it has no retry; zero or duplicate fixture `pr create` calls fail.
- Hidden oracle feedback never enters the workflow.
- Product version becomes `0.1.59-alpha`; code profile version becomes `0.19.0`; every mini-du suite using the shared validator becomes validator version `3`.
- Preserve `.takt/` evaluation evidence and all unrelated user changes.

---

### Task 1: Fail-closed hook session contract and product alpha

**Files:**
- Modify: `schemas/workflow.schema.json`
- Modify: `internal/workflow/validate.go`
- Modify: `internal/workflow/validate_test.go`
- Modify: `internal/schemacontract/schema_registry_test.go`
- Modify: `docs/03-specification.md`
- Modify: `skills/takt/references/configuration.md`
- Modify: `skills/takt/references/workflows.md`
- Modify: `skills/takt/references/troubleshooting.md`
- Modify: `VERSION`
- Modify: `internal/version/version.go`

**Interfaces:**
- Consumes: existing `spec.HookDecision{Action string, Session string}`.
- Produces: authoring contract `session in {fresh,resume}` and `session` allowed only with `action: retry`.

- [ ] **Step 1: Add failing Go authoring tests**

Add table-driven coverage to `internal/workflow/validate_test.go`:

```go
func TestValidateHookFailureSession(t *testing.T) {
	tests := []struct {
		name, action, session string
		wantErr               string
	}{
		{"fresh retry", "retry", "fresh", ""},
		{"resume retry", "retry", "resume", ""},
		{"unknown session", "retry", "reuse", "must be fresh or resume"},
		{"session on continue", "continue", "fresh", "requires action retry"},
		{"session on fail", "fail", "resume", "requires action retry"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wf := &spec.Workflow{Name: "hook-session", Nodes: []spec.Node{{
				ID: "node", Bash: "true", Hooks: spec.HooksSpec{AfterNode: []spec.HookSpec{{
					ID: "gate", Bash: "false", OnFailure: spec.HookDecision{Action: tc.action, Session: tc.session},
				}}},
			}}}
			err := Validate(wf)
			if tc.wantErr == "" && err != nil { t.Fatal(err) }
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) { t.Fatalf("err=%v", err) }
		})
	}
}
```

- [ ] **Step 2: Run the authoring test and confirm RED**

Run: `go test ./internal/workflow -run TestValidateHookFailureSession -count=1`

Expected: unknown/invalid placement cases are accepted and the test fails.

- [ ] **Step 3: Add a failing published-schema test**

In `internal/schemacontract/schema_registry_test.go`, compile `workflow.schema.json` and validate minimal workflow documents. Assert `retry+fresh` and `retry+resume` pass; `retry+reuse`, `continue+fresh`, and `fail+resume` fail.

Use the existing `jsonschema/v6` compiler already imported by the package; do not add a schema library.

- [ ] **Step 4: Implement the minimum schema and authoring validation**

Change `hookDecision.session` to:

```json
"session": {"enum": ["fresh", "resume"]}
```

Add the placement constraint inside `hookDecision`:

```json
"allOf": [{
  "if": {"required": ["session"]},
  "then": {
    "required": ["action"],
    "properties": {"action": {"const": "retry"}}
  }
}]
```

Add one shared validation pass over every hook list in `internal/workflow/validate.go`:

```go
if decision.Session != "" {
	if decision.Action != "retry" {
		return fmt.Errorf("node %q hook %q on_failure.session requires action retry", node.ID, hook.ID)
	}
	if decision.Session != "fresh" && decision.Session != "resume" {
		return fmt.Errorf("node %q hook %q on_failure.session must be fresh or resume", node.ID, hook.ID)
	}
}
```

Reuse one local helper for `before_node`, `on_failure`, `after_node`, and `before_complete`; do not duplicate four loops.

- [ ] **Step 5: Document the strict existing field**

Update `docs/03-specification.md` and the three `skills/takt/references/*.md` files to state:

```text
hook.on_failure.session accepts only fresh or resume and is valid only when action is retry.
```

Do not describe a new YAML feature; this is validation of the existing field.

- [ ] **Step 6: Reserve product version `0.1.59-alpha`**

Set both files exactly:

```text
VERSION: 0.1.59-alpha
internal/version/version.go: const Value = "0.1.59-alpha"
```

- [ ] **Step 7: Run focused tests and confirm GREEN**

Run:

```bash
go test ./internal/workflow ./internal/schemacontract ./internal/version -count=1
```

Expected: all packages pass.

- [ ] **Step 8: Commit Task 1**

```bash
git add VERSION internal/version/version.go schemas/workflow.schema.json internal/workflow/validate.go internal/workflow/validate_test.go internal/schemacontract/schema_registry_test.go docs/03-specification.md skills/takt/references/configuration.md skills/takt/references/workflows.md skills/takt/references/troubleshooting.md
git commit -m "fix(workflow): validate hook retry sessions"
```

---

### Task 2: Mini-du validator version 3

**Files:**
- Modify: `examples/flow-evaluation/mini-du/validator/main.go`
- Modify: `examples/flow-evaluation/mini-du/validator/main_test.go`
- Modify: `examples/flow-evaluation/mini-du/validator/feature_corpus_test.go`
- Modify: `examples/flow-evaluation/mini-du/feature-development/cases/implement-basic/expected.yaml`
- Modify: `examples/flow-evaluation/mini-du/feature-development/cases/implement-symlink-and-hardlink/expected.yaml`
- Modify: `examples/flow-evaluation/mini-du/feature-development/suite.yaml`
- Modify: `examples/flow-evaluation/mini-du/review/suite.yaml`
- Modify: `examples/flow-evaluation/mini-du/architect/suite.yaml`
- Modify: `examples/flow-evaluation/mini-du/README.md`
- Modify: `docs/13-evaluation-plan.md`

**Interfaces:**
- Consumes: `miniDUOracle.Scenarios` and `compareScenario`.
- Produces: scenario IDs `hardlink_multiple` and `double_dash_default`; validator identity `3`.

- [ ] **Step 1: Add failing scenario contract tests**

Extend the known-scenario/corpus assertions with:

```go
for _, scenario := range []string{"hardlink_multiple", "double_dash_default"} {
	if !contains(expected.Oracle.Scenarios, scenario) { t.Fatalf("missing %s", scenario) }
}
```

Add focused tests that run `compareScenario` against a deliberately broken candidate implementing per-argument inode tracking and returning no output for bare `--`; both calls must return an error.

- [ ] **Step 2: Run validator tests and confirm RED**

Run: `go test ./examples/flow-evaluation/mini-du/validator -count=1`

Expected: new scenario IDs are unknown or absent.

- [ ] **Step 3: Implement the two oracle scenarios**

Add both IDs to `known` and add cases to `compareScenario`:

```go
case "hardlink_multiple":
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	if err = os.WriteFile(first, bytes.Repeat([]byte("x"), 8192), 0o644); err == nil {
		err = os.Link(first, second)
	}
	args = []string{first, second}
case "double_dash_default":
	return compareCandidateOracle(bin, []string{"--"}, []string{"-k", "--"}, root, root, scenario)
```

Include `hardlink_multiple` in order-normalization alongside `multiple`; BSD/GNU `du` may omit the already-seen second top-level file.

- [ ] **Step 4: Select scenarios and bump all shared validator descriptors**

Add `double_dash_default` to `implement-basic`; add `hardlink_multiple` to `implement-symlink-and-hardlink`. Set `validator.version: "3"` in all three mini-du suites: feature-development, review, architect.

- [ ] **Step 5: Document evaluation generation v2**

Update the mini-du README and `docs/13-evaluation-plan.md`: validator 3 adds the two scenarios; registry-old runs are generation v0, corrected-registry/validator-2 baseline is v1, validator-3 runs are v2, and cross-generation trend comparison is forbidden.

- [ ] **Step 6: Run validator tests and confirm GREEN**

Run:

```bash
go test ./examples/flow-evaluation/mini-du/validator -count=1
```

Expected: all validator and corpus contracts pass.

- [ ] **Step 7: Prove validator 3 rejects the saved baseline patch**

Create a temporary checkout from `.takt/evals/feature-development/20260817T210622.749715000Z/cases/implement-basic/repeat-001/repository.bundle`, run the new focused scenario tests against its `mini-du`, and record the rejection in the test output. Do not modify or delete `.takt/`.

- [ ] **Step 8: Commit Task 2**

```bash
git add examples/flow-evaluation/mini-du docs/13-evaluation-plan.md
git commit -m "test(eval): extend mini-du oracle coverage"
```

---

### Task 3: Bounded review, repair, and revalidation flow

**Files:**
- Create: `internal/profile/builtin/code/commands/feature-validation.md`
- Create: `internal/profile/builtin/code/commands/feature-repair.md`
- Create: `internal/profile/builtin/code/commands/feature-revalidation.md`
- Modify: `internal/profile/builtin/code/workflows/feature-development.yaml`
- Modify: `internal/profile/builtin/code/tools/require-pr`
- Modify: `internal/testsupport/cmd/takt-fake-code-agent/main.go`
- Modify: `tests/e2e/evaluation_contracts_test.go`

**Interfaces:**
- Consumes: artifacts directory, existing `when`, `trigger_rule: all_done`, assistant `allow_failure`, and fixture SCM log.
- Produces: node outputs `initial-verdict.output` and `revalidation-verdict.output`; artifacts `validation.md`, optional `review-fixes.md`, optional `revalidation.md`, `pr.md`, `pr-url.txt`, `summary.md`.

- [ ] **Step 1: Add failing flow inventory and PASS-branch expectations**

Update expected top-level nodes to:

```go
[]string{"implement", "validate-agent", "initial-verdict", "repair", "revalidate-agent", "revalidation-verdict", "review-acceptance-gate", "validate", "create-pr", "pr-effect-gate", "summary"}
```

In the existing production-flow smoke, assert `repair`, `revalidate-agent`, and `revalidation-verdict` are `skipped`, while both verdict/acceptance gates and downstream nodes are `completed`.

- [ ] **Step 2: Add failing verdict branch E2E tests**

Add a table that passes fake-agent env through `writeProductionFlowSuite`:

```go
tests := []struct {
	name, initial, revalidation string
	wantSuccess                 bool
	wantRepair, wantRevalidate  string
}{
	{"pass", "PASS", "", true, "skipped", "skipped"},
	{"blocked", "BLOCKED", "", false, "skipped", "skipped"},
	{"repair then pass", "REPAIR", "PASS", true, "completed", "completed"},
	{"repair remains repair", "REPAIR", "REPAIR", false, "completed", "completed"},
	{"repair becomes blocked", "REPAIR", "BLOCKED", false, "completed", "completed"},
}
```

Assert every failing branch skips `create-pr`.

- [ ] **Step 3: Add failing parser and terminal-failure tests**

Cover `missing`, `unknown`, `malformed`, and `duplicate` validation verdict artifacts using `FAKE_FEATURE_VERDICT_KIND`. Add `FAKE_FAIL_PHASE=feature-validation` and assert `initial-verdict` plus `review-acceptance-gate` fail, repair/revalidation skip, and the run cannot complete.

- [ ] **Step 4: Add failing PR effect tests**

Add cases:

```go
FAKE_DUPLICATE_PR_CREATE=1 // run fails at pr-effect-gate
FAKE_EXIT_AFTER_PR_CREATE=1 // one create + artifacts; run completes via allow_failure and gate
```

Assert the fixture call log contains exactly two and one `pr create` lines respectively.

In the same focused suite, drive `missing`, `empty`, and `directory` artifacts for
`pr.md`, `pr-url.txt`, and `summary.md`. Assert the owning gate fails and no later
node completes. Existing implementation-artifact coverage remains unchanged;
validation/revalidation artifact variants are covered by Step 3.

- [ ] **Step 5: Run focused E2E tests and confirm RED**

Run:

```bash
go test ./tests/e2e -run 'TestProductionFlowEvaluation|TestFlowInventory' -count=1
```

Expected: missing nodes/contracts and unhandled fake env make the new tests fail.

- [ ] **Step 6: Add the three concrete command prompts**

Each command must include `TAKT_PHASE`, `ARTIFACT_PATH`, current workspace boundary, original request, and exact output artifact. Validation/revalidation require exactly one line:

```text
verdict: PASS|REPAIR|BLOCKED
```

`feature-repair` reads `validation.md`, re-checks every finding, changes only current scope, adds focused tests, runs checks, and writes `review-fixes.md`. It must not claim a second repair opportunity.

- [ ] **Step 7: Implement the workflow topology**

Use the same inline parser body for both verdict nodes:

```sh
artifact="$ARTIFACTS_DIR/validation.md"
if [ ! -f "$artifact" ] || [ -L "$artifact" ] || [ ! -s "$artifact" ]; then
  printf '%s\n' 'validation verdict artifact missing' >&2
  exit 1
fi
exec awk '
  /^verdict:/ { declared++ }
  /^verdict: (PASS|REPAIR|BLOCKED)$/ { valid++; value=$2 }
  END {
    if (declared != 1 || valid != 1) {
      print "invalid validation verdict" > "/dev/stderr"
      exit 1
    }
    printf "%s", value
  }
' "$artifact"
```

Use `revalidation.md` in the second parser. Set both parsers and `review-acceptance-gate` to `trigger_rule: all_done`. Set repair condition exactly to `$initial-verdict.output == "REPAIR"`; do not add attempts or hooks to repair.

The acceptance gate succeeds only for:

```sh
[ $initial-verdict.output? = "PASS" ] || \
{ [ $initial-verdict.output? = "REPAIR" ] && [ $revalidation-verdict.output? = "PASS" ] && [ -s "$ARTIFACTS_DIR/review-fixes.md" ] && [ -s "$ARTIFACTS_DIR/revalidation.md" ]; }
```

- [ ] **Step 8: Make PR and summary result gates fail-closed**

Set `create-pr.allow_failure: true` with no retry. In `pr-effect-gate`, require non-empty regular non-symlink `pr.md` and `pr-url.txt`, then execute `require-pr`. Modify `require-pr` fixture logic to count exact `pr create` records and require count `1`; ordinary production workspace remains a no-op.

Add an `after_node` gate on summary requiring non-empty regular non-symlink `summary.md`.

- [ ] **Step 9: Extend the fake coding agent**

Support phases `feature-validation`, `feature-repair`, and `feature-revalidation`. Default verdicts to `PASS`; render missing/unknown/malformed/duplicate artifacts from `FAKE_FEATURE_VERDICT_KIND`; fail the requested phase from `FAKE_FAIL_PHASE`; issue a second fake `gh pr create` only for `FAKE_DUPLICATE_PR_CREATE`; write artifacts and exit non-zero after one create only for `FAKE_EXIT_AFTER_PR_CREATE`.

Add one generic `FAKE_FLOW_ARTIFACT_KIND=<name>:<missing|empty|directory>` switch
for PR and summary artifact tests; keep the existing
`FAKE_IMPLEMENTATION_ARTIFACT_KIND` compatibility path.

- [ ] **Step 10: Run focused E2E tests and confirm GREEN**

Run:

```bash
go test ./tests/e2e -run 'TestProductionFlowEvaluation|TestFlowInventory' -count=1
```

Expected: PASS/repair branches and every fail-closed branch match the table.

- [ ] **Step 11: Commit Task 3**

```bash
git add internal/profile/builtin/code/commands internal/profile/builtin/code/workflows/feature-development.yaml internal/profile/builtin/code/tools/require-pr internal/testsupport/cmd/takt-fake-code-agent/main.go tests/e2e/evaluation_contracts_test.go
git commit -m "fix(profile): gate feature delivery on review verdict"
```

---

### Task 4: Profile release contract, documentation, and verification

**Files:**
- Modify: `internal/profile/builtin/code/VERSION`
- Modify: `internal/profile/builtin/code/README.md`
- Modify: `tests/e2e/core_contracts_test.go`
- Modify: `README.md`
- Modify: `docs/05-implementation-status.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: completed schema, validator, and profile changes.
- Produces: product `0.1.59-alpha`, profile `0.19.0`, documented compatibility and release evidence.

- [ ] **Step 1: Update failing version/profile contracts first**

Change the expected profile version in `tests/e2e/core_contracts_test.go` to `0.19.0` and extend the feature profile assertions with the strict verdict, single repair, revalidation gate, and exactly-once PR gate node inventory. Run the focused test before changing the profile version and confirm it fails.

Run: `go test ./tests/e2e -run TestCodeProfileCatalogContract -count=1`

- [ ] **Step 2: Bump profile and documentation**

Set `internal/profile/builtin/code/VERSION` to `0.19.0`. Update profile README, repository README, implementation status, and changelog with:

- review verdict/one-repair/revalidation semantics;
- eval-only exactly-one PR effect;
- mini-du validator 3 and measurement generation v2;
- hook session validation;
- product/profile versions `0.1.59-alpha`/`0.19.0`.

- [ ] **Step 3: Run focused contract suites**

Run:

```bash
gofmt -w cmd internal sdk reference tests
go test ./internal/workflow ./internal/schemacontract ./internal/profile ./internal/tooling/compatibility ./examples/flow-evaluation/mini-du/validator ./tests/e2e -count=1
```

Expected: all pass.

- [ ] **Step 4: Run full required gates**

Run separately and require exit 0 for each:

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
make check
./scripts/verify.sh
```

- [ ] **Step 5: Run live Pi smoke generation v2**

Run:

```bash
EVAL_PRESET=qwen38 make eval-feature-smoke
```

Inspect `report.json`, node executions, verdict artifacts, `scm/calls.log`, and validator result. Accept only validator version 3 plus either initial `PASS` or exactly one repair followed by revalidation `PASS`; require exactly one `pr create`.

- [ ] **Step 6: Commit Task 4**

```bash
git add internal/profile/builtin/code/VERSION internal/profile/builtin/code/README.md tests/e2e/core_contracts_test.go README.md docs/05-implementation-status.md CHANGELOG.md
git commit -m "chore: release outcome-gated feature flow"
```

- [ ] **Step 7: Final repository check**

Run:

```bash
git diff --check HEAD~4..HEAD
git status --short --branch
```

Expected: no tracked changes remain; `.takt/` remains untracked and untouched.
