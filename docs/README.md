# Документация

Начните с пользовательского маршрута:

1. [`../README.md`](../README.md) — назначение, установка и быстрый старт;
2. [`user-guide.md`](user-guide.md) — настройка, Workflow, Run, daemon и MCP;
3. [`01-project.md`](01-project.md) — цели, сценарии и границы;
4. [`04-architecture.md`](04-architecture.md) — устройство runtime;
5. [`12-document-map.md`](12-document-map.md) — подробная карта источников
   истины.

## Текущий контракт

| Тема | Документ |
|---|---|
| Workflow и Config | [`03-specification.md`](03-specification.md) |
| Архитектура | [`04-architecture.md`](04-architecture.md) |
| Реализовано и ограничено | [`05-implementation-status.md`](05-implementation-status.md) |
| Roadmap | [`06-roadmap.md`](06-roadmap.md) |
| Archon compatibility | [`07-archon-compatibility.md`](07-archon-compatibility.md) |
| Runtime semantics | [`09-runtime-semantics.md`](09-runtime-semantics.md) |
| Assistant adapters | [`10-assistant-adapter-spec.md`](10-assistant-adapter-spec.md) |
| Evaluation | [`13-evaluation-plan.md`](13-evaluation-plan.md), [`73-evaluation-authoring-guide.md`](73-evaluation-authoring-guide.md) |
| Coding-agent authoring | [`15-coding-agent-start.md`](15-coding-agent-start.md), [`../skills/takt/SKILL.md`](../skills/takt/SKILL.md) |
| Generated canonical operations | [`71-canonical-operation-contracts.generated.md`](71-canonical-operation-contracts.generated.md) |

`08-target-v0.2.md`, `11-implementation-plan.md` и `14-backlog-v0.2.md` — это
целевое состояние и рабочие планы, а не инструкции для обычного пользователя.

## Примеры и схемы

- [`../examples/`](../examples/) — рабочие Workflow, profiles, smoke и
  integration examples;
- [`../schemas/`](../schemas/) — machine-readable contracts;
- [`../skills/takt/`](../skills/takt/) — authoring skill и его references.

## Архив

Исторические release notes, включая versioned architecture slices, и test
evidence вынесены в [`archive/`](archive/). Они сохранены для traceability и
расследования эволюции, но не являются текущей спецификацией и не входят в
onboarding.

При расхождении используйте [`12-document-map.md`](12-document-map.md): код,
schemas и текущие нормативные документы имеют приоритет над roadmap, plans и
архивом.
