# Deterministic Development Flow Acceptance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `code:plan-to-pr` accept a run only when its required phases, artifacts, deterministic validation, Git scope, review result, draft PR result, and final summary are all proven by persisted evidence.

**Architecture:** Keep the existing scheduler, validation runtime, profile workflows, worktree isolation, fake coding agent, and fake SCM. Add one closed-world profile Go script for native Git pathspec scope checking and workflow-local terminal gates; the generic runtime remains unaware of profile-specific status/code values.

**Tech Stack:** Go standard library, installed Git CLI, Takt v1alpha1 YAML workflows, Go unit/contract/E2E tests, existing fake `gh` and coding-agent fixtures.

## Global Constraints

- Use native non-magic Git pathspecs; do not reuse `rolecontract.PathScope` and do not add a runtime action.
- `allowed_paths` is required, non-empty, unique, repository-relative input; incomplete router input must not infer scope or create side effects.
- Git changes are read NUL-delimited with `git diff --name-only -z --no-renames <base> --` and `git ls-files --others --exclude-standard -z`; `$ARTIFACTS_DIR` is outside the worktree and is not drift.
- Scope failures, missing evidence, review failures, PR domain failures, and incomplete summaries fail closed through deterministic workflow gates.
- Product correctness belongs in Go tests and `tests/e2e`; no new shell test or generic plugin/framework abstraction.
- Update affected specification/status/profile docs, changelog, and `TEST_RESULTS.md`; leave historical release documents unchanged.

---

### Task 1: Add the deterministic Git scope checker

**Files:**
- Create: `internal/profile/builtin/code/tools/scope-check.go`
- Create: `internal/profile/scope_check_test.go`

**Interfaces:**
- Consumes: rendered JSON argv `{"base_branch":"main","allowed_paths":["app.txt","docs/**"]}`, an artifact output-path argv, and the current Takt worktree.
- Produces: JSON `{"status":"ready|failed","base_commit":"...","changed_files":[],"outside_allowed":[]}`; exit `0` for ready, `3` for scope drift, `2` for invalid input or Git error.
- Internal functions: `execute(args []string) (report, error)`, `validatePathspec(string) error`, `gitOutput(dir string, args ...string) ([]byte,error)`, `changedPaths`, `splitNUL`, and `difference`.

- [x] **Step 1: Write failing tests for pathspec and Git-state behavior.**

  Build a temporary Git repository with a `main` base commit and exercise the tool through `go run internal/profile/builtin/code/tools/scope-check.go`, asserting:

  - modified `app.txt` is ready for `allowed_paths:["app.txt"]`;
  - an untracked filename containing a space appears in `changed_files` and is outside when not allowed;
  - `docs/**` matches nested tracked changes;
  - a rename is reported as both old and new paths because `--no-renames` is used;
  - `../escape` and `:(exclude)app.txt` return exit `2` without invoking a permissive matcher.

- [x] **Step 2: Run the focused test and verify it fails.**

  Run `go test ./internal/profile -run ScopeCheck -count=1`; expect failure because the tool does not exist.

- [x] **Step 3: Implement the smallest standalone Go tool.**

  Decode strict JSON from the single argv rendered by `script.args`, reject empty/leading-colon/absolute/volume/`..` pathspecs, resolve `git merge-base HEAD <base_branch>`, collect changed and matched NUL-delimited paths with the exact Git commands above, sort/deduplicate, compute `outside_allowed`, and emit the report. Treat Git errors and malformed input as exit `2`; emit the drift report and exit `3` when `outside_allowed` is non-empty. Do not inspect `$ARTIFACTS_DIR`.

- [x] **Step 4: Run the focused test and the profile contract.**

  Run `go test ./internal/profile -run ScopeCheck -count=1` and `go test ./internal/profile -count=1`; both must pass.

- [x] **Step 5: Commit the scope checker.**

  Run `git add internal/profile/builtin/code/tools/scope-check.go internal/profile/scope_check_test.go && git commit -m "feat: add deterministic git scope gate"`.

### Task 2: Make `review-block` terminally accept only a valid review

**Files:**
- Modify: `internal/profile/builtin/code/workflows/review-block.yaml`
- Modify: `internal/testsupport/cmd/takt-fake-code-agent/main.go`
- Modify: `tests/e2e/external_boundaries_test.go`

**Interfaces:**
- Consumes: Existing review child input and outputs from `scope`, `synthesize`, `fixes`, and `validate`.
- Produces: exact `REVIEW_BLOCK_ACCEPTED` child output only after deterministic post-review validation and an existing `review-report.md` artifact.
- Fixture controls: `FAKE_REVIEW_CHANGES_REQUIRED=1` makes `review-synthesis` return `REVIEW_CHANGES_REQUIRED`; `FAKE_BLOCK_PHASE=review-fix` returns `REVIEW_FIX_REQUIRES_DECISION`.

- [x] **Step 1: Add failing E2E scenarios.**

  Extend the deep code workflow fixture with: a happy review, `validation_commands:["false"]` (must fail before publication), and `FAKE_REVIEW_CHANGES_REQUIRED=1` plus `FAKE_BLOCK_PHASE=review-fix` (must fail the child gate). Assert persisted node status, gate diagnostic, required artifact, and fake PR count.

- [x] **Step 2: Run the focused E2E test and verify the new cases expose the gap.**

  Run `go test ./tests/e2e -run 'DeepCodeWorkflow|ReviewBlock' -count=1`; the unresolved-review case must currently complete or skip instead of producing a failed Run.

- [x] **Step 3: Add fixture modes without changing production semantics.**

  In the fake agent, branch on `FAKE_REVIEW_CHANGES_REQUIRED` for `review-synthesis`, add `REVIEW_FIX_REQUIRES_DECISION` to `blockedCodeFor`, and preserve normal artifact creation. Keep `FAKE_BLOCK_PHASE` behavior for all other phases.

- [x] **Step 4: Add post-review validation and a terminal bash gate.**

  Append `script.runtime: validation` node `post-review-validation-commands` after `synthesize`/`fixes`, with typed `validation-command-report` and `$ARTIFACTS_DIR/post-review-validation-commands.json`. Add `review-acceptance-gate` with `trigger_rule: all_done` that requires scope ready, `REVIEW_APPROVED` or `REVIEW_CHANGES_REQUIRED` plus `REVIEW_FIXES_APPLIED`, `validate.code == VALIDATION_PASSED`, post-review validation ready, and the review-report artifact path to exist. Emit `REVIEW_BLOCK_ACCEPTED` on success and exit `1` otherwise.

- [x] **Step 5: Run focused E2E and commit.**

  Run `go test ./tests/e2e -run 'DeepCodeWorkflow|ReviewBlock' -count=1`; then commit with `git add internal/profile/builtin/code/workflows/review-block.yaml internal/testsupport/cmd/takt-fake-code-agent/main.go tests/e2e/external_boundaries_test.go && git commit -m "feat: gate review block completion"`.

### Task 3: Add scope, PR-result, and final acceptance gates to `plan-to-pr`

**Files:**
- Modify: `internal/profile/builtin/code/workflows/plan-to-pr.yaml`
- Modify: `internal/testsupport/cmd/takt-fake-code-agent/main.go`
- Modify: `tests/e2e/external_boundaries_test.go`

**Interfaces:**
- Consumes: Required `allowed_paths`, existing validation gate, scope-check tool, governed `review-block` result, and typed phase artifacts.
- Produces: exact `WORKFLOW_ACCEPTED` root output only when all required evidence is present.
- Fixture controls: `FAKE_OMIT_ARTIFACT_PHASE`, `FAKE_EXTRA_CHANGE_PATH`, `FAKE_BLOCK_PHASE=pr-finalize` (`PR_CREATE_FAILED`), and `FAKE_BLOCK_PHASE=workflow-final-summary` (`WORKFLOW_INCOMPLETE`).

- [x] **Step 1: Add the failing acceptance matrix.**

  Add E2E cases for happy `safe_success`, omitted plan artifact, false deterministic validation, early blocked implementation, extra `docs/extra.md`, blocked PR creation, unresolved review, and incomplete summary. Assert the Run classification (`completed` only for happy), exact gate failure for safe stops, unchanged control checkout, required artifacts, and fake PR count (`0` before PR gates; `1` for review/summary failures).

- [x] **Step 2: Run the matrix and verify tail cases are unsafe today.**

  Run `go test ./tests/e2e -run TestPlanToPRAcceptance -count=1`; PR/review/summary failure cases must demonstrate the current completed/skipped-node hole before workflow changes.

- [x] **Step 3: Extend the fake agent for deterministic fixture outcomes.**

  Omit the requested artifact when `FAKE_OMIT_ARTIFACT_PHASE` matches the phase, write `docs/extra.md` in the isolated workspace for `FAKE_EXTRA_CHANGE_PATH=docs/extra.md`, map blocked `pr-finalize` to `PR_CREATE_FAILED` and blocked `workflow-final-summary` to `WORKFLOW_INCOMPLETE`, and keep all default happy-path behavior unchanged.

- [x] **Step 4: Make the workflow input and gates explicit.**

  Add required `allowed_paths` with `minItems: 1` and `uniqueItems: true`. Add a `scope-check` Go script node after `validation-gate`, before `create-pr`, and a second scope node after review fixes. Add `pr-result-gate` requiring `create-pr.status == ready`, `create-pr.code == PR_READY`, and the PR artifact. Remove the old review `when`; make review depend on the PR gate. Make summary depend on review and the second scope gate. Add `acceptance-gate` with `trigger_rule: all_done` requiring confirmed plan, ready implementation, initial or recovered validation, mandatory typed artifacts, PR_READY, `REVIEW_BLOCK_ACCEPTED`, `WORKFLOW_COMPLETE`, and both scope reports. Emit `WORKFLOW_ACCEPTED`; otherwise exit `1`.

- [x] **Step 5: Run focused acceptance tests and commit.**

  Run `gofmt -w internal/testsupport/cmd/takt-fake-code-agent/main.go tests/e2e/external_boundaries_test.go`, then `go test ./tests/e2e -run TestPlanToPRAcceptance -count=1`; commit with `git add internal/profile/builtin/code/workflows/plan-to-pr.yaml internal/testsupport/cmd/takt-fake-code-agent/main.go tests/e2e/external_boundaries_test.go && git commit -m "feat: enforce plan to pr acceptance gates"`.

### Task 4: Document the contract and route only complete inputs

**Files:**
- Modify: `internal/profile/builtin/code/commands/route-workflow.md`
- Modify: `internal/profile/builtin/code/README.md`
- Modify: `internal/profile/builtin/code/VERSION`
- Modify: `README.md`
- Modify: `docs/03-specification.md`
- Modify: `docs/05-implementation-status.md`
- Modify: `CHANGELOG.md`
- Modify: `TEST_RESULTS.md`

**Interfaces:**
- Consumes: The `allowed_paths` input and `plan-to-pr` acceptance output from Task 3.
- Produces: User-facing documentation that says the router selects `plan-to-pr` only for complete JSON input and never infers scope; profile version `0.17.0`.

- [x] **Step 1: Add/adjust documentation contract assertions.**

  Add a Go contract assertion for profile version `0.17.0`, documented `allowed_paths`, and the complete-input routing rule before changing the docs.

- [x] **Step 2: Run the focused contract and verify it fails.**

  Run `go test ./tests/e2e -run 'CodeProfileCatalogContract|Route' -count=1`; expect failure against the old version/documentation.

- [x] **Step 3: Update only current docs and version.**

  Document `base_branch`, non-magic repository-relative Git pathspecs, required scope input, deterministic gates, bounded fake-SCM guarantee, and `safe_success|safe_stop|unsafe_success`. Update the profile version to `0.17.0`, root current-version references, implementation status, changelog, and `TEST_RESULTS.md`. Do not edit historical `docs/38` or older release records.

- [x] **Step 4: Make router selection fail closed.**

  Update `route-workflow.md` so `plan-to-pr` is eligible only when the original JSON contains repository, plan_path, base_branch, draft_pr, validation_commands, and non-empty unique allowed_paths; otherwise return `assist` and do not infer a scope list. Keep `workflow.yaml` input forwarding unchanged for complete inputs.

- [x] **Step 5: Run focused contracts and commit.**

  Run `go test ./tests/e2e -run 'CodeProfileCatalogContract|Route' -count=1`; commit with `git add internal/profile/builtin/code/commands/route-workflow.md internal/profile/builtin/code/README.md internal/profile/builtin/code/VERSION README.md docs/03-specification.md docs/05-implementation-status.md CHANGELOG.md TEST_RESULTS.md tests/e2e && git commit -m "docs: publish deterministic plan to pr contract"`.

### Task 5: Run the full release verification

**Files:**
- Modify: only files required by formatting or generated contract output.

- [x] **Step 1: Format and run all required checks.**

  Run:

  ```bash
  gofmt -w cmd internal sdk reference tests
  go test ./... -count=1
  go test -race ./... -count=1
  go vet ./...
  make check
  ./scripts/verify.sh
  ```

  With the installed TypeScript compiler, `./scripts/test-host-integrations-typescript.sh` must report `PASS`; no documentation grep gate is expected.

- [x] **Step 2: Inspect the final persisted evidence.**

  Run `git diff --check`, `git status --short`, and the focused acceptance test once more. Confirm the working tree is clean except for intentional generated/build outputs ignored by Git, then record exact commands and outcomes in `TEST_RESULTS.md`.

- [x] **Step 3: Commit verification evidence.**

  Commit any intentional test-results-only update with `git add TEST_RESULTS.md && git commit -m "test: record deterministic acceptance evidence"`; otherwise leave the previous focused commits unchanged.
