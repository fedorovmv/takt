# Test Suite Tiering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Separate the fast developer check from the complete E2E and race suite without removing any existing test target.

**Architecture:** Keep package selection in Make variables derived from `go list`, excluding only the black-box `tests/e2e` package for core targets. Compose `check` from one core run plus one ordinary E2E run; expose the previous broad release gate as `check-full` and make `scripts/verify.sh` use the explicit full targets.

**Tech Stack:** GNU Make, Go test, existing TypeScript smoke script, Markdown documentation.

---

### Task 1: Tier Make targets

**Files:**
- Modify: `Makefile:1-18,185-196`

- [x] Define `GO_ALL_PACKAGES` from `go list ./...` and `GO_CORE_PACKAGES` by removing only the `/tests/e2e` package.
- [x] Make `test` and `race` delegate to core package sets; add `test-all`, `race-all`, and `e2e-race`.
- [x] Keep `e2e` as the ordinary full E2E target and remove `journeys` from `check` composition.
- [x] Add `check-full` as the complete format/vet/test-all/race-all/build/smoke gate.

### Task 2: Align release verification

**Files:**
- Modify: `scripts/verify.sh:1-20`

- [x] Replace duplicated package commands with `make test-all`, `make journeys`, and `make race-all` while retaining the script's build, smoke, and example validation steps.
- [x] Keep the script independently executable and preserve `GO_TEST_P` forwarding.

### Task 3: Update developer and release documentation

**Files:**
- Modify: `DEVELOPMENT.md:76-99`
- Modify: `docs/archive/releases/67-go-native-test-architecture-v0.1.53.md:76-92`
- Modify: `README.md` test/release-gate references
- Modify: `docs/05-implementation-status.md` current test-gate status
- Modify: `CHANGELOG.md` under `Unreleased`
- Modify: `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md` current verification notes

- [x] Document the fast/full target split and the fact that live evaluations stay opt-in.
- [x] Replace claims that `make check` always includes journeys and race with the new target names.
- [x] Record the reason: process-heavy E2E is run once in the fast check and again only in the explicit full gate.

### Task 4: Verify target behavior

- [x] Run `make -n check`, `make -n check-full`, and inspect the equivalent script composition; confirm target composition.
- [x] Run `make check`; it passed in `391.40s`, with the ordinary E2E taking `290.661s`.
- [x] Run focused `go test ./internal/architecture ./internal/tooling/evaluation -count=1` plus `git diff --check`.
- [x] Run `make check-full`; it passed in `676.98s` (ordinary E2E `290.711s`, journeys `3.345s`, race E2E `316.322s`).
