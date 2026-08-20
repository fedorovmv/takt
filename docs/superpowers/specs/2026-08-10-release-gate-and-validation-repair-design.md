# Release Gate and Validation Repair Design

## Goal

Restore the `v0.1.57-alpha` release gate and make the existing strict authoring boundary reject trailing workflow documents and malformed quoted `when` literals without expanding the public workflow language.

## Scope

- normalize `go.mod` and `go.sum` with the Go toolchain;
- align the folded YAML scalar regression with `go.yaml.in/yaml/v3 v3.0.4` behavior;
- require exactly one top-level JSON or YAML document in `internal/yamlcodec`;
- reject unterminated, mismatched, or otherwise partial quote delimiters in `internal/whenexpr`;
- install pinned TypeScript `5.7.2` in GitHub Actions and require the existing TypeScript smoke during `make check`;
- update release status, changelog, test results, and the release manifest.

Historical release documents and the filesystem Store implementation are explicitly out of scope.

## Architecture

`internal/yamlcodec.Unmarshal` remains the single YAML/JSON authoring entry point. JSON continues to use `encoding/json`; YAML continues to use `go.yaml.in/yaml/v3`. Each decoder reads the first value and then performs one additional decode that must return `io.EOF`. No parser, format abstraction, or dependency is added.

`internal/whenexpr` remains the only implementation of the bounded `when` language. Its existing literal parser will require any quote character to form a matching outer pair. Quoted contents keep their current literal semantics; escape syntax and new operators are not introduced.

GitHub Actions will install the already-declared TypeScript version, `5.7.2`, and set `TAKT_REQUIRE_TYPESCRIPT=1` for the existing release-gate step. Local `make check` remains usable without Node and may continue to report the smoke as skipped.

## Error Semantics

- a second JSON or YAML value is rejected as multiple documents;
- invalid trailing bytes retain the underlying decoder diagnostic;
- malformed quoted `when` literals fail workflow validation before Run creation;
- CI fails when the pinned TypeScript compiler cannot be installed or the integration sources do not compile.

## Test Strategy

1. Add YAML codec regressions for a second JSON value, a second YAML document, and invalid trailing JSON; run them red before changing production code.
2. Add `whenexpr` regressions for unterminated and mismatched quotes; run them red before changing production code.
3. Correct the existing folded-scalar expected value to the behavior of the pinned upstream YAML module.
4. Extend the architecture test to require the CI TypeScript enforcement flag and pinned compiler version.
5. Run focused package tests after each minimal implementation.
6. Run `gofmt`, `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`, docs checks, manifest verification, and the full `make check`/`scripts/verify.sh` gates.

## Documentation and Release Metadata

The repair does not change `takt/v1alpha1`; it enforces the already documented strict authoring contract. `CHANGELOG.md`, `docs/05-implementation-status.md`, and `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md` will record the repaired gate and regressions. `MANIFEST.sha256` will be regenerated after all tracked changes are final.
