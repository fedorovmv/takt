# Карта документации и источники истины

Эта карта разделяет документы по аудитории и назначению. README — точка
входа, а документы ниже являются источниками деталей. Исторические заметки не
нужно читать для первого запуска и не следует трактовать как текущий контракт.

Краткий индекс каталога находится в [`README.md`](README.md). Исторические
release notes, test evidence и audits вынесены в [`archive/`](archive/).

## Пользовательский маршрут

| Задача | Документ |
|---|---|
| Понять назначение и границы | [`01-project.md`](01-project.md) |
| Установить, настроить и запустить | [`user-guide.md`](user-guide.md) |
| Создать Workflow или профиль | [`03-specification.md`](03-specification.md), [`../skills/takt/SKILL.md`](../skills/takt/SKILL.md), [`../examples/`](../examples/) |
| Наблюдать, продолжить и расследовать Run | [`09-runtime-semantics.md`](09-runtime-semantics.md), [`user-guide.md`](user-guide.md) |
| Подключить Pi, OpenCode или process adapter | [`10-assistant-adapter-spec.md`](10-assistant-adapter-spec.md), [`../sdk/agentadapter/`](../sdk/agentadapter/) |
| Использовать MCP или daemon | [`user-guide.md`](user-guide.md), [`03-specification.md`](03-specification.md), [`71-canonical-operation-contracts.generated.md`](71-canonical-operation-contracts.generated.md) |
| Создать evaluation | [`73-evaluation-authoring-guide.md`](73-evaluation-authoring-guide.md), [`13-evaluation-plan.md`](13-evaluation-plan.md) |
| Проверить security scope | [`../SECURITY.md`](../SECURITY.md) |

## Маршрут контрибьютора

| Задача | Документ |
|---|---|
| Сообщить дефект или подготовить PR | [`../CONTRIBUTING.md`](../CONTRIBUTING.md) |
| Запустить локальные проверки | [`../DEVELOPMENT.md`](../DEVELOPMENT.md) |
| Понять архитектурные границы | [`04-architecture.md`](04-architecture.md), [`../ARCHITECTURE_DECISIONS.md`](../ARCHITECTURE_DECISIONS.md) |
| Изменить Workflow/Config/protocol | [`03-specification.md`](03-specification.md), [`09-runtime-semantics.md`](09-runtime-semantics.md), соответствующие `schemas/*.json` |
| Изменить assistant adapter | [`10-assistant-adapter-spec.md`](10-assistant-adapter-spec.md), [`../sdk/agentadapter/`](../sdk/agentadapter/), contract tests |
| Разобраться со статусом и gaps | [`05-implementation-status.md`](05-implementation-status.md), [`06-roadmap.md`](06-roadmap.md), [`14-backlog-v0.2.md`](14-backlog-v0.2.md) |

## Нормативные источники

Используйте этот порядок при расхождении утверждений:

1. код, схемы и выполняемые tests — фактическое поведение;
2. [`03-specification.md`](03-specification.md) — текущий внешний
   Workflow/Config контракт;
3. [`09-runtime-semantics.md`](09-runtime-semantics.md) — статусы, retries,
   loops, approval, cancellation и resume;
4. [`10-assistant-adapter-spec.md`](10-assistant-adapter-spec.md) — assistant
   protocol и adapter-specific semantics;
5. [`05-implementation-status.md`](05-implementation-status.md) — реализовано,
   ограничено и deferred;
6. [`04-architecture.md`](04-architecture.md) и ADR — архитектурные правила и
   эволюция;
7. roadmap, backlog, proposals и release notes — планы, решения и история, но
   не нормативный runtime contract.

Машиночитаемые форматы находятся в [`../schemas/`](../schemas/). Generated
операции appapi/MCP — в
[`71-canonical-operation-contracts.generated.md`](71-canonical-operation-contracts.generated.md).

## Архитектура и стабильность модулей

- [`04-architecture.md`](04-architecture.md) — компонентная схема, scheduler,
  Store, adapters, extensions и trust boundary;
- [`../ARCHITECTURE_DECISIONS.md`](../ARCHITECTURE_DECISIONS.md) — действующие
  ADR.

## Evaluation и интеграции

- [`13-evaluation-plan.md`](13-evaluation-plan.md) — измерительная модель и
  legacy compatibility;
- [`73-evaluation-authoring-guide.md`](73-evaluation-authoring-guide.md) —
  production-shaped authored flow;
- [`10-assistant-adapter-spec.md`](10-assistant-adapter-spec.md) — текущий
  assistant protocol и adapter semantics;
- [`../integrations/`](../integrations/) — поддерживаемые интеграции и их
  локальные инструкции;
- [`../examples/`](../examples/) — runnable fixtures, smoke tests и
  integrations.

## История и планы

Документы [`archive/releases/`](archive/releases/) с суффиксом `v0.1.*` —
исторические alpha-срезы и traceability решений. Они полезны для расследования
происхождения контракта, но не являются onboarding-маршрутом. Для текущего состояния используйте
[`05-implementation-status.md`](05-implementation-status.md), для будущих
изменений — [`06-roadmap.md`](06-roadmap.md) и
[`14-backlog-v0.2.md`](14-backlog-v0.2.md).

Отдельные proposals и документы `docs/superpowers/` — материалы проектирования
и реализации. Они не переопределяют текущую спецификацию без явного переноса
решения в нормативные документы, код и tests.

Первоначальная мотивация нового Go-runtime и роль Archon как поведенческого
ориентира сохранены в
[`archive/analysis/02-initial-approach.md`](archive/analysis/02-initial-approach.md).

Исторические verification reports находятся в
[`archive/verification/`](archive/verification/), а датированные аудиты — в
[`archive/analysis/`](archive/analysis/).

## Как обновлять документацию

- Изменение YAML/JSON-контракта требует обновления спецификации и schema.
- Изменение runtime/protocol semantics требует contract tests и обновления
  [`09-runtime-semantics.md`](09-runtime-semantics.md) или
  [`10-assistant-adapter-spec.md`](10-assistant-adapter-spec.md).
- Изменение фактического статуса требует обновить
  [`05-implementation-status.md`](05-implementation-status.md), changelog и
  roadmap/backlog, если они затронуты.
- README должен оставаться короткой точкой входа; release history, backlog и
  внутренние design notes находятся в своих документах.
