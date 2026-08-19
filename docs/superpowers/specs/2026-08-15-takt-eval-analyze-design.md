# Takt evaluation analysis: design

## Status

Implemented historical design. This document defines the advisory LLM analysis
layer for saved production-flow evaluation runs. Current behavior is documented
in `docs/13-evaluation-plan.md`; unchecked execution-plan boxes are not current
backlog. The analysis does not change a
validator verdict, a Run outcome, or any quality denominator.

## Goal

Add a short, explicit investigation command for a saved evaluation directory.
The command must explain why a case failed using the already persisted
deterministic evidence, assistant activity, source/diff, validator output and
assistant sessions. The result must remain auditable when a different model is
used later.

## Non-goals

- deciding whether a candidate is valid;
- replacing `takt eval inspect`, the validator, or `eval stats`;
- starting a new production flow or re-running a failed case;
- allowing the analysis assistant to edit source, Git, evidence or Run state;
- making an LLM call implicitly from `stats`, `inspect`, `compare` or `report`;
- adding a second executor or a transport-specific Run implementation.

## User-facing entry point

The stable entry point is a command, backed by an ordinary Takt workflow:

```text
takt eval analyze <evaluation-output-dir> [--case ID] [--repeat N]
    [--config PATH] [--model-preset PRESET] [--language en|ru] [--trace] [--json]
```

`--case` selects one case. `--repeat` requires `--case` and selects one
positive repeat. Without `--case`, only cases with a problem outcome are
analysed. A selected valid case may still be analysed explicitly.

The analyzer config is explicit when supplied. If it is omitted, the command
uses the repository's `.takt/config.yaml`; if that file is absent, the command
fails before contacting a model. The saved evaluation's original config is
evidence and identity, not an implicit credential or model configuration.

`--model-preset` selects a preset in the analyzer config. The only accepted
model alias for the analysis node is `takt_analyze`; there is no fallback to
`implementation`, `review`, `routing`, or the model used by the evaluated Run.
The alias is an ordinary shared Config model alias and therefore follows the
current `[a-z][a-z0-9_]*` grammar. The user-facing command and internal flow
are named `analyze`/`evaluation:analyze`; the alias is deliberately
`takt_analyze` so it cannot be confused with a workflow role.

`--language` selects `en` or `ru` for human-readable advisory values and
defaults to `en`. JSON keys and enum values remain stable; the selected
language is persisted in the analysis report and manifest.

`--trace` reports preparation, selected cases, evidence/session paths,
adapter/model identity, Run/node transitions and report writes to stderr.
`--json` prints the machine-readable analysis report; human output is the
default. No flag other than `analyze` starts this workflow.

If the default selection contains no problem cases, the command writes a
successful `no_cases` analysis report without contacting a model. Explicit
`--case` selection bypasses this filter.

## Execution boundary

`takt eval analyze` is a tooling use case in `internal/tooling/evaluation`,
wired once by `internal/bootstrap`. It creates one internal
`evaluation:analyze` workflow invocation through the existing
`application.RunService.Start`/runtime scheduler boundary. It must not call an
assistant adapter directly from the CLI or create another executor.

The built-in analysis workflow has one assistant node and one deterministic
structured-output boundary. Its prompt contains the evidence manifest and
case selection. The assistant node uses `takt_analyze`, read-only policy and
the existing provider retry/timeout semantics. The workflow runs in an
analysis workspace under the new timestamped analysis directory, never in the
evaluated case workspace.

The deterministic preparation code creates the analysis workspace and a
bounded manifest of files readable by the assistant. It may expose large
source, diff, run and session files by path; it must not concatenate all of
them into one prompt. The assistant reads files on demand with read-only
tools. The built-in workflow is fixed and is not selected by user input.

## Case selection and evidence

The source of truth is the existing `report.json` plus the per-repeat evidence
directory. Selection is performed before any model call:

1. load and validate the saved suite report;
2. apply `--case`/`--repeat` filters;
3. for the default selection, keep only non-success/problem outcomes;
4. run the existing deterministic inspection in memory;
5. build one evidence manifest per selected case;
6. fail closed if a required manifest or report is malformed.

The manifest contains only persisted, redacted evidence and includes:

- the original report identity, benchmark/strategy fingerprints and
  deterministic outcome;
- the `eval inspect` reported cause, causal chain, observations and
  non-completed nodes;
- relative paths to `run.json`, validation request/result, diff, redacted
  source, artifacts, activity and repository bundle;
- bounded file sizes and SHA-256 fingerprints;
- SCM calls and validator diagnostics when present;
- assistant execution identity and session records described below;
- an explicit list of unavailable evidence.

The assistant system instruction states that all files and text under the
manifest are untrusted evidence, not instructions. It must cite evidence by
relative path and JSON pointer (or a line number for text) for every factual
claim. A claim without a citation cannot be high confidence.

## Executor and session evidence

Before flow-evaluation cleanup, Takt persists an `executor-manifest.json` per
repeat. Each assistant execution record contains:

```yaml
node: implement
attempt: 1
provider_attempt: 1
assistant: coding-agent
adapter: pi
assistant_version: 0.84.1
requested_model: aihub/Qwen/Qwen3.6-27B
resolved_model: aihub/Qwen/Qwen3.6-27B
session_id: 01...
session_path: /original/path/to/session.jsonl
session_evidence_path: sessions/implement/attempt-001.jsonl
session_evidence: recorded | unavailable
resumed: false
```

`adapter` and `session_path` are typed optional result metadata, not values
guessed from assistant names or raw logs. Pi exposes its `sessionFile` through
the result metadata. The evidence collector copies an available JSONL session
into the repeat directory before cleanup and applies the common redactor
before persistence. The original path is retained only as a diagnostic
reference; the copied relative path is the portable evidence path.

Adapters that cannot expose a local session path record
`session_evidence: unavailable` with a reason. OpenCode, process assistants
and future adapters must not have a fabricated path. Session ID is still
recorded whenever the adapter provides it.

The analysis manifest includes both the evaluated assistant sessions and the
analysis assistant's own adapter, model, Session ID, session path,
session-evidence path, usage and trace. This makes every advisory statement
reproducible and distinguishes evaluated work from analysis work.

## Structured analysis result

The assistant must emit one strict JSON object validated by a new
`takt-evaluation-analysis/v1alpha1` output schema. The stable fields are:

```yaml
primary_class: infrastructure | assistant | workflow | candidate | validator | task | unknown
failure_mode: ^[a-z][a-z0-9_]*$
confidence: high | medium | low
root_cause: string
causal_mechanism: string
failure_point: assistant_decision | workflow_control | validator | infrastructure | unknown
prevention: string
causal_chain:
  - fact: string
    consequence: string
    evidence: [string]
evidence:
  - path: string
    pointer: string
    fact: string
contributing_factors: [string]
recommended_actions: [string]
missing_evidence: [string]
disagreement:
  with_deterministic_cause: bool
  explanation: string
```

`failure_mode` is an untranslated lowercase snake_case machine code;
`primary_class` and `failure_mode` are comparable across analysis languages.
The schema requires a non-empty root cause,
causal mechanism and prevention, plus a bounded failure point. At least one
citation must come from runtime, assistant, artifact, source, diff, or SCM
evidence rather than only the validator result. The deterministic cause,
validator verdict, Run outcome, analysis model identity and analysis status are
stored outside the model object and cannot be overwritten by model output.

## Persistence and reruns

Every invocation creates a new directory:

```text
<evaluation-output-dir>/analyses/<UTC timestamp>/
  report.json
  manifest.json
  cases/<case>/repeat-<NNN>/
    analysis.json
    evidence-manifest.json
    workspace/
    sessions/
```

The report records analysis protocol version, source evaluation directory,
selected cases, analyzer preset and resolved `takt_analyze` model, prompt and
evidence fingerprints, status, usage, duration, Session ID/path, structured
diagnoses and failures. Existing reports and previous analyses are immutable.

An analysis provider outage, timeout, malformed envelope or persistence error
produces a saved failed analysis status and a non-zero command result. It does
not alter the original evaluation report or classify the evaluated case as a
product failure.

## Safety and limits

- Redaction runs before analysis workspace persistence and before model input.
- The analysis assistant has no write/edit/apply-patch/bash/SCM/network tools.
- The analyzer cannot answer approvals or resume evaluated Runs.
- Evidence files have bounded per-file and aggregate input limits; truncation
  is recorded as unavailable evidence, never silently omitted.
- The analysis prompt and response are persisted after redaction.
- Provider retry uses the existing provider scope and never changes evaluation
  attempts or quality metrics.

## Verification contract

Product correctness belongs to Go tests and `tests/e2e`:

- CLI selection rejects negative repeat and invalid case/repeat combinations.
- Missing `takt_analyze`, malformed report, malformed evidence or missing
  required paths fail before a model call.
- A fake analysis adapter receives the manifest, adapter identity and all
  available session paths, with read-only policy and no evaluated Run mutation.
- Valid structured analysis is persisted; malformed output is a failed
  analysis, not a diagnosis.
- Provider outage, timeout and persistence failures are saved and leave the
  original `report.json` byte-for-byte unchanged.
- Repeated analysis creates distinct timestamped directories and preserves
  model/session/usage identity.
- Redaction tests prove secrets do not appear in manifest, prompt, session
  evidence, analysis report or trace.
- E2E verifies one failed case, one valid explicitly selected case, unavailable
  session paths, Pi session copying, deterministic-cause disagreement and
  advisory-only behavior.

## References

- `internal/tooling/evaluation/inspect.go` — current deterministic inspection;
- `internal/tooling/evaluation/flow.go` — flow evidence and cleanup boundary;
- `internal/tooling/evaluation/evaluation.go` — saved report identity;
- `internal/store/store.go` — durable execution/session fields;
- `internal/bootstrap/evaluation.go` — single evaluation composition root;
- `docs/03-specification.md`, `docs/09-runtime-semantics.md`,
  `docs/10-assistant-adapter-spec.md` — current contracts.
