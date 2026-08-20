# mini-du flow evaluation corpus

Общий порядок создания собственного corpus, authored evaluation workflow,
validator, evidence и assessment описан в
[evaluation authoring guide](../../../docs/73-evaluation-authoring-guide.md).
Этот каталог является полным рабочим примером того контракта.

`validator/` is the portable post-run oracle for the `mini-du` flow cases. It
reads one `takt-evaluation-validator/v1alpha1` request from stdin and returns
one `takt-validation/v1alpha1` result. Product failures are measured as
`valid:false`; malformed requests or an unavailable host `du` oracle exit 2.

The candidate contract is `mini-du [-s] [-k] [-H] [-h|--help] [--] PATH...`.
Default numeric output and `-k` both use integer kibibytes matching host
`du -k`; `-k` is an accepted alias, not a different unit. `-H` uses binary
humanized units (`0B`, one decimal below 10, rounded integers from 10). The validator builds the candidate,
rejects delegation to host `du`, compares fixed filesystem scenarios against
host `du -k`, persists bounded normalized candidate/oracle output in mismatch
diagnostics, checks help/option behavior and the humanized oracle, and checks
the declared path/artifact/SCM requirements. Exact diagnostic prose is not part
of the public contract: failure scenarios require the expected exit code,
clean stdout where specified and a non-empty stderr diagnostic. Missing
`implementation.md` is reported as `missing_artifact`, and the workflow requires
that artifact to be a non-empty regular file and retries
implementation before starting review when that record is absent.
Copy exactly one host-specific config example to `config.yaml` before running
the suite; the generated file is intentionally ignored by git.
The Pi example supplies native `settings.httpIdleTimeoutMs`; evaluation copies
the complete `settings` object into its isolated `.pi/settings.json` and then
forces Pi/SDK retries off so durable retries remain owned by Takt.

Validator version 3 added `hardlink_multiple` (cross-argument inode de-duplication)
and `double_dash_default` (bare `--` selects the default path) to the corpus.
Version 4 checks product behavior before missing delivery artifacts, while
artifact inspection failures remain infrastructure errors.
Evaluation generations are not interchangeable: registry-old runs are generation
v0, the corrected-registry/validator-2 baseline is generation v1, and validator-3
runs are generation v2. Validator-4 runs are generation v3. Do not compare trends
across generations.

## Reading `eval stats`

One Run is one isolated `case + repeat` execution. Node retries and resumes are
attempts inside that Run. For example, three cases run with `--repeat 3`
produce nine Runs.

`Node attempts` is the sum of executions of all workflow action nodes,
including deterministic nodes. `Assistant executions` counts only actual
assistant execution records. `ASSISTANT STEPS` shows their model, tokens and
wall time from first `node.started` to the terminal node event. Wall time
includes tool calls, retry backoff and waiting; it is not provider-only
inference latency. Reports created before node timing was added show `-`.
`ASSISTANT SESSIONS` contains the full Session ID for every recorded assistant
execution, with workflow/provider attempt and `fresh` or `resume` mode. Use the
ID with the corresponding assistant's own inspection tools; Takt does not
invent a provider URL.

Live `--trace` output uses `SCOPE | EVENT | DETAILS`. Repeated assistant events
start with `RUN <short-id> · NODE <id>#<attempt>`; the full Run ID is printed at
acceptance and a Session ID only when first observed. Tool lines therefore keep
the action and command together without repeating stable run/model/session
fields in the middle of every message.
An active-node heartbeat includes `context=<N>t` when the assistant exposed
input tokens for its last completed model request, otherwise
`context=unknown`. This is an observed request size, not cumulative billed
usage or the model's maximum context window.

During a long run, use `make eval-status RUN=<eval-dir>` in another terminal for
a compact current case, phase, Run/node progress and measured persisted usage.
It reads `<eval-dir>/progress.json` only and cannot launch or resume a model.
After completion, use `make eval-stats RUN=<eval-dir>` for the final report.
Its `FAILURES` section attributes the saved validator/runtime/node cause. For a
case-level evidence inventory, run `make eval-inspect RUN=<eval-dir>` or add
`CASE=<id> REPEAT=<n>`. Inspection reads saved files only: it cannot launch a
model, resume the flow or change the validator verdict. `CAUSAL CHAIN` explains
saved assistant limits/tool activity, empty results, validation failures and
skipped downstream nodes instead of only repeating the validator diagnostic. New runs preserve
filtered redacted tool-start inputs in `activity.json`; messages and tool output
are intentionally excluded.

- `Flow valid` is `true_accept / evaluated runs`: the workflow completed and
  the validator accepted the product.
- `Completion` is `completed runs / all runs`, regardless of product quality.
- `False accept` is `completed + invalid / evaluated runs`.
- `False reject` is `not completed + valid / evaluated runs`.
- `Validation errors` is `runs without a usable validator result / all runs`.

Runs classified as `infrastructure_error` remain visible in usage and
diagnostics but are excluded from evaluated quality rates. With only one Run,
`100%` means `1/1`; use multiple cases and repeats to compare stochastic
models. See [Production flow evaluation](../../../docs/13-evaluation-plan.md#production-flow-evaluation)
for the complete outcome table and denominator rules.
