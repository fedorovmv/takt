# Takt v0.1.55-alpha — TEST RESULTS

Release theme: **Core Stabilization & Modularization**. No product capability was intentionally removed and no new workflow/runtime feature was added.

## Architecture / modularization

PASS:

- `internal/application` contains the stable Run-facing use cases and does not import `internal/experimental`, `internal/extensions` or `internal/tooling`;
- Dynamic Flow / Router / Dynamic Plan / Evidence / Host Control / Learning are under `internal/experimental`;
- Package Distribution / Block Catalog / Notifications are under `internal/extensions`;
- evaluation / compatibility are under `internal/tooling`;
- `profile` no longer imports package distribution; installed package manifests are merged by `internal/catalogload` above the stable boundary;
- production composition remains in `internal/bootstrap`;
- architecture tests reject reverse stable -> experimental/extensions/tooling imports and the return of `internal/yamlmini`.

Production Go LOC after the split (tests excluded):

| Area | LOC |
| --- | ---: |
| `internal/application` | 3,493 |
| `internal/experimental` | 5,639 |
| `internal/extensions` | 2,681 |
| `internal/tooling` | 3,184 |
| `internal/runtime` | 5,164 |
| `internal/yamlcodec` | 228 |

The stable application package was reduced from about 6.8k LOC in v0.1.54 to 3.5k LOC by moving independent modules, not by deleting their functionality.

## User journeys

`make journeys` uses the real `takt` binary through the Go black-box harness. PASS:

1. `init -> validate -> run -> status/events/artifacts`;
2. approval -> `answer` -> continue;
3. failed Run -> `retry` -> completed;
4. reusable `subworkflow`.

The README quick start was changed to the same stable path. Dynamic Flow and evaluation are no longer prerequisites for the first user experience.

## YAML dependency

The handwritten `internal/yamlmini` parser was removed. Production source imports:

```text
go.yaml.in/yaml/v3 v3.0.4
```

`internal/yamlcodec` keeps only Takt-specific contract behavior: canonical JSON field names, strict unknown-field diagnostics, suggestions, JSON-shaped normalization and the common YAML/JSON decode path.

### Release-environment limitation

This execution environment has no outbound DNS and did not have `go.yaml.in/yaml/v3 v3.0.4` in its Go module cache. A direct offline production-dependency check therefore reports:

```text
go: downloading go.yaml.in/yaml/v3 v3.0.4
module lookup disabled by GOPROXY=off
```

For local verification only, the test commands used an **external temporary compatibility module** at the same import path. It preserves the previous YAML behavior so the modular refactor and all existing contracts can be regression-tested in this sandbox. The temporary module is outside the project, is not included in the release archive, and the final `go.mod` contains **no `replace` directive**.

Consequently:

- all Takt regression results below are factual;
- direct execution against the downloaded upstream v3.0.4 module was **environment-blocked here**, not claimed as PASS;
- normal Go/CI environments resolve the exact production dependency from `go.mod`/`go.sum`; `.github/workflows/ci.yml` runs `make check` on Linux and macOS and will exercise the actual module.

## Verification results

Using the final source tree plus the temporary external compatibility dependency described above:

```text
gofmt                         PASS
go vet ./...                  PASS
go test -p 8 ./... -count=1  PASS
go build ./...                PASS
architecture gate             PASS
documentation gate            PASS
make journeys                 PASS
TypeScript host smoke         PASS
```

Full race run:

```text
go test -race -p 8 ./... -count=1  PASS
```

This includes stable application/runtime, all experimental/extensions/tooling packages, CLI/daemon/MCP/appapi, reference adapters/SDKs and `tests/e2e`.

## Release scope

External contracts intentionally retained:

- `takt/v1alpha1` Workflow/Config contracts;
- durable Run state/event semantics;
- CLI and MCP operation compatibility;
- assistant/domain-adapter/task-source public SDKs;
- Dynamic Flow state/API compatibility, now explicitly experimental;
- evaluation/compatibility/learning/package functionality.

The release changes stability boundaries and dependency direction, not the intended behavior of those capabilities.

## Clean-archive candidate verification

A candidate ZIP was unpacked into a new directory before final packaging.

Before any sandbox-only dependency substitution:

```text
VERSION                         0.1.55-alpha
Takt skill                      0.37.0
bin/                            absent
MANIFEST                        PASS (615 files)
documentation                   PASS
go.mod replace directives       absent
```

With the same external temporary YAML compatibility module used only to compensate for this sandbox's missing network/module cache:

```text
go vet ./...                    PASS
go test -p 8 ./... -count=1    PASS
go build ./...                  PASS
make journeys                   PASS
changed modular contour -race   PASS
TypeScript host smoke           PASS
```

A second full `go test -race -p 8 ./...` from the clean directory was stopped by the external execution limit during repeated race compilation, so a clean-directory aggregate race PASS is **not** claimed. The identical final source tree had already completed one uninterrupted full aggregate race PASS before packaging, and the entire changed modular/E2E contour completed race PASS again from the clean extraction.
