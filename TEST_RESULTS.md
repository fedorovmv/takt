# Test results — v0.1.27-alpha

## Environment

| Field | Value |
|---|---|
| Date | 2026-08-05 |
| OS used for executable tests | Linux 6.18.35, amd64 |
| Go | go1.23.2 linux/amd64 |
| Git | 2.47.3 |
| Bash | 5.2.37 |

## Release gate results

The following commands completed successfully in the working tree:

```text
gofmt -w cmd internal                         PASS
go vet ./...                                  PASS
go test ./... -count=1                        PASS
go test -race ./... -count=1                  PASS
go build -o bin/takt ./cmd/takt               PASS
scripts/test-fake-assistant.sh                 PASS
scripts/test-pi-adapter.sh                     PASS
scripts/test-opencode-adapter.sh               PASS
scripts/test-route-dsl-e2e.sh                  PASS
scripts/test-route-dsl-eval.sh                 PASS
scripts/test-composition.sh                    PASS
scripts/test-takt-skill.sh                     PASS
scripts/test-code-profile.sh                   PASS
scripts/test-worktree.sh                       PASS
scripts/test-child-runs.sh                     PASS
scripts/test-policies.sh                       PASS
scripts/check-docs.sh                          PASS
scripts/verify.sh                              PASS
```

A single monolithic `make check` was attempted twice. The first invocation was terminated by the execution wrapper during the Pi contract after unit/race/fake suites had passed. The second reached the OpenCode contract and the surrounding `make` process ended with `wait: No child processes` in this container. No project test failure was printed. Every unchanged target from `make check` was then executed directly and completed successfully as listed above. This report does **not** describe either interrupted monolithic invocation as PASS.


## Clean archive verification

The release ZIP was unpacked into a new temporary directory. `MANIFEST.sha256`, all three version files, `go test ./... -count=1`, and `go test -race ./... -count=1` passed from the unpacked tree. A monolithic `scripts/verify.sh` invocation was stopped by the execution wrapper during its repeated race phase. Its remaining unchanged commands were then executed from the same clean directory in two groups: assistant contracts, followed by Route DSL/composition/skill/profile/worktree/child-run/policy/docs/example validation. Both groups completed successfully (`CLEAN_VERIFICATION_PASS`).

## Review regressions

```text
macOS-style symlinked workspace test, 20 repetitions    PASS
Darwin/amd64 gitworktree test binary cross-compilation  PASS
hidden-node usage aggregation                           PASS
cancel failed/completed/cancelled Run rejection         PASS
governed recursion rejected by takt validate            PASS
empty worktree branch cleanup                           PASS
explicit allowed_tools: [] / skills: []                 PASS
child policy upper-bound inheritance                    PASS
adapter capability preflight before invocation          PASS
Pi tool/skill policy mapping                             PASS
OpenCode permission/MCP/path-skill mapping               PASS
```

The symlink regression uses a real symbolic link and verifies that `Prepare` compares physical paths after `filepath.EvalSymlinks`. It covers the `/var` versus `/private/var` class of failure reported on macOS.

## OS matrix

| Platform | Current result |
|---|---|
| Linux amd64 | Full executable suites above: PASS |
| macOS | Not executed in this container. `.github/workflows/ci.yml` now runs full `make check` on `macos-latest`; the Darwin test binary cross-compiles and the symlink regression passes on Linux. A real macOS CI result is still required before claiming macOS PASS. |

## External smoke tests

Real Pi and OpenCode smoke tests were not run because this environment does not contain user credentials, provider configuration and target models. Fake Pi/OpenCode/process contract suites were run.

Real GitHub mutations, MCP servers and network sandbox enforcement were not exercised. Node policy tests use local fake adapters and static MCP configuration. Filesystem/network fields in this release are assistant-enforced contracts, not an OS sandbox.
