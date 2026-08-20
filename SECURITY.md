# Security scope

## Supported trust model

Takt `v0.1.63-alpha` is a local, single-user, trusted runtime. `takt daemon` may serve several clients owned by the same OS user but does not create authentication, authorization, tenant isolation or an untrusted execution boundary.

Trusted inputs include workflow/config/package files, Markdown commands, scripts and hooks, assistant/adapter binaries and argv, workspace contents and locally installed package sources. Do not expose the current runtime as a service that accepts these values from untrusted users.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability and do not include
credentials, exploit details or private workspace contents in a public report.
Use GitHub's private security advisory flow for this repository:
<https://github.com/fedorovmv/takt/security/advisories/new>.

Include the affected version or commit, operating system, a minimal trusted
reproduction, impact and any safe mitigation. If the advisory form is
unavailable, open a non-public maintainer contact through the repository
administration rather than disclosing exploit details publicly. There is no
guaranteed response or remediation SLA during the alpha period.

## Current protection layers

Takt combines several independent controls:

- strict config/workflow/schema validation and bounded path checks;
- per-Run locking, revisions and definition/content fingerprints;
- managed Git worktrees and repository-aware integrity checks;
- process timeout/output limits and process-group cancellation;
- durable retry/backoff, normalized diagnostics and failure fingerprints;
- `SecretRef` (`secret://ENV_NAME`) plus redaction before state/event/text-artifact persistence;
- local OS sandbox enforcement for deterministic `bash/script` nodes when explicitly requested;
- assistant capability/policy preflight for tools, skills, MCP and assistant-level sandbox contracts;
- side-effect idempotency/reconciliation for external/domain operations with unknown outcomes.

No single item above is the security boundary for the whole system. Worktrees isolate changes from the control checkout, not arbitrary filesystem/process/network access. Assistant policy constrains an adapter interface, not a malicious local binary. Local OS sandbox applies only to deterministic nodes that Takt launches itself.

## Secrets and redaction

Prefer environment-backed references instead of literal credentials:

```yaml
script:
  runtime: command
  path: ./scripts/check.sh
  env:
    TOKEN: secret://CORP_TOKEN
```

`secret://NAME` resolves from the current process environment immediately before execution. Templated environment values are scanned again after request rendering so a rendered `secret://NAME` reference is registered before execution. The resolved value is registered with the runtime redactor even when it is short. Takt also heuristically registers values of environment variables whose names look secret-bearing; this heuristic is defense-in-depth and is not a replacement for explicit `SecretRef`.

Before persistence, Takt redacts known secrets from Run state, approvals, node/execution output, diagnostics, domain receipts, external tool inputs/results, event data and textual artifacts. The same persistence boundary is used by runtime and control/external worker paths. A non-text artifact containing a known secret is rejected instead of being stored. Foreground control/CLI responses are reloaded from the durable Store after execution so a resolved secret is not returned from the live in-memory state.

Limitations:

- redaction only protects values known to the redactor; transformed, encoded, encrypted, split or previously unknown values may not match;
- resolved values necessarily exist in the environment/memory of the process that needs them;
- trusted subprocesses, coding agents and MCP servers still run with the OS credentials available to them unless an independent sandbox restricts those capabilities;
- artifact filenames/paths and trusted configuration metadata should not contain credentials;
- evaluation model `params` are persisted as execution identity; keep credentials and secret headers out of `models.*.params`.

## Local OS sandbox

For deterministic `bash` and `script` nodes, `sandbox.enforcement` requests an OS-level wrapper in addition to ordinary workflow policy:

```yaml
sandbox:
  enforcement: required
  filesystem: read_only
  network: deny
```

Current local backends are:

- Linux: `bwrap`/bubblewrap when available;
- macOS: `sandbox-exec` when available.

`required` fails before the payload command if a supported backend is unavailable. `optional` executes with a persisted `degraded` sandbox decision. Validation commands and node hooks use the same deterministic-node sandbox and cannot silently bypass a required sandbox.

This is deliberately a local hardening layer, not a general container/security runtime. The current filesystem policy is coarse (`read_only` versus normal host access), and backend availability/behavior is OS-specific. `command`/`prompt` coding-agent nodes do not claim OS sandbox enforcement: their `sandbox.filesystem/network` fields remain adapter capability contracts.

## OpenCode/Pi and host control

`assistants.*.auto_approve: true` (OpenCode `--auto`) removes an external approval boundary. Enable it only for a trusted workspace and reviewed agent configuration. Takt approval nodes remain workflow-level durable gates but do not replace the coding-agent host's tool permissions.

Host session/cache are not authentication. Go host guards are default-deny, but physical blocking depends on capabilities of the coding-agent host. Bundled Pi/OpenCode integrations remain `guarded` until live contract tests on pinned host versions prove the relevant enforcement path.

## Worktrees, multi-repo and packages

A managed worktree is an execution-lifecycle boundary, not an OS security boundary. Multi-repo repository paths are bounded to the control workspace and checked again after symlink resolution; repository fingerprints are rechecked before execution/replanning. These controls prevent accidental repository/path substitution but do not make workspace contents untrusted-safe.

Portable packages support source allowlists, integrity locks, dependency/capability preflight and optional Ed25519 signature policy. These mechanisms prove the selected package source/content against configured local trust policy; they do not make arbitrary package scripts safe to execute. Package commands/scripts/skills/MCP configuration are trusted executable content.

## External/domain side effects

For operations that may outlive the local process, use an idempotent adapter or `side_effect.mode: reconcile`. If an outcome becomes unknown, Takt blocks blind retry until the external fact is checked. `applied` requires a receipt; `not_applied` permits a new attempt; `unknown` remains parked. This reduces duplicate effects but does not provide exactly-once semantics unless the external system itself supplies the required guarantees.

External worker claim tokens are local lease secrets and must not be copied into prompts, tool payloads, diagnostics or artifacts. Cooperative tool cancellation cannot forcibly terminate a worker process outside Takt's process group.

## Local MCP and daemon

`takt mcp` is intended as a stdio child process of a trusted local coding-agent host. `takt daemon` uses a local Unix socket and the same `control.Service`; any process that can access that socket has the effective authority of the local CLI over the workspace. Do not proxy the socket over TCP or place the workspace/socket in a directory writable by untrusted users.

Daemon restart recovery may create a new attempt after a lost subprocess. Durable backoff deadlines and side-effect reconciliation are preserved, but an arbitrary external action still needs an idempotency/reconciliation contract to avoid duplication.

Notifications are local convenience outputs, not an independent security boundary. Notification data normally originates from persisted/redacted Run events, but trusted notification configuration, paths and process-sink behavior remain visible to the local user/process. A `process` sink executes a trusted local binary with the user's privileges.

## Evaluation and connected definitions

`takt eval run`, `subworkflow.path`, governed `workflow.path`, package content and workspace templates are trusted inputs. Evaluation isolation uses copied workspaces/worktrees and does not automatically turn arbitrary cases or validators into untrusted code. Absolute/linked definition paths and local MCP servers must be governed by the same trusted-local policy.

## Unsupported scenarios

The current version is not intended for:

- a multi-user or Internet-facing server;
- arbitrary workflows/packages supplied by untrusted users;
- shared privileged execution workspaces;
- a guarantee that malicious coding-agent binaries cannot access host credentials;
- distributed workers or tenant-separated secrets;
- a built-in secret store/broker;
- a portable cross-platform sandbox stronger than the available local OS backend.

A future untrusted/server mode needs a separate threat model and, at minimum, authenticated/authorized control, a hardened sandbox/container boundary, explicit filesystem and network egress policy, a secret broker, quotas, distributed locking and audited retention/recovery.


## Structured Task Sources

`task_sources.*.env` supports the same `secret://ENV_NAME` resolution and fail-closed missing-secret behavior as other process adapters. The resolved external task is treated as trusted input from a configured source adapter, normalized to a bounded public contract and persisted through the same plan redaction boundary. Source content does not grant tools, weaken policies or bypass Router/Plan validation.
