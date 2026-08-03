# Карта документов и источники истины

## 1. С чего начинать

Для знакомства с проектом:

1. `README.md`;
2. `docs/01-project.md`;
3. `docs/05-implementation-status.md`;
4. `docs/16-audit-remediation-v0.1.2.md`;
5. `docs/08-target-v0.2.md`;
6. `docs/11-implementation-plan.md`.

Для изменения runtime:

1. `ARCHITECTURE_DECISIONS.md`;
2. `docs/03-specification.md`;
3. `docs/09-runtime-semantics.md`;
4. `docs/05-implementation-status.md`;
5. код и тесты.

Для реализации исполнителя:

1. `docs/10-assistant-adapter-spec.md`;
2. `docs/14-backlog-v0.2.md`, задача TAKT-008;
3. `internal/assistant` и `internal/execution`;
4. `schemas/assistant-protocol.schema.json`;
5. contract tests.

## 2. Источники истины

| Вопрос | Основной документ | Статус |
|---|---|---|
| Зачем нужен Takt | `01-project.md` | действующий |
| Архитектурный подход | `02-approach.md`, ADR | действующий |
| Текущий внешний контракт | `03-specification.md` | реализованный `v1alpha1` |
| Текущее состояние кода | `05-implementation-status.md` | фактический |
| Исправления последнего аудита | `16-audit-remediation-v0.1.2.md` | фактический |
| Граница безопасности | `../SECURITY.md` | действующий |
| Ближайшее целевое состояние | `08-target-v0.2.md` | целевой |
| Семантика runtime | `09-runtime-semantics.md` | реализовано частично/цель v0.2 |
| Контракт исполнителя | `10-assistant-adapter-spec.md` | целевой v0.2 |
| Очередность работ | `11-implementation-plan.md` | рабочий план |
| Задачи | `14-backlog-v0.2.md` | рабочий backlog |
| Старт кодового агента | `15-coding-agent-start.md` | готовая инструкция |
| Отличия от Archon | `07-archon-compatibility.md` | справочный |
| Машиночитаемые форматы | `schemas/*.json` | текущая alpha-схема |

## 3. Приоритет при расхождении

1. код и тесты описывают фактическое поведение;
2. `03-specification.md` описывает обещанный текущий контракт;
3. `05-implementation-status.md` фиксирует ограничения;
4. `08–10` описывают целевое состояние;
5. roadmap не является спецификацией.

Расхождение кода с `03-specification.md` — дефект. Расхождение с `08–10` — незавершённая задача, если оно отмечено в status/backlog.

## 4. Как менять контракт

- архитектурная граница — новый ADR;
- изменение YAML/JSON-контракта — `03-specification.md` и JSON Schema;
- изменение семантики — `09-runtime-semantics.md` и contract tests;
- реализованная задача — обновить `05`, `11`, `14` и changelog;
- несовместимое изменение — новая `apiVersion` или мигратор.
