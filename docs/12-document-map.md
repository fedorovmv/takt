# Карта документов и источники истины

## 1. С чего начинать

Для знакомства с проектом:

1. `AGENTS.md`;
2. `README.md`;
3. `docs/01-project.md`;
4. `docs/05-implementation-status.md`;
5. `skills/takt/SKILL.md`;
6. `docs/44-local-mcp-control-plane-v0.1.30.md`;
7. `docs/43-script-nodes-typed-artifacts-v0.1.29.md`;
8. `docs/42-governed-child-fanout-v0.1.28.md`;
9. `docs/41-node-capability-policies-v0.1.27.md`;
10. `docs/40-governed-child-runs-v0.1.26.md`;
11. `docs/39-git-worktree-isolation-v0.1.25.md`;
12. `docs/38-archon-workflow-catalog-v0.1.24.md`;
13. `docs/37-composition-hardening-v0.1.23.md`;
14. `docs/34-opencode-provider-diagnostics-v0.1.20.md`;
15. `docs/33-opencode-adapter-v0.1.19.md`;
16. `docs/32-takt-authoring-skill-v0.1.18.md`;
17. `docs/31-quality-stdout-separation-v0.1.17.md`;
18. `docs/30-quality-envelope-semantics-v0.1.16.md`;
19. `docs/29-benchmark-metric-semantics-v0.1.15.md`;
20. `docs/28-benchmark-identity-quality-v0.1.14.md`;
21. `docs/27-evaluation-isolation-report-v0.1.13.md`;
22. `docs/26-evaluation-runner-v0.1.12.md`;
23. `docs/25-route-dsl-e2e-v0.1.11.md`;
24. `docs/24-pi-context-usage-hardening-v0.1.10.md`;
25. `docs/23-pi-rpc-alignment-v0.1.9.md`;
26. `docs/22-pi-adapter-v0.1.8.md`;
27. `docs/21-protocol-hardening-v0.1.7.md`;
28. `docs/20-fake-assistant-contract-v0.1.6.md`;
29. `docs/19-document-recovery-v0.1.5.md`;
30. `docs/18-audit-remediation-v0.1.4.md`;
31. `docs/17-audit-remediation-v0.1.3.md`;
32. `docs/16-audit-remediation-v0.1.2.md`;
33. `docs/08-target-v0.2.md`;
34. `docs/11-implementation-plan.md`;


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
| Правила работы кодовых агентов | `../AGENTS.md` | действующий |
| Создание и настройка workflow | `../skills/takt/SKILL.md`, `43-script-nodes-typed-artifacts-v0.1.29.md`, `42-governed-child-fanout-v0.1.28.md`, `41-node-capability-policies-v0.1.27.md`, `40-governed-child-runs-v0.1.26.md`, `39-git-worktree-isolation-v0.1.25.md`, `38-archon-workflow-catalog-v0.1.24.md`, `36-workflow-composition-v0.1.22.md`, `37-composition-hardening-v0.1.23.md` | действующий скилл, каталог, роутер и композиция |
| Зачем нужен Takt | `01-project.md` | действующий |
| Архитектурный подход | `02-approach.md`, ADR | действующий |
| Текущий внешний контракт | `03-specification.md` | реализованный `v1alpha1` |
| Script-узлы и типизированные артефакты | `43-script-nodes-typed-artifacts-v0.1.29.md` | фактический |
| Динамический fan-out governed children | `42-governed-child-fanout-v0.1.28.md` | фактический |
| Текущее состояние кода | `05-implementation-status.md` | фактический |
| Локальный MCP control plane | `44-local-mcp-control-plane-v0.1.30.md` | фактический |
| Исправления последнего аудита | `16–18-audit-remediation-*.md` | фактический |
| Fake-assistant protocol suite | `20-fake-assistant-contract-v0.1.6.md` | фактический |
| Строгая OS/envelope семантика | `21-protocol-hardening-v0.1.7.md` | фактический |
| Pi RPC adapter | `22-pi-adapter-v0.1.8.md`, `23-pi-rpc-alignment-v0.1.9.md`, `24-pi-context-usage-hardening-v0.1.10.md` | фактический |
| OpenCode adapter | `33-opencode-adapter-v0.1.19.md`, `34-opencode-provider-diagnostics-v0.1.20.md`, `../examples/opencode-smoke/` | фактический |
| Route DSL end-to-end | `25-route-dsl-e2e-v0.1.11.md`, `../examples/route-dsl-e2e/` | контрактный срез реализован |
| Evaluation runner и метрики | `26-evaluation-runner-v0.1.12.md`, `27-evaluation-isolation-report-v0.1.13.md`, `28-benchmark-identity-quality-v0.1.14.md`, `29-benchmark-metric-semantics-v0.1.15.md`, `30-quality-envelope-semantics-v0.1.16.md`, `31-quality-stdout-separation-v0.1.17.md`, `../examples/route-dsl-eval/`, `../examples/route-dsl-benchmark/` | идентичность, per-attempt usage и предметные метрики реализованы; реальный baseline требует штатного валидатора |
| Восстановление документации релиза | `19-document-recovery-v0.1.5.md` | фактический |
| Граница безопасности | `../SECURITY.md` | действующий |
| Ближайшее целевое состояние | `08-target-v0.2.md` | целевой |
| Семантика runtime | `09-runtime-semantics.md` | реализовано частично/цель v0.2 |
| Контракт исполнителя | `10-assistant-adapter-spec.md` | process protocol, Pi и OpenCode adapters реализованы; capability discovery и policy mapping реализованы |
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

- `35-profile-packages-code-v0.1.21.md` — пакеты профилей, `takt init` и режим разработки по Markdown-плану.

- `36-workflow-composition-v0.1.22.md` — reusable subworkflow, последовательный foreach и fingerprint подключённых определений.

- `37-composition-hardening-v0.1.23.md` — внешний items source, композиция в loop_group, агрегированный foreach output и публичная проекция Run.

- `38-archon-workflow-catalog-v0.1.24.md` — 19 процессов профиля code, умный роутер, параллельность, output_format и интерактивные циклы.

- `39-git-worktree-isolation-v0.1.25.md` — managed Git worktree lifecycle, router-aware activation and local-runtime boundary.

- `40-governed-child-runs-v0.1.26.md` — отдельные child Run, parent/child lifecycle, approval cascade, cancellation tree и isolation modes.

- `41-node-capability-policies-v0.1.27.md` — tool/skill/MCP policies, capability negotiation, inheritance and review fixes.

- `43-script-nodes-typed-artifacts-v0.1.29.md` — script runtime, fingerprints исходников, типизированные артефакты, CLI и parent/child propagation.

- `44-local-mcp-control-plane-v0.1.30.md` — dual-era stdio MCP server, local control service, detached Run lifecycle, events и artifacts.

- `45-agent-events-external-executor-v0.1.31.md` — нормализованные assistant/tool events, durable внешний executor узла и исправление конкурентного store.
