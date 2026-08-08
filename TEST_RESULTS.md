# Takt v0.1.44-alpha — release verification

Release slice: **Runtime Reliability & Local Security** plus the accepted review fixes for `v0.1.43-alpha`.

## Baseline / ordinary Go gate

From the final working tree:

```text
gofmt                                  PASS
go vet ./...                           PASS
go test ./... -count=1                 PASS
go build ./...                         PASS
20 JSON schemas parse                  PASS
scripts/check-docs.sh                  PASS
```

The ordinary full test run covers all Go packages, including the new `diagnostic`, `redact` and `localsandbox` packages and the updated runtime/control/workspacecatalog behavior.

## Race verification

A single aggregated:

```text
go test -race ./... -count=1
```

was attempted. The external command harness terminated the long aggregate after packages through `internal/rolecontract` had reported PASS. It did **not** report a Go test failure or race detector finding.

The packages not reported before that external timeout were then run explicitly and all passed:

```text
internal/runtime                       PASS
internal/store                         PASS
internal/taskroute                     PASS
internal/validation                    PASS
internal/workflow                      PASS
internal/workspacecatalog              PASS
internal/yamlmini                      PASS
sdk/agentadapter                       PASS
sdk/domainadapter                      PASS
```

After the final public-state redaction change, the changed/security-critical set was run again under race and all passed:

```text
internal/diagnostic                    PASS
internal/redact                        PASS
internal/localsandbox                  PASS
internal/runtime                       PASS
internal/control                       PASS
internal/workspacecatalog              PASS
internal/dynamicplan                   PASS
internal/packagedist                   PASS
cmd/takt                               PASS
```

Therefore the release does not claim that one uninterrupted aggregate race command completed in this sandbox; it records the aggregate timeout and the successful package-level coverage instead.

## v0.1.43 review regressions

Verified by permanent tests / scripts:

```text
macOS logical/physical path discovery (EvalSymlinks)       PASS
repository child resolved workspace comparison             PASS
python3 with python fallback in multi-repo E2E             PASS
real topological repository merge order                    PASS
explicit Workspace with empty repositories rejected        PASS
Git/local source allowlist path/repository boundary         PASS
adapter doctor negative exit                               PASS
multi-repo integrity deny path                              PASS
replanner repository payload                               PASS
repository fingerprint drift                               PASS
dependency_results in TaskBrief                            PASS
workflow.repository node rules                             PASS
completed child reuse on parent retry                      PASS
```

GitHub Actions keeps both `ubuntu-latest` and `macos-latest` in the CI matrix.

## Runtime Reliability & Local Security

`scripts/test-runtime-reliability-security.sh` — **PASS**.

The focused contract verifies:

- durable retry deadline/backoff and stable diagnostic fingerprints;
- timeout as an explicit retry kind;
- `secret://ENV_NAME` resolution and persistence redaction;
- explicit short secrets are redacted;
- foreground `control.Start` returns the durable/redacted state rather than live in-memory state;
- textual artifact redaction and fail-closed binary artifact handling;
- required OS sandbox cannot be bypassed by `runtime: validation`;
- canonical `NodeState.path`;
- fan-out `one_success` / `all_success` early termination with `fanout_result_decided`;
- completed governed child reuse on parent post-check retry;
- macOS/symlink workspace discovery regression;
- package source/signature negative cases;
- multi-repo deny/replanner/fingerprint regressions;
- deterministic repository merge order;
- `adapter doctor` negative result.

## Existing end-to-end / contract suites

The following existing contracts passed after the v0.1.44 changes:

```text
fake assistant protocol                  PASS
Pi adapter                               PASS
OpenCode adapter                         PASS
Route DSL end-to-end                     PASS
Route DSL evaluation/isolation           PASS
workflow composition                     PASS
Takt authoring skill                      PASS
code profile                              PASS
Git worktree                              PASS
governed child Runs                      PASS
node capability policies                 PASS
governed child fan-out                   PASS
script / typed artifacts                 PASS
local MCP                                 PASS
external executor                        PASS
deep code workflows                      PASS
authoring                                PASS
daemon                                   PASS
Dynamic Takt                             PASS
trusted block packages                   PASS
host control                             PASS
host integration TypeScript              PASS
autonomous Run operations               PASS
Simple Reliable Router                   PASS
Evidence / baseline / failure routing    PASS
Adapter Platform                         PASS
Portable Package Distribution            PASS
Multi-repo Dynamic Workflows             PASS
Runtime Reliability & Local Security     PASS
agent-adapter conformance                 PASS
documentation                            PASS
```

Some very long make target chains were terminated by the surrounding command harness after earlier targets had passed. The interrupted targets (`OpenCode` and `multi-repo`) were rerun directly and passed; later targets were run in smaller groups. No contract FAIL was hidden behind those external timeouts.

## Security boundary verified for this release

`v0.1.44-alpha` remains a local single-user trusted runtime.

- Assistant `sandbox.filesystem/network` remains an adapter capability contract.
- OS enforcement is available only for deterministic `bash/script` nodes through `bwrap` on Linux or `sandbox-exec` on macOS when available.
- `required` fails before payload execution when no backend is available; `optional` records `degraded`.
- Takt is not a secret store. SecretRef values are resolved from the environment for execution, then known values are redacted at persistence boundaries.
- Worktrees, package signatures and path checks are separate hardening layers and do not make arbitrary workflows/packages untrusted-safe.

See `SECURITY.md`, ADR-070/071 and `docs/58-runtime-reliability-local-security-v0.1.44.md` for the exact boundary.
