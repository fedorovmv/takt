# Приоритетный backlog Takt v0.2

Срез от `2026-08-21`, после `v0.1.64-alpha`. Здесь остаются только текущие
release-gates, открытые evidence gaps и условные задачи. Реализованные
архитектурные и тестовые срезы описаны в
[`docs/05-implementation-status.md`](05-implementation-status.md), а история
релизов — в [`docs/archive/releases/`](archive/releases/).

## Решение по границе продукта

Takt остаётся локальным trusted single-user runtime. Следующий рост —
evidence-driven: сначала доказательства user journeys, host compatibility и
реальной полезности evaluation, затем стабилизация контрактов. Не добавлять
server, Web UI, database-backed store, remote workers или multi-user auth без
отдельного use case и threat model.

Route DSL/micro DSL, их examples, validators, benchmarks и evaluation fixtures
являются публичными OSS surfaces. Их нельзя отключать или удалять при
подготовке open-source репозитория; authored evaluation использует тот же
обычный scheduler и deterministic validation.

## P0 — release и внешняя совместимость

| ID | Задача | Владелец | Критерий закрытия | Зависимости | Статус |
|---|---|---|---|---|---|
| `REL-001` | Синхронизировать versioned-срез после `v0.1.63-alpha` | maintainer | `VERSION`, `internal/version`, README, specification/status и changelog согласованы на `0.1.64-alpha`; contract test проходит | — | **закрыто в `v0.1.64-alpha`** |
| `HOST-001` | Доказать strict host control для закреплённых Pi/OpenCode | integration owner | На pinned versions пройдены command/input interception, tool blocking, completion blocking и recovery; сохранены redacted logs/fingerprints; только тогда `strict_allowed=true` | `HOST-002` | открыто, `guarded` |
| `HOST-002` | Повторить evidence для реально поддерживаемых версий host | integration owner | Выбраны и зафиксированы версии; полный conformance повторён для Pi/OpenCode (сейчас локально наблюдаются Pi `0.84.1`, OpenCode `1.18.18`, старое evidence — Pi `0.83.0`, OpenCode `1.18.14`) | credentials и live host | открыто |

До закрытия `HOST-001`/`HOST-002` bundled Pi/OpenCode остаются `guarded`.
Version probe или fresh/resume smoke сами по себе не дают права объявить host
`strict`.

## P1 — production evidence и контрактная стабилизация

| ID | Задача | Владелец | Критерий закрытия | Зависимости | Статус |
|---|---|---|---|---|---|
| `EVAL-001` | Измерить полезность Takt на реальном обезличенном corpus | product/evaluation owner | Для Route DSL, Go и Document: штатный validator, минимум 3 repeat на case/strategy, `success@1`, final success, attempts-to-valid, time-to-valid, tokens/cost, stable/unstable cases и manual corrections; отчёты сохранены с model/adapter/version fingerprints | внешний corpus, credentials, модели | открыто |
| `EVAL-002` | Завершить переход authoring с deprecated fixed-stage suite | evaluation owner | `eval flow init` по умолчанию создаёт authored `takt/v1alpha1` scaffold; `--legacy` явно сохраняет compatibility path; docs и tests согласованы; после `EVAL-001` принять срок/условия удаления legacy | `EVAL-001` для deprecation decision | implementation закрыта; compatibility window активен |
| `ADAPTER-001` | Провести live smoke reference Qwen/GitHub adapters | integration owner | Отдельные credentialed smoke runs с redacted evidence подтверждают fresh/resume, model/usage и domain read/write/reconcile; отсутствие credentials фиксируется как gap, а не unsupported claim | pinned providers, credentials | открыто |
| `API-001` | Подготовить финальный `v1alpha1 → v1beta1` field audit и migration policy | contract owner | Реальные workflow/config/evaluation из `EVAL-001` сопоставлены с полями; выпущены migration guide и migrator только там, где он нужен; неиспользованные поля не замораживаются без evidence | `EVAL-001`, `HOST-001` | заблокировано до evidence |

Synthetic fixtures и deterministic contract tests подтверждают correctness
измерительного контура, но не заменяют `EVAL-001` или live adapter/host evidence.

## P2 — только при подтверждённой потребности

| ID | Кандидат | Условие старта | Минимальный acceptance |
|---|---|---|---|
| `UX-001` | `workflow graph/explain/scaffold`, расширенный `plan explain` | реальный user request, который не покрывают `workflow describe`, `task explain` и `run inspect` | один bounded CLI contract и user-journey test |
| `FLOW-001` | static approval reject/revise path | use case нельзя выразить существующими `loop_group` + approval + `when` без потери governance | fail-closed schema/runtime semantics и E2E contract |
| `BUDGET-001` | hard token/tool budgets | live capability proof конкретного adapter и измеренная потребность | fail-before-execution, durable diagnostics, adapter contract tests |
| `MERGE-001` | mutating merge fan-out | production multi-repo use case и threat model внешнего side effect | reconcile/rollback contract, unknown-effect handling и deterministic tests |

Остальные идеи (object storage, database, server/UI, remote workers, message
adapters, RBAC) не являются backlog ядра до появления отдельной границы
безопасности и эксплуатационного сценария.

## Порядок работы

1. Сохранить `guarded` для host и получить `HOST-001`/`HOST-002` evidence.
2. Собрать `EVAL-001`; не считать synthetic benchmark production evidence.
3. На этой базе закрыть `EVAL-002`, затем `ADAPTER-001` и `API-001`.
4. P2 начинать только после явного use case; новые YAML-поля и runtime seams не
   добавлять ради гипотетического будущего.

Подробный реестр неизвестных, исходные свидетельства и условия возврата к
design review сохранены в
[`docs/archive/analysis/2026-08-21-gap-backlog-audit.md`](archive/analysis/2026-08-21-gap-backlog-audit.md).
