# Карта документов и источники истины

## 1. С чего начинать

Для обычного пользователя и разработчика достаточно текущих документов:

1. `../README.md` — установка, быстрый старт и стабильные пользовательские сценарии;
2. `01-project.md` — назначение и границы Takt;
3. `03-specification.md` — текущий внешний Workflow/Config контракт;
4. `05-implementation-status.md` — что реализовано и какой статус имеет;
5. `09-runtime-semantics.md` — durable execution semantics;
6. `10-assistant-adapter-spec.md` — stable assistant protocol и extension adapters;
7. `../skills/takt/SKILL.md` — authoring workflow/profile;
8. `72-architecture-contracts-v0.1.57.md` — нормативные контракты эволюции языка, registrations и canonical operations;
9. `71-canonical-operation-contracts.generated.md` — generated appapi/MCP operation surface;
10. `70-codebase-hygiene-stabilization-v0.1.56.md` — граница stable/extensions/experimental/tooling;
11. `../ARCHITECTURE_DECISIONS.md` — действующие архитектурные решения.

Документы `16–70` сохраняют историю отдельных alpha-релизов и нужны при расследовании происхождения конкретного контракта. Они **не являются обязательным маршрутом onboarding**. `proposals/` описывает принятые или обсуждаемые направления, а не текущий пользовательский контракт.

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
8. `docs/14-backlog-v0.2.md`, раздел P2 про внешние seams;
9. `internal/assistant` и `internal/execution`;
10. `schemas/assistant-protocol.schema.json`;
11. contract tests.


### История alpha-релизов

Файлы ниже сохраняются как traceability/history и не входят в обязательный onboarding:

- `16-audit-remediation-v0.1.2.md`
- `17-audit-remediation-v0.1.3.md`
- `18-audit-remediation-v0.1.4.md`
- `19-document-recovery-v0.1.5.md`
- `20-fake-assistant-contract-v0.1.6.md`
- `21-protocol-hardening-v0.1.7.md`
- `22-pi-adapter-v0.1.8.md`
- `23-pi-rpc-alignment-v0.1.9.md`
- `24-pi-context-usage-hardening-v0.1.10.md`
- `25-route-dsl-e2e-v0.1.11.md`
- `26-evaluation-runner-v0.1.12.md`
- `27-evaluation-isolation-report-v0.1.13.md`
- `28-benchmark-identity-quality-v0.1.14.md`
- `29-benchmark-metric-semantics-v0.1.15.md`
- `30-quality-envelope-semantics-v0.1.16.md`
- `31-quality-stdout-separation-v0.1.17.md`
- `32-takt-authoring-skill-v0.1.18.md`
- `33-opencode-adapter-v0.1.19.md`
- `34-opencode-provider-diagnostics-v0.1.20.md`
- `35-profile-packages-code-v0.1.21.md`
- `36-workflow-composition-v0.1.22.md`
- `37-composition-hardening-v0.1.23.md`
- `38-archon-workflow-catalog-v0.1.24.md`
- `39-git-worktree-isolation-v0.1.25.md`
- `40-governed-child-runs-v0.1.26.md`
- `41-node-capability-policies-v0.1.27.md`
- `42-governed-child-fanout-v0.1.28.md`
- `43-script-nodes-typed-artifacts-v0.1.29.md`
- `44-local-mcp-control-plane-v0.1.30.md`
- `45-agent-events-external-executor-v0.1.31.md`
- `46-controlled-agent-events-deep-workflows-v0.1.32.md`
- `47-authoring-local-daemon-v0.1.33.md`
- `48-dynamic-takt-v0.1.34.md`
- `49-trusted-block-packages-v0.1.35.md`
- `50-coding-agent-host-control-v0.1.36.md`
- `51-autonomous-run-operations-v0.1.37.md`
- `52-simple-reliable-agent-neutral-router-v0.1.38.md`
- `53-role-brief-controls-v0.1.39.md`
- `54-evidence-baseline-failure-routing-v0.1.40.md`
- `55-adapter-platform-v0.1.41.md`
- `56-portable-package-distribution-v0.1.42.md`
- `57-multi-repo-dynamic-workflows-v0.1.43.md`
- `58-runtime-reliability-local-security-v0.1.44.md`
- `59-route-dsl-evaluation-strategy-benchmark-v0.1.45.md`
- `60-task-level-dynamic-evaluation-v0.1.46.md`
- `61-v0.2-stabilization-iteration-history-v0.1.47.md`
- `62-contract-convergence-compatibility-v0.1.48.md`
- `63-reference-external-adapters-v0.1.49.md`
- `64-structured-task-sources-v0.1.50.md`
- `65-human-reviewed-learning-loop-v0.1.51.md`
- `66-application-boundary-architecture-refactor-v0.1.52.md`
- `67-go-native-test-architecture-v0.1.53.md`
- `68-architecture-hardening-v0.1.54.md`
- `69-core-stabilization-modularization-v0.1.55.md`

## 2. Источники истины

| Вопрос | Основной документ | Статус |
|---|---|---|
| Правила работы кодовых агентов | `../AGENTS.md` | действующий |
| Создание и настройка workflow | `../skills/takt/SKILL.md`, `47-authoring-local-daemon-v0.1.33.md`, `46-controlled-agent-events-deep-workflows-v0.1.32.md`, `43-script-nodes-typed-artifacts-v0.1.29.md`, `42-governed-child-fanout-v0.1.28.md`, `41-node-capability-policies-v0.1.27.md`, `40-governed-child-runs-v0.1.26.md`, `39-git-worktree-isolation-v0.1.25.md`, `38-archon-workflow-catalog-v0.1.24.md`, `36-workflow-composition-v0.1.22.md`, `37-composition-hardening-v0.1.23.md` | действующий скилл, каталог, роутер и композиция |
| Зачем нужен Takt | `01-project.md` | действующий |
| Архитектурный подход | `02-approach.md`, ADR | действующий |
| Текущий внешний контракт | `03-specification.md` | реализованный `v1alpha1` |
| Script-узлы и типизированные артефакты | `43-script-nodes-typed-artifacts-v0.1.29.md` | фактический |
| Динамический fan-out governed children | `42-governed-child-fanout-v0.1.28.md` | фактический |
| Текущее состояние кода | `05-implementation-status.md` | фактический |
| Архитектурные и тестовые границы | `72-architecture-contracts-v0.1.57.md`, `71-canonical-operation-contracts.generated.md`, `70-codebase-hygiene-stabilization-v0.1.56.md`, `../internal/application/`, `../internal/externalworker/`, `../internal/runcontrol/`, `../internal/architecture/`, `../tests/e2e/` | stable dependency direction + focused Run/external-worker use cases + modular experimental/extensions/tooling + Go-native contracts |
| Archon-first Takt YAML и bounded repair loops | `proposals/002-archon-compatible-flow-runtime.md`, `superpowers/specs/2026-08-11-archon-compatible-flow-runtime-spec.md`, `07-archon-compatibility.md` | A0/A1 реализованы: единый target language, loop/signal/evidence/session/recovery; `CONDITIONAL` остаются live budgets и mutating parallel merge |
| v0.2 contract convergence и compatibility | `62-contract-convergence-compatibility-v0.1.48.md`, `61-v0.2-stabilization-iteration-history-v0.1.47.md`, `09-runtime-semantics.md`, `../schemas/schema-subset-v1.schema.json`, `../schemas/v1beta1-field-matrix.schema.json` | schema subset/field audit/matrix фактические; v1beta1 ещё не выпущен |
| Multi-repo Dynamic Workflows | `57-multi-repo-dynamic-workflows-v0.1.43.md`, `../schemas/workspace.schema.json` | фактический |
| Runtime reliability, SecretRef и локальный OS sandbox | `58-runtime-reliability-local-security-v0.1.44.md`, `../schemas/workflow.schema.json`, `../schemas/run-state.schema.json` | фактический |
| Переносимые BlockPackage и lock/policy | `56-portable-package-distribution-v0.1.42.md`, `03-specification.md` | фактический |
| Domain Adapter Platform | `55-adapter-platform-v0.1.41.md`, `../schemas/domain-adapter-protocol.schema.json` | фактический |
| Локальный MCP control plane | `44-local-mcp-control-plane-v0.1.30.md` | фактический |
| Строгий authoring и локальный daemon | `47-authoring-local-daemon-v0.1.33.md` | фактический |
| Dynamic Takt | `48-dynamic-takt-v0.1.34.md` | фактический |
| Доверенные пакеты блоков | `49-trusted-block-packages-v0.1.35.md`, `../schemas/block-package.schema.json`, `../examples/corporate-block-package/` | фактический |
| Coding Agent Host Control | `50-coding-agent-host-control-v0.1.36.md`, `../integrations/coding-agent-host-control/` | Go strict-core фактический; bundled Pi/OpenCode guarded до live smoke |
| Evidence, baseline, parking и external reconciliation | `54-evidence-baseline-failure-routing-v0.1.40.md`, `../schemas/evidence-manifest.schema.json` | фактический |
| Role Contract, TaskBrief и управляемые проверки | `53-role-brief-controls-v0.1.39.md`, `../schemas/task-brief.schema.json` | фактический |
| Task Router, compact Task API и agent-neutral adapter | `52-simple-reliable-agent-neutral-router-v0.1.38.md`, `proposals/001-simple-reliable-agent-neutral-takt.md` | фактический срез и принятый proposal |
| Автономная эксплуатация Run | `51-autonomous-run-operations-v0.1.37.md` | фактический |
| Управляемые agent events и глубокие workflow | `46-controlled-agent-events-deep-workflows-v0.1.32.md` | фактический |
| Исправления последнего аудита | `16–18-audit-remediation-*.md` | фактический |
| Fake-assistant protocol suite | `20-fake-assistant-contract-v0.1.6.md` | фактический |
| Строгая OS/envelope семантика | `21-protocol-hardening-v0.1.7.md` | фактический |
| Pi RPC adapter | `22-pi-adapter-v0.1.8.md`, `23-pi-rpc-alignment-v0.1.9.md`, `24-pi-context-usage-hardening-v0.1.10.md` | фактический |
| OpenCode adapter | `33-opencode-adapter-v0.1.19.md`, `34-opencode-provider-diagnostics-v0.1.20.md`, `../examples/opencode-smoke/` | фактический |
| Route DSL end-to-end | `25-route-dsl-e2e-v0.1.11.md`, `../examples/route-dsl-e2e/` | контрактный срез реализован |
| Evaluation runner и метрики | `60-task-level-dynamic-evaluation-v0.1.46.md`, `59-route-dsl-evaluation-strategy-benchmark-v0.1.45.md`, `26-evaluation-runner-v0.1.12.md`, `27-evaluation-isolation-report-v0.1.13.md`, `28-benchmark-identity-quality-v0.1.14.md`, `29-benchmark-metric-semantics-v0.1.15.md`, `30-quality-envelope-semantics-v0.1.16.md`, `31-quality-stdout-separation-v0.1.17.md`, `../examples/route-dsl-eval/`, `../examples/route-dsl-benchmark/` | workflow matrix/compare/gates и task-level Router/Dynamic/replan benchmark реализованы; production evidence требует реальных corpus/models/validators |
| Production flow evaluation | `superpowers/specs/2026-08-12-production-flow-evaluation-design.md` | подтверждённый дизайн; реализация не начата |
| Восстановление документации релиза | `19-document-recovery-v0.1.5.md` | фактический |
| Граница безопасности | `../SECURITY.md` | действующий |
| Ближайшее целевое состояние | `08-target-v0.2.md` | целевой |
| Семантика runtime | `09-runtime-semantics.md` | реализовано частично/цель v0.2 |
| Контракт исполнителя | `10-assistant-adapter-spec.md` | process protocol, Pi и OpenCode adapters реализованы; capability discovery и policy mapping реализованы |
| Очередность работ | `11-implementation-plan.md` | рабочий план |
| Задачи | `14-backlog-v0.2.md` | рабочий backlog |
| Старт кодового агента | `15-coding-agent-start.md` | готовая инструкция |
| Отличия от Archon | `07-archon-compatibility.md` | справочный |
| Машиночитаемые форматы | `schemas/*.json`, включая `evidence-manifest.schema.json` и `notification-config.schema.json` | текущая alpha-схема |

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

- `46-controlled-agent-events-deep-workflows-v0.1.32.md` — event protocol v2, управляемые tool calls и глубокие шесть workflow с Git/recovery contract.

- `47-authoring-local-daemon-v0.1.33.md` — authoring preflight, строгий renderer, always/idle semantics и локальный Unix-socket daemon.

- `48-dynamic-takt-v0.1.34.md` — ограниченный WorkflowPlan, checkpoint replanning, coding-agent flow, MCP/CLI и promotion.

- `49-trusted-block-packages-v0.1.35.md` — BlockPackage, корпоративный governance, fingerprint каталога и исправления Dynamic Takt.

- `50-coding-agent-host-control-v0.1.36.md` — host-control core, guarded Pi/OpenCode integrations и исправления v0.1.35.

- `53-role-brief-controls-v0.1.39.md` — внутренние роли, bounded TaskBrief, path scope, deny/repair/warn и исправления автономного control plane.

- `52-simple-reliable-agent-neutral-router-v0.1.38.md` — Task Router, simple-reliable, MCP surfaces и логический coding-agent.

- `proposals/001-simple-reliable-agent-neutral-takt.md` — сводное развитие после анализа надёжных flow и KiroCrew.

- `51-autonomous-run-operations-v0.1.37.md` — реестр Run, attention, pause/resume/retry/fork/abandon, recovery, notifications и summary.

- `58-runtime-reliability-local-security-v0.1.44.md` — durable backoff/diagnostics, fan-out early exit, SecretRef/redaction, локальный OS sandbox, NodePath и исправления ревью multi-repo.

- Structured Task Sources / ingress protocol: `docs/64-structured-task-sources-v0.1.50.md`, `sdk/tasksource`, `schemas/task-source*.schema.json`.

- Application boundary / architecture refactor: `docs/66-application-boundary-architecture-refactor-v0.1.52.md`, hardening `docs/68-architecture-hardening-v0.1.54.md`, modularization `docs/69-core-stabilization-modularization-v0.1.55.md`, hygiene `docs/70-codebase-hygiene-stabilization-v0.1.56.md`, architecture contracts `docs/72-architecture-contracts-v0.1.57.md`, generated operations `docs/71-canonical-operation-contracts.generated.md`, `internal/application`, `internal/externalworker`, `internal/runcontrol`, `internal/experimental`, `internal/extensions`, `internal/tooling`, `internal/bootstrap`, `internal/appapi`, `internal/architecture`, `go test ./internal/architecture`.

- Human-reviewed Skill/Block Learning Loop: `docs/65-human-reviewed-learning-loop-v0.1.51.md`, `internal/experimental/learning`, `schemas/learning-proposal.schema.json`, Go regression/E2E tests.
