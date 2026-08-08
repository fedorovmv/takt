# Takt v0.1.50-alpha — test results

Release verification was performed from the working tree and repeated from a clean extraction of the final release archive. The current execution environment is Linux; macOS-specific regressions are included but this report does not claim a fresh macOS run.

## Working tree

PASS:

- `gofmt` / `go vet ./...`;
- `go test ./... -count=1`;
- `go build ./...`;
- all 34 JSON Schema files parse and the offline schema registry contract passes;
- changed-package race contour: runtime, control, assistant, evaluation, schema subset, workflow, domain/task-source transports, compatibility, reference adapters, public SDKs and CLI;
- iteration history, structured task sources, reference adapters, Adapter Platform, package distribution, multi-repo, runtime/security, compatibility, Route DSL strategy benchmark and task-level benchmark;
- MCP, external executor, deep workflows, authoring, daemon, Dynamic Takt, trusted block packages, host control, autonomous runs, simple router and evidence routing.

The single aggregate `go test -race ./... -count=1` run was stopped by the external command timeout after packages through `internal/rolecontract` had passed. No test failure was observed. The remaining package tail was then run separately under `-race` and passed, including runtime/store/workspace/workflow/reference/SDK packages. This report therefore does not claim one uninterrupted aggregate race PASS.

## Review debt closed in v0.1.50

- durable loop history resumes after the last completed iteration and remains bounded at 64 across operator retry;
- `foreach` and governed child workflow inside `loop_group` have direct regressions; old state without `loop_iterations` remains readable;
- task-evaluation excludes `.git` from identity/copy and reconstructs a clean Git baseline for cases that require worktree isolation;
- redaction fails closed on an unreadable per-run config;
- `input.schema` and `output_format` share behaviorally tested `takt-schema-subset/v1`; unsupported keywords fail closed;
- all published schemas form an offline registry with local `$ref` only;
- Pi/OpenCode probes, Domain Describe and reference GitHub commands have bounded timeouts;
- MCP Domain Adapter env resolves `secret://`; Qwen-side budget exit is normalized as `timed_out`;
- GitHub SCM macOS path expectation is canonicalized and release garbage such as `version.go.tmp` is rejected by manifest verification.

## v0.1.50 feature contract

`structured task sources: PASS`: the GitHub Issue fixture is resolved through `takt-task-source/v1alpha1`, normalized into a Task with immutable revision/acceptance data, passed through the ordinary `task start`/MCP Task API, and exposed to Router/Planner/Replanner without a second runtime.

## Clean release archive

The release ZIP was extracted to a new directory. From that extraction the following passed: `verify-manifest`, no `bin/`, `gofmt`, `go vet ./...`, full `go test ./... -count=1`, `go build ./...`, the 34-schema offline registry, docs, Structured Task Sources, iteration history, reference adapters, Adapter Platform, package distribution, multi-repo, runtime/security, compatibility, Route DSL strategy benchmark and task-level benchmark. The changed-package race contour also passed from the extracted archive; two long combined race invocations were split after the external command wrapper timed out between package groups.
