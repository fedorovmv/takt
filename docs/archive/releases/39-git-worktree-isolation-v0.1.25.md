# Git worktree isolation in v0.1.25-alpha

> Historical note: `v0.1.25-alpha` activated a selected structural subworkflow at a compiled gate. Since `v0.1.26-alpha`, the smart router starts the selected process as a governed child Run, which applies its own worktree policy. The worktree lifecycle described below remains valid.

## Goal

Takt is currently a local trusted runtime, but code-changing workflows still need an execution boundary. A Run must be able to modify, test, and commit a dedicated branch without changing the checkout from which the user started Takt.

This slice adds managed Git worktrees as a runtime feature. Server, Web UI, and database storage remain proposals because local single-user operation does not require them yet.

## Workflow policy

```yaml
worktree:
  enabled: true
  base: HEAD
  branch_prefix: takt
  cleanup: on_success
  allow_dirty: false
```

`enabled` moves node execution into a branch and worktree created for the Run. State, events, locks, and artifacts remain under the control workspace.

`cleanup` accepts:

- `on_success`: remove a clean successful worktree and delete its branch only when the branch still points to the recorded base commit;
- `manual`: retain it until an explicit CLI removal.

A failed, cancelled, waiting, dirty, or uninspectable worktree is retained. Takt never discards uncommitted work automatically.

## Router-aware isolation

The `code` router itself runs in the control checkout. The selected governed child applies its own worktree policy before its first node. Once a structural dynamic gate activates a worktree, the remainder of that Run uses the execution workspace; switching is Run-scoped, not container-scoped. This keeps routing auditable without applying the wrong isolation policy to every branch.

Mutating workflows such as feature development, issue fixing, refactoring, architecture changes, Ralph, and Remotion generation enable isolation. General assistance, current-PR reviews, issue creation, validation, and conflict resolution stay in the live checkout because they either do not mutate code or depend on the checkout's current branch/conflict state.

Direct selectors such as `code:feature-development` apply the same policy at Run start.

## CLI

```bash
takt run code:feature-development --input PLAN.md
takt run code:feature-development --worktree-base origin/main
takt run code:feature-development --keep-worktree
takt run code:feature-development --no-worktree
takt run code:feature-development --allow-dirty-worktree

takt worktree list --workspace .
takt worktree remove <run-id> --workspace .
takt worktree remove <run-id> --workspace . --force
takt worktree prune --workspace .
```

`--allow-dirty-worktree` does not copy uncommitted files. It explicitly starts from the resolved committed base while recording that the control checkout was dirty.

## Stored state

Run state records the control and execution workspace, repository root, worktree path, branch, base revision and commit, cleanup policy, dirty state, removal status, and retention or cleanup error. CLI overrides are also persisted so resume preserves the original isolation decision.

Definitions and bundled Markdown commands remain authoritative from the control checkout. Execution-worktree project commands are only a fallback. This prevents definition fingerprints from changing merely because node execution moved to another checkout.

## Reliability fixes included in the slice

- `output_format` normalizes only `NodeState.output`; raw provider stdout remains unchanged for diagnostics;
- native retry can target `protocol` errors and passes the exact schema validation error through `${feedback}`;
- the router uses one retry mechanism instead of combining attempts and a failing hook;
- approved `interactive-prd` content is no longer revised on the `ready` iteration;
- `create-issue` reports malformed reproduction results and still runs its summary branch;
- parallel waves persist both active `current_nodes` and an explicit completion transition;
- integer validation remains exact beyond the IEEE-754 safe range;
- the full review catalog now uses `foreach.parallel` for five review perspectives.

## Deliberate boundaries

Per-node tool policies, MCP/skills and assistant-enforced sandbox were added in `v0.1.27-alpha`; dynamic child fan-out and parallel governed children were added in `v0.1.28-alpha`; script nodes remain the active implementation gap. Governed child Runs and cancellation were added in `v0.1.26-alpha`.

Server, Web UI, database storage, remote workers, and message adapters remain proposal-level extensions. They become relevant only if Takt moves beyond local trusted execution; that move requires a separate threat model, authentication, secret handling, and multi-user persistence contract.

## Path canonicalization

Before comparing the requested workspace with `git rev-parse --show-toplevel`, Takt resolves both paths through `filepath.EvalSymlinks`. This is required on macOS, where temporary paths commonly traverse `/var` → `/private/var`, and for any user workspace reached through a symbolic link.
