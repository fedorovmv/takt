# Flow Gate Matrix Tiering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move deterministic artifact/verdict combinations out of expensive full-flow E2E while preserving one end-to-end proof per branch.

**Architecture:** Shared executable profile tools own regular-artifact and strict-verdict checks. The feature workflow calls those tools; `internal/profile` runs the complete matrix directly, while `tests/e2e` keeps representative scheduler/outcome cases.

**Tech Stack:** POSIX shell profile tools, Go contract tests, existing YAML workflow and Go E2E harness.

---

### Task 1: Add shared profile gate tools

**Files:**
- Create: `internal/profile/builtin/code/tools/require-artifacts`
- Create: `internal/profile/builtin/code/tools/require-verdict`
- Modify: `internal/profile/builtin/code/workflows/feature-development.yaml`

- [x] Add fail-closed regular-file checks and strict verdict parsing.
- [x] Replace duplicated YAML snippets with tool calls without changing node IDs, outputs, or branches.

### Task 2: Move deterministic matrices to profile contracts

**Files:**
- Create: `internal/profile/profile_gates_test.go`
- Modify: `tests/e2e/core_contracts_test.go`

- [x] Cover valid and missing/empty/directory artifact forms for implementation, repair, revalidation, PR, URL, and summary artifacts.
- [x] Cover valid PASS/REPAIR/BLOCKED, evidence text, missing, malformed, duplicate, NUL, and typo verdicts.
- [x] Assert the installed feature workflow calls the shared tools.

### Task 3: Reduce redundant full-flow matrices

**Files:**
- Modify: `tests/e2e/evaluation_contracts_test.go`

- [x] Keep branch and representative failure cases while reducing parser/artifact loops to one scenario each.
- [x] Remove the redundant implementation artifact full-flow matrix; the profile tool matrix owns file-shape coverage.

### Task 4: Verify and measure

- [x] Run shell syntax checks and focused profile contracts.
- [x] Run representative feature-flow tests (`45.564s`) and full `tests/e2e` (`182.49s`, down from `290.661s`).
- [x] Run final `make check` (`267.39s`, ordinary E2E `182.723s`), update measured results, and run race-focused profile/E2E verification (`internal/profile` `2.858s`, feature E2E `82.450s`).
