# Security scope

## Supported trust model

Takt `v0.1.26-alpha` is a local, single-user, trusted runtime.

Trusted inputs:

- workflow and config files;
- Markdown commands;
- shell scripts and hook commands;
- assistant argv/env/binary configuration, including OpenCode `agent` and `auto_approve`;
- workspace contents;
- Run ID received from the local CLI after validation.

The current version must not be exposed as a service that accepts these values from untrusted users.

## Current protections

- strict unknown-field validation;
- safe Run ID format;
- per-Run lock for `answer` and `resume`;
- definition fingerprints before resume;
- timeout and output limit for process assistants;
- Unix process-group termination on context cancellation;
- revision consistency between state and event log;
- managed Git worktree separation for code-changing Runs, with safe retention of dirty results;
- explicit parent/child Run links and durable local cancellation markers.

These controls improve reliability but do not form a sandbox. A Git worktree isolates changes from the control checkout, but it does not restrict filesystem access, processes, network, credentials, or agent tools.

## OpenCode approval mode

`assistants.*.auto_approve: true` passes OpenCode `--auto`. It removes an external safety boundary and must be enabled only for a trusted workspace, trusted workflow and explicitly reviewed OpenCode agent configuration. Takt approval nodes remain the durable workflow-level confirmation mechanism and are independent of OpenCode tool permissions.

## Unsupported scenarios

- multi-user server;
- arbitrary workflows from external users;
- execution in a shared privileged workspace;
- secret-bearing stdout/stderr without external redaction;
- network isolation;
- filesystem isolation;
- protection from malicious shell commands or coding agents.

## Secrets

Takt does not intentionally copy assistant environment variables into state or events. However, command output, hook feedback, model responses and error messages may contain secrets and are currently persisted without automatic redaction. Evaluation reports also persist model `params` used for execution identity; credentials and secret headers must remain in environment or an external secret source, not in `models.*.params`.

Before production-like use, define:

- secret sources;
- redaction patterns and structured secret markers;
- fields prohibited in state/events;
- retention and deletion policy;
- tests for stdout/stderr and error-message leakage.

## Server/untrusted prerequisites

A future server or untrusted mode requires at minimum:

- sandboxed process execution;
- workspace and artifact path policy;
- network egress policy;
- secret broker and redaction;
- authentication and authorization;
- durable distributed locking;
- quotas and resource limits;
- audit retention policy;
- recovery from stale locks and interrupted commits.


## Evaluation

`takt eval run` копирует и выполняет workspace template для каждого задания. Template, workflow, config, cases, assistant binaries и внешний validator входят в trusted input. Evaluation runner не создаёт sandbox и не должен использоваться для запуска недоверенных наборов заданий или шаблонов.

## Подключённые workflow

Пути `subworkflow.path` считаются доверенными и могут ссылаться на локальные файлы, включая абсолютные пути. Компиляция не создаёт sandbox и не ограничивает чтение definition files. Для untrusted/server режима потребуются отдельная политика корней, запрет выхода из package и проверка символических ссылок.

## Governed child Runs

`workflow.path` is a trusted local definition path. A child Run has separate state, events and artifacts, but this is an execution-lifecycle boundary rather than a security boundary. `isolation: inherit` deliberately shares the parent execution workspace; `worktree` creates a Git worktree but still does not restrict filesystem, network, credentials or process access. Cascading cancellation is cooperative and relies on local context/process termination.

## Assistant-enforced node policies

`allowed_tools`, `denied_tools`, `skills`, `mcp`, `sandbox` and `requires` prevent silent policy omission: Takt verifies adapter capabilities before invocation and persists the effective policy. These controls constrain the coding agent interface but do not isolate the assistant binary, arbitrary subprocesses, custom MCP servers or host credentials. `sandbox.filesystem` and `sandbox.network` are contracts that an adapter must implement; they are not an OS security boundary.
