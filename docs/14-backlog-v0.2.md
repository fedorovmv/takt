# Актуальный backlog Takt v0.2

Статус пересобран после `v0.1.52-alpha`. Этот документ содержит открытые задачи и явно отмеченные стабилизационные решения. Выполненные срезы находятся в `05-implementation-status.md` и `06-roadmap.md`.

## P-1. Архитектурный долг — закрыт в v0.1.52

### ARCH-001. Application/transport/runtime boundaries — выполнено

Удалён `internal/control.Service`, введены use-case application services и production `bootstrap`; `cmd/takt` оставлен launcher, daemon/MCP используют canonical API, filesystem persistence подключается через `RunStore`, runtime dependencies явные. `scripts/test-architecture.sh` защищает границы от регрессии.

Новый DI/plugin/event-bus framework не вводился; refactor не изменил внешний API/state contracts.

## P0. Доказать полезность текущего Takt

### EVIDENCE-001. Live Route DSL matrix

Запустить неизменную `EvaluationMatrix` из `v0.1.45` на реальном обезличенном corpus, штатном Route DSL validator и фактически используемых coding-agent/model конфигурациях.

Нужно получить:

- не менее трёх повторов каждого case/strategy;
- `success@1`, final success, attempts-to-valid, time-to-valid, tokens/cost;
- stable/unstable cases и diagnostic fingerprints;
- manual corrections отдельной метрикой;
- сравнение `baseline-direct`, feedback/simple-reliable и Dynamic Takt там, где стратегии применимы.

Synthetic `production-shaped` corpus остаётся regression fixture и не считается production evidence.

### EVIDENCE-002. Task-level Dynamic Takt benchmark — реализовано в v0.1.46

`takt eval task-benchmark` проверяет полный управляющий путь:

```text
Task
→ semantic Router
→ workflow | template | dynamic
→ Dynamic Plan
→ checkpoint
→ replan
→ execution
→ terminal result
```

Остаётся прогнать этот контракт на реальных задачах и моделях. Встроенный deterministic fixture доказывает correctness измерительного контура, а не качество LLM.

### EVIDENCE-003. Универсальность на трёх предметных классах

Зафиксировать production-like наборы для:

1. Route DSL;
2. Go-разработки;
3. подготовки технического документа.

Критерий: все три сценария используют одно ядро Takt; предметные различия выражаются workflow, blocks, roles, skills, validators и adapters, а не изменениями scheduler/runtime.

## P1. v0.2 Stabilization

### STABLE-001. Ревизия внешних контрактов — выполнено в v0.1.47

Контракты классифицированы как `stable-candidate | supported-alpha | deprecated | internal` в `docs/61-v0.2-stabilization-iteration-history-v0.1.47.md`:

- `takt/v1alpha1 Workflow`;
- Config/Profile/BlockPackage;
- TaskRoute/WorkflowPlan;
- run state/events;
- `takt-assistant/v1alpha1|v1alpha2`;
- Agent/Domain Adapter SDK;
- evaluation formats;
- MCP agent/host/worker/operator surfaces.

### STABLE-002. План `v1alpha1 → v1beta1` — draft в v0.1.47

Draft compatibility/migration policy подготовлен. Финальная field-by-field migration и, при необходимости, migrator выполняются только после evidence из P0 и не должны фиксировать механизмы, которые не доказали пользу.

### STABLE-003. Полная история iteration state — выполнено в v0.1.47

Добавлен `loop_iterations[]` со всеми завершёнными iteration snapshots. `loop_previous` сохранён как compatibility alias последней итерации; `max_iterations <= 64` ограничивает durable state.

### STABLE-004. Решение по nested composition — выполнено в v0.1.47

Для `v0.2` и первого `v1beta1` nested `loop_group` явно остаётся запрещённым. Возврат этой возможности требует отдельного production use case и совместимого расширения контракта.

### STABLE-005. Граница structured output — выполнено в v0.1.48

Текущий контракт зафиксирован как `takt-schema-subset/v1` и используется одинаково для `input.schema` и `output_format`. Полный JSON Schema не входит в v0.2; расширение возможно только новой совместимой версией по production evidence.

### STABLE-006. Compatibility matrix adapters/hosts — выполнено в v0.1.48

Добавлены `takt compatibility matrix|check`, разделяющие session adapter, host integration и domain adapter. Bundled Pi/OpenCode host остаются `guarded`; `strict` требует live conformance на pinned version.

### STABLE-007. Field-by-field v1beta1 audit — выполнено в v0.1.48

`takt compatibility fields` и contract-test фиксируют точный набор публичных полей stable-candidate authoring/config contracts. `executor`, `native_hooks` и `tool_approval` остаются `supported-alpha/defer`; process protocol `v1alpha1` deprecated для новых wrappers.

### STABLE-008. Финальная v1beta1 migration — после P0 evidence

На основании live Route DSL + Go + Document evidence подтвердить/скорректировать field decisions, выпустить migration guide и только при необходимости автоматический migrator.

## P2. Доказать внешние seams

### SEAM-001. Live Pi/OpenCode host conformance

Проверить `/takt`, input interception, tool blocking, completion blocking и recovery на зафиксированных версиях host. До этого bundled integrations остаются `guarded`.

### SEAM-002. Один внешний coding-agent wrapper — реализовано в v0.1.49

`qwen-takt-adapter` реализует `takt-assistant/v1alpha2` только через public `sdk/agentadapter`, поддерживает headless Qwen Code fresh/exact resume/model/usage и проходит общий conformance kit. Capability surface намеренно узкий и не выдаёт transport version за `tool_control`.

### SEAM-003. Один production-like Domain Adapter — реализовано в v0.1.49

`takt-github-scm-adapter` реализует neutral SCM contract через public `sdk/domainadapter` и `gh`: repository/change/check reads, change create/comment/review и reconcile неизвестного side effect. Public domain request получил execution `workspace`, multi-repo publication — точный `repository_workspace`. Корпоративные Git/Tracker/CI должны использовать тот же SDK.

### SEAM-004. Structured task source adapter — реализовано в v0.1.50

`takt-task-source/v1alpha1` и public `sdk/tasksource` приводят внешний объект к normalized Task до Router. `takt task start`/`takt.task.start` принимают `source + source_ref`; provenance/revision сохраняются в plan и передаются Router/Planner/Replanner. Reference GitHub Issue source доказан E2E. Корпоративные tracker/OpenSpec/PRD adapters используют тот же протокол и не требуют новой core-фичи.

## P3. Product learning и UX

### LEARN-001. Skill/Block Learning Loop — реализовано в v0.1.51

`takt learn scan|propose|review|evaluate|stage` реализует управляемый путь от повторяемого durable fingerprint к immutable candidate snapshot. Proposal сохраняет supporting Run IDs, expected benefit, human rationale и matrix evaluation provenance. Stage доступен только после accept + passing gates и пишет в `.takt/learning/ready`, не изменяя trusted package/skill configuration.

### UX-001. Представление процесса

Небольшой локальный UX без Web Builder:

```text
takt workflow graph
takt workflow explain
takt workflow scaffold
takt plan explain
```

Цель — объяснять DAG, extension points, approvals, policies и выбранные controls, не вводя отдельный server/UI runtime.

### FLOW-001. Декларативный reject/revise

Добавить для статических процессов проверяемый `approval.on_reject`/revise path. Dynamic Takt уже умеет steering/replan, но статический корпоративный workflow должен иметь явный bounded контракт пересмотра.

## По evidence, а не по календарю

Следующие идеи остаются условными до появления фактической потребности:

- path-level OS write allowlists сверх текущего read-only sandbox;
- constrained Route DSL generation, RAG examples, N-candidate selection, DSPy/GEPA optimization как отдельные benchmark strategies;
- object storage;
- database-backed store;
- server/Web UI;
- remote workers;
- message adapters;
- multi-user auth/RBAC.

## Не является backlog ядра

Takt не должен становиться ещё одним coding-agent. В ядро не планируются собственные `read/edit/bash` tools для LLM, LSP, model tool-loop, agent conversation memory или TUI редактора. Этим управляет конкретный coding-agent; Takt оркестрирует сессии и проверяемый процесс вокруг них.
