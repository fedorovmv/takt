# Reference External Adapters — v0.1.49-alpha

`v0.1.49-alpha` начинает P2 External seams двумя настоящими reference implementations поверх публичных SDK Takt. Цель среза — доказать, что coding-agent и SCM provider можно реализовать без импорта `internal/` и без provider-specific логики в workflow/runtime.

## Qwen Code process wrapper

`cmd/qwen-takt-adapter` использует только `sdk/agentadapter` и переводит официальный headless stream Qwen Code в `takt-assistant/v1alpha2`.

Запуск upstream CLI:

```text
qwen --prompt <prompt>
     --output-format stream-json
     --safe-mode
     --approval-mode yolo
     --model <model-id>
     [--resume <exact-session-id>]
     [--max-wall-time <timeout>]
```

Wrapper нормализует `session.started|session.resumed`, assistant message, usage, diagnostics и terminal result. Exact resume identity проверяется публичным SDK и process host.

Capability surface намеренно узкий:

```text
agent_events_v2
session_events
usage_events
```

Reference wrapper не заявляет `tool_policy`, selected skills, MCP projection, `tool_control`, filesystem/network sandbox. `--safe-mode` изолирует запуск от project-level Qwen customizations; `--approval-mode yolo` делает headless запуск неинтерактивным. Поэтому этот wrapper предназначен только для trusted execution workspace и узлов, whose effective Takt policy не требует перечисленных возможностей.

Версия `takt-assistant/v1alpha2` сама по себе больше не означает `tool_control`: static process declaration строится из явно настроенных capabilities, а фактическая stream declaration проверяется в runtime. Tool request без объявленного `tool_control`, undeclared event или расхождение configured/stream capabilities является protocol failure.

Официальная документация Qwen Code: <https://qwenlm.github.io/qwen-code-docs/en/users/features/headless/>.

## GitHub SCM reference adapter

`cmd/takt-github-scm-adapter` использует только `sdk/domainadapter` и authenticated `gh` CLI. Workflow видит только нейтральные операции:

```text
repository.get
change.get
change.create
change.comment
change.review
checks.get
```

Provider-specific mapping находится внутри adapter.

Repository resolution идёт в порядке:

1. `repository_workspace` из operation input;
2. существующий repository path относительно execution workspace;
3. execution workspace и его Git `origin`;
4. явный `[HOST/]OWNER/REPO`;
5. `GH_REPO`.

Для этого публичные `InvokeRequest`/`ReconcileRequest` получили необязательный `workspace`. Public SDK валидирует `run_id`, `node_id`, `attempt`, domain/operation, side-effect/idempotency contract и непустой `workspace`. Process и MCP transports применяют один request-validator и запускают adapter в том же execution workspace. Multi-repo publication передаёт точный `child_execution_workspace` как `repository_workspace`, поэтому публикация относится к candidate worktree конкретного repository.

Mutating `change.create|comment|review` поддерживают durable reconcile. Внешнее тело получает не raw Takt idempotency key, а маркер:

```text
<!-- takt-idempotency:<sha256(key)> -->
```

После неоднозначной ошибки adapter сначала ищет существующий PR/comment/review по receipt/marker; повторная мутация разрешается только после `not_applied` по обычной runtime semantics.

Официальная документация GitHub CLI: `gh pr create`, `gh pr view`, `gh pr checks`, `gh api`.

## Проверяемая граница

`tests/e2e` / `TestReferenceAdaptersBoundary` выполняет два E2E:

1. настоящий Takt process-v1alpha2 → `qwen-takt-adapter` → fake upstream Qwen stream → completed Run;
2. настоящий adapter node → `takt-github-scm-adapter` → fake `gh`, который создаёт внешний факт и теряет ответ → reconcile находит PR → completed без второго create.

Unit tests дополнительно проверяют fresh/resume conformance, fail-closed unsupported Qwen policy, repository/worktree resolution, Git remote parsing и hashed idempotency marker.

Reference packages и binaries не импортируют `takt/internal/*`.

## Граница среза

Это reference implementations, а не перенос provider logic в core. Live Qwen с реальной моделью/credential и настоящий GitHub mutation не входят в release gate. Они требуют внешних credentials и отдельного opt-in smoke. Корпоративный Git/SCM и другие coding agents должны подключаться теми же public SDK contracts.
