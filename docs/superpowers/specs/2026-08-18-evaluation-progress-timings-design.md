# Evaluation progress timings

## Goal

Make `progress.json` and `takt eval status` expose measured time spent in the
evaluation phases and in observable assistant work while a flow is running.
The existing live counters and the read-only nature of status remain unchanged.

## Contract

`runtime.timings` is an optional object in
`takt-flow-evaluation-progress/v1alpha1`. New snapshots populate:

- `phases`: accumulated milliseconds for `prepare`, `validator_preflight`,
  `workflow`, `validator`, `evidence`, and `cleanup`;
- `assistant`: provider-reported `wait_ms`, `stream_ms`, and `total_ms`, plus
  elapsed assistant tool calls measured from normalized tool lifecycle events.

`current.phase_started_at` identifies the currently accumulating phase. A
missing `timings` object in an old snapshot means the metric is unavailable;
measured zero remains `0`.

The values are additive observations. Assistant totals may overlap phase time
and `assistant.total_ms` intentionally includes provider wait and stream time.
Parallel nodes contribute to assistant totals independently; the suite
`Elapsed` remains wall-clock time.

## Data flow

The flow progress tracker accumulates phase time on every atomic progress write
and on phase transitions. The bootstrap flow activity tracker consumes Pi
diagnostic timing fields and tool start/complete timestamps, then merges those
assistant totals into the next runtime progress snapshot. `eval status` renders
the persisted timing values and the active phase/provider age without starting
any workflow or model.

## Compatibility and failure handling

The schema change is additive and keeps the `v1alpha1` progress version. Old
snapshots remain readable and render timing as unavailable. Missing or malformed
timing values fail the same closed validation path as existing progress
counters; negative durations are rejected. Timing persistence errors continue
to be returned by the progress writer.

## Verification

Go tests cover phase accumulation, assistant diagnostic/tool aggregation,
atomic round-trip, rendering, schema validation, and preservation of timing
values when runtime counters are refreshed.
