# Adapter Platform — v0.1.41-alpha

`v0.1.41-alpha` реализует P3 Agent + Domain Adapter SDK поверх единого Takt runtime. Главная цель — переносимые coding-agent и инженерные интеграции без GitHub/Jira-понятий в workflow и без расширения пяти публичных `takt.task.*` MCP tools.

## Что добавлено

- `sdk/agentadapter` — публичные envelope/request/result types, validators и переиспользуемый conformance kit для `takt-assistant/v1alpha2`;
- публичный `sdk/domainadapter` с типами протокола и core operations для `scm|tracker|ci`;
- новый `adapter` Node action с `name`, `operation`, JSON `input` и обычным `output_format`;
- `process` transport `takt-domain-adapter/v1alpha1`;
- MCP stdio transport с `initialize`, `tools/list`, `tools/call` и явным mapping нейтральных операций;
- `takt adapter list|describe|doctor`;
- durable `DomainOperationState` с capabilities, idempotency key, receipt и reconcile status;
- preflight reconcile capability только для операций с `side_effect.mode: reconcile` и запрет blind retry после `unknown`;
- `internal/testsupport/cmd/takt-fake-domain-adapter` и provider-neutral E2E `examples/adapter-platform`;
- схемы config/workflow/run state и `schemas/domain-adapter-protocol.schema.json`.

## Provider-neutral workflow

```yaml
- id: publish
  adapter:
    name: scm
    operation: change.create
    input: |
      {"title":"${nodes.prepare.output.title}"}
  side_effect:
    mode: reconcile
```

Конкретный Git или корпоративный SCM определяется только в `config.yaml`. Один workflow может работать с process adapter либо MCP server без изменения DAG.

## Безопасные side effects

`v0.1.40` ввела reconcile для external worker. В `v0.1.41` тот же принцип применяется к доменным adapters. Для `reconcile` runtime до мутации проверяет capability, сохраняет устойчивый idempotency key и после неопределённого ответа сначала сверяет внешний факт. `not_applied` разрешает повтор; `applied` принимает receipt; `unknown` остаётся fail-closed.

## Исправления по ревью v0.1.40

- `docs/03-specification.md` теперь полностью описывает внешний `side_effect` контракт;
- parking record очищается только после принятого steering/replan; ошибка replanner сохраняет Failure/ParkedAt;
- verdict `partial` теперь достижим: required evidence прошло, но preferred checks неполны;
- Pi overflow contract test получил 5s deadline как OpenCode-вариант;
- `BOUNDARY_VIOLATION` использует общую parking model;
- добавлен прямой regression failed→passed при evidence re-check;
- MCP surface counts закреплены как 5/7/13/29 и 54 total;
- claim после reconcile `unknown` проверяется напрямую;
- required verdict provenance fields больше не `omitempty`.

Межпроцессный notification dispatch lock и desktop sink timeout на macOS остаются платформенными интеграционными проверками; ядро ради синтетического unit-test не менялось.

## Граница реализации

Adapter Platform не содержит готовых production credentials/providers и не превращает Takt в сетевой integration server. Fake adapters доказывают контракт, а реальный GitHub/GitLab/корпоративный SCM или tracker реализуется отдельным adapter package. `process` и MCP transport считаются доверенными локальными интеграциями текущего пользователя.


## Уточнения после ревью

В `v0.1.42` kit получил общие fixture-файлы и используется contract test реального `internal/testsupport/cmd/takt-fake-assistant`, а не только собственными unit tests. Transcript validator не видит OS process exit status и поэтому не подменяет host-проверку соответствия process exit code полю `result.exit_code`.

## Reference SCM implementation — v0.1.49

`v0.1.49-alpha` добавляет `cmd/takt-github-scm-adapter` как первую production-like реализацию `sdk/domainadapter`. Workflow по-прежнему использует только `scm/*` операции; GitHub/`gh` полностью находится за adapter boundary.

Публичные `InvokeRequest` и `ReconcileRequest` получили `workspace`. Это execution workspace конкретного Run/node, а не provider-specific поле. Process transport устанавливает тот же каталог как cwd. Multi-repo publication дополнительно передаёт `repository_workspace` с точным child worktree.

Reference adapter поддерживает reconcile для `change.create`, `change.comment`, `change.review`; наружу публикуется SHA-256 marker idempotency key, а не raw Run/Node identity. Контракт проверяется `tests/e2e` / `TestReferenceAdaptersBoundary`.

### Public request validation и execution workspace — v0.1.49

`InvokeRequest` и `ReconcileRequest` теперь содержат provider-neutral `workspace`. `sdk/domainadapter` публикует `ValidateInvokeRequest`/`ValidateReconcileRequest`; process и MCP transports проверяют один и тот же контракт до вызова provider. Это позволяет внешнему SCM/Tracker/CI adapter работать с фактическим execution worktree без импорта runtime internals.
