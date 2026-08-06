# Security scope

## Supported trust model

Takt `v0.1.33-alpha` is a local, single-user, trusted runtime. `takt daemon` allows several clients owned by the same OS user but does not create a multi-user trust boundary.

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
- per-Run lock for control mutations, with bounded serialization for concurrent CLI/MCP/daemon clients;
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


## Локальный MCP control plane

`takt mcp` предоставляет тем же локальным полномочиям структурированный интерфейс запуска, approval, cancellation и чтения state/events/artifacts. Он не содержит аутентификации и не является сетевой security boundary. Запускай его только как stdio child process доверенного coding-agent host того же пользователя.

Tool arguments считаются доверенными локальными запросами, но декодируются строго. Artifact content ограничивается по размеру; это ограничение защищает transport от случайного большого ответа, но не выполняет redaction. Содержимое state, events, stdout/stderr и artifacts может включать секреты согласно общему trust model.

## Локальный daemon

`takt daemon` слушает только Unix socket в `.takt/daemon.sock`; socket, metadata и log создаются для текущего пользователя. Любой процесс с доступом к socket получает полномочия локального CLI над workspace: запуск, чтение output/artifacts, approval, cancellation и внешний worker control. Это механизм локальной координации, а не аутентификация.

Не публикуй socket через TCP proxy и не размещай workspace в каталоге с доступом недоверенных пользователей. Daemon переживает закрытие клиента, но не является supervisor для crash-recovery произвольного OS-процесса. После завершения daemon исполнявшийся subprocess прекращается вместе с ним; durable waiting/pending state остаётся для явного resume/reclaim.

`idle_timeout` внешнего executor выполняется daemon. Он закрывает незавершённые tool calls и сохраняет timeout transition, но не может принудительно остановить сторонний процесс worker, который находится вне process group Takt.

## Управляемые tool calls внешнего executor

`tool_approval` и `tool_control` являются сохраняемым управляющим контрактом, но действуют только для adapter, который способен передать запрос до фактического запуска инструмента. OpenCode и Pi в текущих интеграциях публикуют наблюдательные события и не предоставляют этот security boundary.

Claim token внешнего worker является локальным секретом lease. Его нельзя записывать в assistant messages, tool input/output, state-visible diagnostics или artifacts. Controller может отменить отдельный tool call; для уже выполняющегося внешнего действия это cooperative `cancel_requested`, который worker обязан подтвердить terminal-событием. Takt не может принудительно остановить произвольный внешний процесс за пределами своего process group.

Policy/approval не заменяют OS sandbox. Разрешённый tool call всё ещё выполняется с полномочиями внешнего worker и текущего пользователя.
