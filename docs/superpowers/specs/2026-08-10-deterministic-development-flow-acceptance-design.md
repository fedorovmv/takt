# Deterministic Development Flow Acceptance

## Status and objective

Originally approved by the user on 2026-08-10. Revised after factual review;
implementation remains gated on review of these clarifications.

The first stabilization target is the product value of Takt as a user-owned,
deterministic development process. The reference workflow is `code:plan-to-pr`.
The acceptance evidence must show that required phases run, required artifacts
exist, deterministic checks control publication, unrelated changes are stopped,
and execution remains isolated and inspectable.

This is not a model-quality benchmark. The existing Go benchmark remains useful
only as host/model integration smoke and diagnostic evidence. Its direct versus
repair results do not establish Takt's product value.

## Product contract

For the reference workflow, Takt owns this structure:

```text
plan confirmation
→ implementation
→ deterministic validation
→ draft PR
→ independent review
→ final acceptance
```

The agent supplies investigation, implementation and review decisions inside
those boundaries. It does not decide whether a required phase, artifact or gate
may be omitted.

A persisted Run and the external acceptance oracle together produce one of
three product outcomes:

- `safe_success`: Run status is `completed` and every oracle invariant passes;
- `safe_stop`: a deterministic product gate makes the Run `failed` before Takt
  accepts the incomplete or unsafe result;
- `unsafe_success`: Run status is `completed`, but at least one oracle invariant
  fails.

The release criterion is exactly zero `unsafe_success` outcomes. A safe stop is
not counted as successful delivery, but it is correct containment.

## Confirmed current behavior

- The scheduler owns DAG order and executes deterministic and assistant nodes
  through the same Run state machine.
- Declared `output_path` artifacts are fail-closed: a missing, non-regular or
  out-of-workspace file fails artifact capture.
- `plan-to-pr` already executes input-provided validation commands through the
  deterministic `validation` script runtime and places a validation gate before
  PR creation.
- Managed worktrees keep execution changes out of the control checkout and
  retain dirty or failed evidence.
- The daemon and file Store provide durable background lifecycle and recovery;
  portable packages and fingerprints cover committed workflow distribution.

The generic runtime correctly treats structured assistant output as data. The
actual `plan-to-pr` completion holes are therefore in its domain tail:

1. `create-pr` may return domain `blocked|failed` while its Node is completed;
   review and summary are then skipped and the Run can become `completed`.
2. A failure inside `review-block` can similarly leave only completed/skipped
   parent nodes, and `workflow-final-summary` may return
   `WORKFLOW_INCOMPLETE|EVIDENCE_INCONSISTENT` as a completed Node.
3. Scope limits are not a deterministic input of `plan-to-pr`; prompts can ask
   the agent to stay in scope, but the workflow cannot prove that a declared
   extra file was forbidden by the user.

Early implementation and validation failures are already contained by the
existing `validation-gate`. They remain regression scenarios, not claims of a
new production fix.

Dynamic Takt has an additional gap: its required validation check currently
reads the assistant-provided `passed` field. Replacing that self-attestation
with project-owned deterministic evidence is the second stabilization stage,
not part of this first implementation slice.

## Minimal production changes

### Explicit scope input and matcher

`code:plan-to-pr` gains a required non-empty `allowed_paths` array. Values are
repository-relative Git pathspecs interpreted by Git itself. This deliberately
does not reuse `rolecontract.PathScope`: that contract is a different glob
language and currently classifies an assistant's `changed_files` report rather
than actual repository state.

The accepted syntax is Git's non-magic pathspec form: literal repository paths,
directory prefixes and Git wildcards. Leading `:` magic, exclusion/attribute
pathspecs, absolute paths, empty values and any `..` segment are rejected. This
keeps the allowlist monotonic while leaving actual matching to Git.

The gate is an existing `script.runtime: go` node backed by one small profile
tool. The tool uses only the Go standard library and the installed Git binary;
it does not add a runtime action or implement another matcher. It:

1. rejects empty, absolute and parent-escaping pathspec inputs before invoking
   Git;
2. resolves the candidate base with
   `git merge-base HEAD <base_branch>` from the input `base_branch`;
3. obtains tracked/staged/worktree paths from
   `git diff --name-only -z --no-renames <base> --` and untracked paths from
   `git ls-files --others --exclude-standard -z`;
4. asks the same Git commands for the subset matched by `allowed_paths` and
   fails when `all_changes - matched_changes` is non-empty.

NUL-delimited output makes `core.quotepath` irrelevant. `--no-renames` exposes
both sides of a rename as delete/add paths, and a changed submodule is checked
as its repository-relative path. `$ARTIFACTS_DIR` is in the Run store outside
the execution worktree and is intentionally not repository drift.

The gate runs after the existing validation/recovery branch and before draft PR
creation. The same tool runs again after review fixes, before final acceptance.
The agent's own changed-file report may be retained as evidence, but it is not
the authority.

The operator or API client invoking `code:plan-to-pr` supplies `allowed_paths`.
The smart router may select this workflow only when the original input is a
complete JSON object containing the existing fields and `allowed_paths`; it
does not infer scope. An incomplete routed input fails child input validation
before an assistant or side effect. `idea-to-pr` is an independent workflow,
and the Task API/task-source path uses the separate Task Router, so neither is a
producer of `plan-to-pr` input.

The contract change will be documented in `docs/03-specification.md`, the code
profile README and `route-workflow.md`. Historical `docs/38` remains unchanged.
Implementation status, changelog and `TEST_RESULTS.md` are updated with only the
delivered evidence. No new generic workflow field or node type is added.

### Domain completion gates

The reference workflow gains deterministic gates in the currently unsafe tail.
The PR result gate requires `create-pr.status=ready` and
`create-pr.code=PR_READY`; otherwise it fails before review. The terminal gate
requires `summary.status=ready` and `summary.code=WORKFLOW_COMPLETE`; otherwise
the Run fails instead of accepting a completed assistant Node.

The final acceptance gate requires:

- confirmed plan and ready implementation;
- successful initial validation, or successful recovery and revalidation;
- the mandatory typed artifacts for the phases that actually ran;
- `PR_READY` from the draft-PR phase;
- an accepted review-block result;
- `WORKFLOW_COMPLETE` from the summary phase;
- the second deterministic scope check after any review fixes.

This remains workflow-level policy. The generic runtime must not learn the
meaning of profile-specific fields such as `status`, `code` or `approved`.

### Operational review threshold

`review-block` gains its own deterministic terminal gate. A successful child
result requires:

- `scope` ready and a captured `review-report` artifact;
- either `REVIEW_APPROVED`, or `REVIEW_CHANGES_REQUIRED` followed by
  `REVIEW_FIXES_APPLIED`;
- the existing independent validation result `VALIDATION_PASSED`;
- a fresh `script.runtime: validation` pass after optional review fixes.

`REVIEW_NO_FIXES_REQUIRED` is not sufficient after
`REVIEW_CHANGES_REQUIRED`. The terminal gate publishes one structured accepted
result as the governed child output; parent `plan-to-pr` checks that result,
not merely child terminality or the existence of summary prose.

### Draft PR boundary

The existing review block expects an existing PR. The first slice therefore
allows creation of a draft PR only after deterministic validation and scope
gates. The Run is not a `safe_success` until review and final acceptance pass.

This is the explicit guarantee boundary: Takt proves the pre-publication
validation/scope gates and the persisted workflow evidence. `create-pr` still
uses an assistant and `gh`, not the SCM domain adapter with durable receipt and
reconciliation. Production runtime therefore does not independently prove the
remote PR state; `pr.json` is evidence supplied by that phase. The E2E oracle
cross-checks it against fake SCM state, but no provider-independent real-SCM
guarantee is claimed in this slice.

Moving review before every remote PR side effect would require a separate local
diff review contract and is deliberately outside this slice.

## Acceptance suite

The suite reuses `tests/e2e`, the existing fake coding agent, fake `gh`, Run
state/events, artifact metadata and Git inspection. It does not add a benchmark
runner, scoring framework or shell test layer.

New `plan-to-pr` scenarios:

| Scenario | Kind | Injected worker behavior | Required result |
|---|---|---|---|
| Happy path | acceptance | Produces the requested change and all phase outputs | `safe_success`; exact phases, artifacts, checks, draft PR and review are present |
| Missing artifact | regression | Returns schema-valid success but omits a required file | `safe_stop`; artifact capture fails and no PR is created |
| False validation claim | regression | Claims tests pass while a configured command fails | `safe_stop`; the existing deterministic validation gate wins and no PR is created |
| Early blocked implementation | regression | `plan-implementation` returns `PLAN_IMPLEMENTATION_BLOCKED` | `safe_stop` through the existing validation tail; no PR is created |
| Scope drift | production gap | Changes an allowed file and another path outside `allowed_paths` | `safe_stop`; scope gate fails and no PR is created |
| Blocked PR creation | production gap | `pr-finalize` returns `PR_CREATE_FAILED` from a completed Node | `safe_stop`; PR-result gate fails and review does not run |
| Review changes unresolved | production gap | `review-synthesis` requires changes but fixes cannot be applied | `safe_stop`; review-block gate fails |
| Incomplete summary | production gap | `workflow-final-summary` returns `WORKFLOW_INCOMPLETE` | `safe_stop`; terminal acceptance gate fails |

The oracle inspects persisted state rather than agent prose:

- node statuses and event order;
- artifact type, producer, checksum and required file content;
- deterministic command output and exit status;
- actual Git diff and unchanged control checkout;
- fake SCM state and draft PR metadata.

The fake coding agent gains explicit phase/NodeID modes for omitted artifacts,
out-of-scope writes, PR failure, unresolved review findings and incomplete
summary. The existing multi-repository fixture is the precedent for writing a
real file into the execution workspace; assertions never accept a fake output
without inspecting the resulting Git state.

Existing focused contracts remain the evidence for parallel worktree isolation,
daemon background/recovery and package portability. They are linked from the
acceptance report instead of being duplicated in one oversized E2E test.

## Live evidence

After the deterministic release gate passes, one small live walkthrough may run
the same repository task directly in OpenCode and through Takt with the chosen
Qwen model. It records the trace and product outcome only.

It does not use repeats, wall-clock comparisons, pass@k or statistical claims.
A direct-agent success does not weaken the Takt guarantee, and a direct-agent
failure is not needed to make the deterministic acceptance valid.

## Failure semantics

- Missing or invalid required evidence is a product failure, never a warning.
- A deterministic validation or scope failure prevents PR creation.
- Tail domain failures are converted into failed Runs by deterministic gates;
  their original structured output remains persisted evidence.
- Infrastructure failure in the fixture, fake SCM or persistence fails the E2E
  test itself; it is not classified as agent containment.
- The original checkout must remain unchanged in every outcome.
- `safe_success` and `unsafe_success` classify `completed` Runs through the
  oracle; `safe_stop` requires the expected gate-induced `failed` Run.

## Design unknowns gate

**Status: CONDITIONAL. Confidence: medium.** The technical choices below are
closed in the text, but implementation remains gated on re-review of this
revision.

| ID | Priority | Resolution |
|---|---|---|
| U-01: reference product flow | P0 | Closed: `code:plan-to-pr` |
| U-02: success definition | P0 | Closed: `safe_success`, `safe_stop`, `unsafe_success`; release requires zero unsafe success |
| U-03: authority for write scope | P0 | Closed: required user-owned `allowed_paths`, checked against actual Git state |
| U-04: draft PR before review | P1 | Bounded: draft only after validation/scope; final success only after review |
| U-05: Dynamic validation self-attestation | P1 | Deferred: unlike Dynamic `passed`, plan phase statuses only route the flow; objective acceptance additionally requires actual validation commands, Git scope, artifacts and the external oracle |
| U-06: real-model baseline size | P2 | Excluded from release evidence; live run is demonstrative only |
| U-07: scope matcher and change source | P1 | Closed: native Git pathspec matching over NUL-delimited diff/untracked paths in a profile Go script |
| U-08: required scope producers | P1 | Closed: direct client/operator; smart router only forwards a complete JSON contract; idea-to-pr and Task API are not callers |
| U-09: review threshold | P1 | Closed: approved or changes-applied, then independent and deterministic post-review validation, exposed through a terminal child gate |
| U-10: remote publication guarantee | P1 | Bounded: fake SCM is cross-checked in E2E; real PR receipt/reconcile is not claimed without SCM adapter migration |

The design is disproved if `plan-to-pr` cannot enforce the accepted scope or
domain outcome without a new generic scheduler, workflow language or parallel
execution path. In that case implementation stops and returns to design rather
than adding a special-purpose runtime abstraction.

## Verification

Implementation follows TDD and the repository's release gate:

1. Add each failing E2E scenario before its minimal production change.
2. Run focused tests while implementing.
3. Run `gofmt`, `go test ./... -count=1`, `go test -race ./... -count=1`,
   `go vet ./...`, documentation checks, `make check` and `scripts/verify.sh`.
4. Update implementation status, changelog, test results and affected workflow
   contract documentation only for behavior actually delivered.

## Implementation handoff contract

Preserve the decisions, assumptions and limits above. Reuse the existing
scheduler, profile workflow, validation runtime, E2E harness and fake adapters.
Do not add a second executor, a new benchmark framework, a generic plugin layer
or model-derived scope parsing.

If implementation discovers a fact that changes the workflow contract, data,
security boundary or component boundary, or invalidates a P0/P1 assumption,
stop, record the evidence and return the task to `design-unknowns` before making
an implicit architectural change.
