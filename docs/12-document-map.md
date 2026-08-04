# Карта документов и источники истины

## 1. С чего начинать

Для знакомства с проектом:

1. `README.md`;
2. `docs/01-project.md`;
3. `docs/05-implementation-status.md`;
4. `docs/27-evaluation-isolation-report-v0.1.13.md`;
5. `docs/26-evaluation-runner-v0.1.12.md`;
6. `docs/25-route-dsl-e2e-v0.1.11.md`;
7. `docs/24-pi-context-usage-hardening-v0.1.10.md`;
8. `docs/23-pi-rpc-alignment-v0.1.9.md`;
9. `docs/22-pi-adapter-v0.1.8.md`;
10. `docs/21-protocol-hardening-v0.1.7.md`;
11. `docs/20-fake-assistant-contract-v0.1.6.md`;
12. `docs/19-document-recovery-v0.1.5.md`;
13. `docs/18-audit-remediation-v0.1.4.md`;
14. `docs/17-audit-remediation-v0.1.3.md`;
15. `docs/16-audit-remediation-v0.1.2.md`;
16. `docs/08-target-v0.2.md`;
17. `docs/11-implementation-plan.md`.

Для изменения runtime:

1. `ARCHITECTURE_DECISIONS.md`;
2. `docs/03-specification.md`;
3. `docs/09-runtime-semantics.md`;
4. `docs/05-implementation-status.md`;
5. код и тесты.

Для реализации исполнителя:

1. `docs/10-assistant-adapter-spec.md`;
2. `docs/25-route-dsl-e2e-v0.1.11.md`;
3. `docs/24-pi-context-usage-hardening-v0.1.10.md`;
4. `docs/23-pi-rpc-alignment-v0.1.9.md`;
5. `docs/22-pi-adapter-v0.1.8.md`;
6. `docs/21-protocol-hardening-v0.1.7.md`;
7. `docs/20-fake-assistant-contract-v0.1.6.md`;
8. `docs/14-backlog-v0.2.md`, задачи TAKT-009 и TAKT-011;
9. `internal/assistant` и `internal/execution`;
10. `schemas/assistant-protocol.schema.json`;
11. contract tests.

## 2. Источники истины

| Вопрос | Основной документ | Статус |
|---|---|---|
| Зачем нужен Takt | `01-project.md` | действующий |
| Архитектурный подход | `02-approach.md`, ADR | действующий |
| Текущий внешний контракт | `03-specification.md` | реализованный `v1alpha1` |
| Текущее состояние кода | `05-implementation-status.md` | фактический |
| Исправления последнего аудита | `16–18-audit-remediation-*.md` | фактический |
| Fake-assistant protocol suite | `20-fake-assistant-contract-v0.1.6.md` | фактический |
| Строгая OS/envelope семантика | `21-protocol-hardening-v0.1.7.md` | фактический |
| Pi RPC adapter | `22-pi-adapter-v0.1.8.md`, `23-pi-rpc-alignment-v0.1.9.md`, `24-pi-context-usage-hardening-v0.1.10.md` | фактический |
| Route DSL end-to-end | `25-route-dsl-e2e-v0.1.11.md`, `../examples/route-dsl-e2e/` | контрактный срез реализован |
| Evaluation runner и метрики | `26-evaluation-runner-v0.1.12.md`, `27-evaluation-isolation-report-v0.1.13.md`, `../examples/route-dsl-eval/` | безопасный базовый контур реализован |
| Восстановление документации релиза | `19-document-recovery-v0.1.5.md` | фактический |
| Граница безопасности | `../SECURITY.md` | действующий |
| Ближайшее целевое состояние | `08-target-v0.2.md` | целевой |
| Семантика runtime | `09-runtime-semantics.md` | реализовано частично/цель v0.2 |
| Контракт исполнителя | `10-assistant-adapter-spec.md` | process protocol и Pi adapter реализованы; OpenCode/capability discovery — цель v0.2 |
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
