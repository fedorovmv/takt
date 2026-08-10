# Deterministic Development Flow Acceptance

## Status and objective

Approved by the user on 2026-08-10.

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

A run has one of three product outcomes:

- `safe_success`: the requested result and every process invariant are proven;
- `safe_stop`: Takt detected an incomplete or unsafe result before accepting it;
- `unsafe_success`: Takt reported success despite a missing invariant.

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

Two gaps prevent the current code profile from making the stronger product
claim:

1. A structured assistant result such as `{"status":"blocked"}` is still a
   technically completed node. If its dependent tail is skipped and no node is
   failed, the Run can become `completed`.
2. Scope limits are not a deterministic input of `plan-to-pr`; prompts can ask
   the agent to stay in scope, but the workflow cannot prove that a declared
   extra file was forbidden by the user.

Dynamic Takt has an additional gap: its required validation check currently
reads the assistant-provided `passed` field. Replacing that self-attestation
with project-owned deterministic evidence is the second stabilization stage,
not part of this first implementation slice.

## Minimal production changes

### Explicit scope input

`code:plan-to-pr` gains a required non-empty `allowed_paths` array. Values are
repository-relative Git pathspecs. This is governance data that the workflow
must see; it is not inferred from Markdown plan text.

Before the first remote publication, a deterministic scope gate compares all
tracked and untracked worktree changes against `allowed_paths`. Any unmatched,
absolute or parent-escaping path fails the gate. The agent's own changed-file
report may be retained as evidence, but it is not the authority.

The input-contract change is documented in the current specification and code
profile documentation. No new generic workflow field or node type is added.

### Domain completion gates

The reference workflow gains deterministic gates at the points where a
structured domain status controls progress. A required phase is accepted only
when its expected success status/code is present. A `blocked` or `failed`
assistant result must therefore lead to a failed or waiting product outcome,
not a successful Run whose tail merely happens to be skipped.

The final acceptance gate requires:

- confirmed plan and ready implementation;
- successful initial validation, or successful recovery and revalidation;
- the mandatory typed artifacts for the phases that actually ran;
- a ready draft-PR result;
- a completed review result and workflow summary.

This remains workflow-level policy. The generic runtime must not learn the
meaning of profile-specific fields such as `status`, `code` or `approved`.

### Draft PR boundary

The existing review block expects an existing PR. The first slice therefore
allows creation of a draft PR only after deterministic validation and scope
gates. The Run is not a `safe_success` until review and final acceptance pass.

Moving review before every remote PR side effect would require a separate local
diff review contract and is deliberately outside this slice.

## Acceptance suite

The suite reuses `tests/e2e`, the existing fake coding agent, fake `gh`, Run
state/events, artifact metadata and Git inspection. It does not add a benchmark
runner, scoring framework or shell test layer.

New `plan-to-pr` scenarios:

| Scenario | Injected worker behavior | Required result |
|---|---|---|
| Happy path | Produces the requested change and all phase outputs | `safe_success`; exact phases, artifacts, checks, draft PR and review are present |
| Missing artifact | Returns schema-valid success but omits a required file | `safe_stop`; artifact capture fails and no PR is created |
| False validation claim | Claims tests pass while a configured command fails | `safe_stop`; the deterministic validation result wins and no PR is created |
| Scope drift | Changes an allowed file and another path outside `allowed_paths` | `safe_stop`; scope gate fails and no PR is created |
| Blocked phase | Returns a valid `blocked` domain result | `safe_stop`; Run cannot finish as successful and downstream mutation/publication does not run |

The oracle inspects persisted state rather than agent prose:

- node statuses and event order;
- artifact type, producer, checksum and required file content;
- deterministic command output and exit status;
- actual Git diff and unchanged control checkout;
- fake SCM state and draft PR metadata.

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
- A worker refusal or blocked status is preserved with its diagnostic and does
  not masquerade as completed delivery.
- Infrastructure failure in the fixture, fake SCM or persistence fails the E2E
  test itself; it is not classified as agent containment.
- The original checkout must remain unchanged in every outcome.

## Design unknowns gate

**Status: READY. Confidence: high.**

| ID | Priority | Resolution |
|---|---|---|
| U-01: reference product flow | P0 | Closed: `code:plan-to-pr` |
| U-02: success definition | P0 | Closed: `safe_success`, `safe_stop`, `unsafe_success`; release requires zero unsafe success |
| U-03: authority for write scope | P0 | Closed: required user-owned `allowed_paths`, checked against actual Git state |
| U-04: draft PR before review | P1 | Bounded: draft only after validation/scope; final success only after review |
| U-05: Dynamic validation self-attestation | P1 | Deferred to a separate second-stage design; no strong Dynamic claim before it is fixed |
| U-06: real-model baseline size | P2 | Excluded from release evidence; live run is demonstrative only |

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
