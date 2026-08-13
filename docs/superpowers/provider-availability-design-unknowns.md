# Provider availability recovery: design unknowns audit

## Verdict

**Status: READY** — user-approved contract, no open P0/P1 unknowns.

## Confirmed territory

| Area | Fact and evidence |
|---|---|
| Workflow retry | `internal/runtime/attempt.go` and `runner.go` retry only configured execution kinds and persist `RetryState`. |
| Durable state | `internal/store/store.go:80` has `RetryState`; `ExecutionState.Attempt` is currently the workflow-attempt number. |
| Pi | `internal/extensions/assistants/pi/pi.go` records Pi internal retry counters, waits for `agent_settled`, and returns terminal agent errors after exhaustion. |
| OpenCode | `internal/extensions/assistants/opencode/opencode.go` parses JSON error events and preserves stderr/provider diagnostics. |
| Eval | `internal/tooling/evaluation/flow.go` validates after terminal Run; `flow_report.go` currently maps failed + valid=false to `true_reject`. |
| Prior incident | `.takt/evals/feature-development/20260813T124333.433038000Z/report.json` records `aihub/Qwen/Qwen3.6-27B`, Pi exit 1, HTML 503, and missing implementation artifact. |

## Registry

| ID | Unknown | Priority | Closure |
|---|---|---:|---|
| U-01 | Whether provider retry should consume workflow attempts | P0 | User decision: separate durable provider scope; closed. |
| U-02 | Whether an exhausted provider failure is product invalid or infrastructure | P0 | User decision: infrastructure outcome, excluded denominator; closed. |
| U-03 | Whether retry may resume the same session | P1 | User decision: same session ID; constrained to model transport only; closed. |
| U-04 | Whether Pi/OpenCode expose enough structured evidence | P1 | Code and installed Pi/OpenCode inspection: adapter diagnostics/events available; classification remains adapter-owned; closed. |
| U-05 | How retry survives restart/pause | P1 | Existing `RetryState` reused with explicit scope; contract test required; closed. |
| U-06 | Whether `Retry-After` can cause unbounded waits | P1 | Cap at 60s; default 2s/4s; closed. |
| U-07 | Whether external/domain side effects can use this retry | P0 | Explicit non-goal: no; domain adapters retain reconcile semantics. |

## Blind-spot pass

- Parent cancellation/deadline remains authoritative over provider errors.
- A provider error after a mutating domain action is not eligible; domain
  adapters remain behind `side_effect: reconcile`.
- Provider retries must not inflate `attempts_to_valid`, success-at-first,
  or workflow retry counters.
- Missing validator artifacts remain a product/evidence failure only when the
  Run reached validator evaluation; provider-unavailable skips product
  classification and preserves the validator result for audit.
- Redaction applies to provider messages before state/events/artifacts persist.
- Old Runs without provider scope retain current semantics; new state is
  versioned through optional fields.

## Conditions for implementation

- Add regression tests before implementation changes.
- Use the existing retry persistence and scheduler; do not add a second
  executor or sleep loop.
- Keep provider classification provider-neutral in `internal/execution`; Pi and
  OpenCode only supply evidence/classification.
- Update `docs/09-runtime-semantics.md`, `docs/10-assistant-adapter-spec.md`,
  `docs/05-implementation-status.md`, changelog, schemas, and ADR.

## Transfer contract

Follow the approved design document exactly. If implementation discovers that
an adapter cannot prove transient status, that provider retry cannot preserve
session identity, or that evaluation cannot exclude infrastructure runs from
denominators without changing existing report compatibility, stop and return to
design-unknowns. Record the fact, source, affected contracts, changes already
made, and the question requiring re-review.
