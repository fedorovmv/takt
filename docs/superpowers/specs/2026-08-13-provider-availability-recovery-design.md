# Provider availability recovery: design

## Status

Approved design. Implementation must remain limited to provider availability
classification, durable provider retries, trace/report semantics, and tests.

## Problem

An assistant provider can return a transient failure after a long tool loop.
Today Pi/OpenCode expose that failure as an ordinary `exit` or `timed_out`, so a
flow validator may report a product `true_reject` even though no product
contract was evaluated. The run also has no durable provider retry boundary.

The target behavior is:

- transient provider failures are classified as `provider_unavailable`;
- Takt performs at most three durable provider retries with the same session;
- provider retries do not consume workflow `attempts.max`;
- after exhaustion, evaluation records an infrastructure failure and excludes
  it from quality denominators while retaining usage and diagnostics.

## Current territory

- `internal/runtime/attempt.go` owns workflow attempt retry and only retries
  kinds listed in `attempts.retry_on`.
- `store.RetryState` already persists `next_attempt`, `not_before`, `delay`,
  kind, and diagnostic fingerprint.
- `store.ExecutionState.Attempt` currently means workflow attempt and is used by
  evaluation `attempts_to_valid`; it must not be overloaded by provider retries.
- Pi emits `auto_retry_start`/`auto_retry_end` and already retries transient
  model failures internally. After its own budget is exhausted, the adapter
  returns a terminal assistant error.
- OpenCode exposes JSON `error` events and stderr diagnostics. Its adapter
  currently maps these to ordinary `exit` or preserves parent timeout.
- Runtime commits `node.failed`/`node.errored` and `run.failed`; flow evaluation
  currently classifies any completed validator envelope on a failed run as
  `true_reject`.

## Contract

### Provider error classification

Adapters return a new execution kind `provider_unavailable` only when the
failure is demonstrably transient:

- HTTP status `429`, `502`, `503`, or `504` reported by the provider/runtime;
- a provider error explicitly identified as rate-limited, overloaded, or
  temporarily unavailable;
- connection reset, connection refused, temporary DNS, or equivalent transport
  failure when no request-side effect can be observed.

`4xx` errors other than `429`, malformed protocol, tool failures, context
overflow, assistant decisions, and unknown external side effects are not
provider-unavailable. Parent context cancellation/deadline remains authoritative
and wins over provider classification.

The normalized human diagnostic keeps the provider message. The diagnostic
code/kind is `provider_unavailable`; the fingerprint is stable after volatile
workspace/process details are removed.

### Durable provider retry

Provider retry uses the existing durable retry marker, extended with an explicit
scope (`workflow` or `provider`). `scope=provider` stores its own `next_attempt`,
`not_before`, `delay`, kind, and fingerprint. The maximum is exactly three
provider executions total (initial call plus two retries). The default delays are
2s and 4s; `Retry-After` is honored when present but capped at 60s. A provider
retry always uses the same session ID and does not increment workflow attempt
count or `attempts_to_valid`.

The provider retry marker is committed before waiting. Pause, cancel, daemon
restart, and process recovery preserve the marker. On readiness Takt commits a
retry-ready event and invokes the adapter again. Each actual provider execution
is retained in execution history with a provider-attempt ordinal, usage,
diagnostics, and session ID.

If the provider retry budget is exhausted, the node and Run terminate with
`error_code=provider_unavailable`. No workflow `retry_on` or `allow_failure`
setting can convert this into a successful product result.

### Evaluation semantics

Flow records with `provider_unavailable` use:

- `status=failed` (the durable Run status remains truthful);
- `outcome=infrastructure_error`;
- no `true_reject`/`false_reject` classification;
- no increment to `flow.evaluated_runs`, `quality_runs`, `valid`, or `invalid`;
- an explicit infrastructure counter and diagnostic distribution;
- retained tokens, cost, duration, executions, and validator evidence.

The suite continues to the next case. Existing preparation, validator, and
report-write errors remain suite infrastructure errors and keep their current
stop behavior. `flow_completion_rate` continues to use all scheduled cases;
quality rates use only runs that reached a non-infrastructure validator result.

### Observability

Trace emits:

```text
provider.retry.scheduled run=<id> node=<id> attempt=1/3 delay=2s kind=provider_unavailable
provider.retry.ready run=<id> node=<id> attempt=2/3
provider.retry.exhausted run=<id> node=<id> attempts=3
```

Reports expose provider-attempt counts and `provider_unavailable` diagnostics at
run, node, and summary levels. Raw adapter stdout/stderr remains available for
diagnosis and is redacted before persistence.

## Non-goals

- no generic provider plugin framework;
- no retry of arbitrary assistant exits or malformed output;
- no change to workflow `attempts.max`, `retry_on`, or user-facing retry syntax;
- no guarantee that a request already accepted by a provider is harmless to
  repeat; provider retries apply only to model invocation transport, not domain
  side effects.

## Acceptance criteria

1. Fake Pi/OpenCode tests classify 429/503 and temporary connection failures as
   `provider_unavailable`, while ordinary exit, protocol, timeout, and cancel
   retain their existing kinds.
2. A runtime test proves provider retry persists across pause/reload and keeps
   workflow attempt count unchanged.
3. A runtime test proves provider retry exhaustion produces durable
   `provider_unavailable` and no successful `allow_failure` path.
4. Flow evaluation test proves provider-unavailable runs are excluded from
   quality denominators but their usage and diagnostics remain in the report.
5. Trace/report/schema tests cover retry scheduled, ready, exhausted, and
   provider-attempt attribution.

## Rollback

The change is backward compatible for existing workflows: if no adapter emits
`provider_unavailable`, behavior is unchanged. Removing the new scope and
classification fields is safe for old state; new state must be rejected as
unresumable only if its provider retry marker cannot be interpreted.
