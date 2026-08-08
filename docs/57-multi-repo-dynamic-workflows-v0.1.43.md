# Multi-repo Dynamic Workflows — v0.1.43-alpha

`v0.1.43-alpha` extends Dynamic Takt from one Git repository to a bounded local workspace containing several repositories. The implementation reuses the existing `WorkflowPlan -> Workflow -> scheduler -> governed child Run` path: there is no multi-repo scheduler, second state machine, or provider-specific SCM runtime.

## Workspace catalog

A workspace can declare `.takt/workspace.yaml`:

```yaml
apiVersion: takt/v1alpha1
kind: Workspace
repositories:
  - id: api
    path: api
  - id: client
    path: client
    depends_on: [api]
  - id: service
    path: service
    depends_on: [client]
```

An explicit manifest must declare both `apiVersion` and `kind`. Repository IDs match `[a-z][a-z0-9-]{0,62}`. Paths must stay inside the control workspace after symlink resolution, point to Git repositories, use unique IDs, and form an acyclic dependency graph. If the manifest is absent, Takt performs bounded local discovery: the workspace Git root itself, or immediate child Git repositories. The resolved IDs, paths, dependency edges and repository HEADs form a catalog fingerprint that is persisted in the plan and checked again before execution/replanning.

## Repository-aware plan

A `WorkflowPlan` phase can set `repository` and optionally `publish_change`:

```yaml
- id: api-change
  uses: repository-change
  repository: api
  publish_change: true
  strategy: task

- id: client-change
  uses: repository-change
  repository: client
  depends_on: [api-change]
  publish_change: true
  strategy: task
```

Repository IDs must exist in the catalog. Declared repository dependencies must be reflected by phase dependencies. A repository phase cannot use `map`; the first release also permits at most one mutating phase per repository so a single child Run owns that repository's candidate worktree.

Router, planner and replanner receive the repository catalog together with `adapter_preflight`. Semantic routing proposes repository scope and dependency order; Go validation proves that the proposal references real repositories and is acyclic before it can reach runtime.

## Isolation and cross-repository data

A mutating repository phase compiles to the ordinary governed `workflow` node with:

```yaml
workflow:
  repository: api
  isolation: worktree
```

The child Run receives the selected repository as control workspace and a dedicated managed worktree as execution workspace. Parent state records the child run ID, control/execution workspace, branch and base commit. The worktree is retained through the parent Dynamic Plan so later repository phases and integration verification can inspect the actual candidate; cleanup remains explicit through the existing worktree operations.

Dependency results are passed through normal bounded TaskBrief context. For repository dependencies Takt also exposes the upstream child execution workspace, branch and base commit. The next worker therefore consumes explicit results/artifacts/workspace references, not another agent's hidden conversation history.

## Evidence and change integrity

Each completed repository gets a `RepositoryExecution` entry containing its candidate SHA-256 and an `EvidenceManifest`. Candidate identity uses the same Git-content fingerprint model as single-repository Dynamic Takt. Actual Git changes from every completed repository are aggregated under `<repository-id>/<path>` and compared with the structured `changed_files` declared by repository workers, preserving the post-action integrity gate across repository boundaries.

The plan-level candidate SHA is derived from the sorted set of repository candidate fingerprints. A repository result can therefore be verified independently while the whole plan still gets one final evidence verdict.

## SCM publication and integration verification

`publish_change: true` compiles to an ordinary neutral `adapter` node using `scm/change.create` and existing durable idempotency/reconciliation semantics. Workflows remain independent of GitHub, GitLab or a corporate Git implementation. The record stores the publisher output per repository and a deterministic repository merge order derived from the dependency graph.

`integration-verify` is a read-only trusted block that runs after repository changes and receives their bounded results/workspaces. It produces a normal required check and participates in the existing evidence/deny/repair model.

## Partial continuation and replanning

Completed repository phases stay in `completed_phases`. `PendingPhases` and checkpoint replanning operate only on the unfinished tail, while `repository_executions` is supplied to the replanner. Operator retry still uses the existing dependency closure, so a failure in a downstream repository does not reset already successful independent/upstream child Runs.

## v0.1.42 review hardening included

This release also closes the outstanding Portable Package Distribution review items:

- security-negative tests cover source allowlists, signed-content tampering, untrusted/missing signatures, incompatible Takt requirements and local-source drift;
- local source allowlists use real path boundaries instead of string-prefix matching;
- package dependencies are scope-aware and ambiguous names fail closed;
- caret constraints use semver-compatible `0.x` behavior (`^0.1.42` does not accept `0.2.0`);
- lock-write rollback is injectable and regression-tested;
- the shipped `examples/portable-package` is installed and executed from the locked copy in E2E, including bundled command/script/skill/MCP resolution;
- `adapter doctor` returns a non-zero exit status for an error report;
- process-adapter error paths drain stderr before waiting for the child process;
- conformance and package tests cover duplicate tool call IDs, update/dependent compatibility and exact Git sync commits;
- multi-repo E2E asserts that both Router and Planner receive adapter preflight and repository-catalog payloads.

## Release contract

The release E2E creates three real local Git repositories (`api -> client -> service`), lets the semantic fixture produce the repository-aware plan, executes three isolated change Runs, publishes three fake neutral SCM changes, performs integration verification, checks per-repository and plan-level evidence, and proves that the original checkouts are untouched.

## Уточнение publication workspace — v0.1.49

Repository-aware `publish_change` передаёт SCM adapter поле `repository_workspace=${nodes.<phase>.child_execution_workspace}`. Поэтому provider adapter разрешает remote и выполняет mutation относительно candidate worktree конкретного repository, а не общего control workspace или базового checkout.
