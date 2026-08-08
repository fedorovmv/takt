# Актуальный backlog Takt v0.2

Статус пересобран после `v0.1.46-alpha`. Этот документ содержит только незакрытые задачи и осознанно не повторяет историю релизов. Выполненные срезы находятся в `05-implementation-status.md` и `06-roadmap.md`.

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

### STABLE-001. Ревизия внешних контрактов

Проверить и классифицировать как `keep | deprecate | internal`:

- `takt/v1alpha1 Workflow`;
- Config/Profile/BlockPackage;
- TaskRoute/WorkflowPlan;
- run state/events;
- `takt-assistant/v1alpha1|v1alpha2`;
- Agent/Domain Adapter SDK;
- evaluation formats;
- MCP agent/host/worker/operator surfaces.

### STABLE-002. План `v1alpha1 → v1beta1`

Подготовить migration guide и схемы совместимости. `v1beta1` вводится только после evidence из P0 и не должен фиксировать механизмы, которые не доказали пользу.

### STABLE-003. Полная история iteration state

Заменить ограниченную модель `LoopPrevious` на first-class историю итераций там, где это требуется для диагностики/evidence/resume. Сохранить bounded storage и совместимость публичного результата.

### STABLE-004. Решение по nested composition

Определить, нужен ли nested `loop_group` в стабильном контракте. Реализовывать только вместе с path-based namespace и проверяемым resume, либо явно оставить запрет в `v1beta1`.

### STABLE-005. Граница structured output

По production evidence решить, достаточно ли текущего проверяемого subset `output_format` или нужен более полный JSON Schema. Не расширять контракт без фактических ограничений.

### STABLE-006. Compatibility matrix adapters/hosts

Зафиксировать проверенные версии и capability level для Pi/OpenCode и внешних wrappers. `strict` host control объявляется только после live conformance на конкретной версии.

## P2. Доказать внешние seams

### SEAM-001. Live Pi/OpenCode host conformance

Проверить `/takt`, input interception, tool blocking, completion blocking и recovery на зафиксированных версиях host. До этого bundled integrations остаются `guarded`.

### SEAM-002. Один внешний coding-agent wrapper

Реализовать reference wrapper вне внутренних пакетов Takt через `sdk/agentadapter` для одного из Codex / Oh My Pi / Qwen CLI. Цель — доказать достаточность публичного SDK, а не собрать каталог адаптеров.

### SEAM-003. Один production-like Domain Adapter

Реализовать reference SCM adapter, предпочтительно GitHub, как отдельную поставку поверх `SCM/Tracker/CI` contracts. Корпоративные Git/Tracker/CI должны подключаться тем же SDK без изменений workflow.

### SEAM-004. Structured task source adapter

Добавить один реальный входной источник (`issue`, tracker item, OpenSpec/PRD или JSON/YAML contract), который приводит внешний объект к Task input до Router. Не смешивать эту границу с domain operations внутри workflow.

## P3. Следующий продуктовый скачок

### LEARN-001. Skill/Block Learning Loop

Построить только как управляемое предложение:

```text
Run history
→ повторяющийся паттерн/ошибка
→ candidate skill/block
→ provenance + примеры Run + ожидаемая польза
→ human review
→ package update
→ evaluation gate
```

Автоматическая мутация trusted packages запрещена. Новый skill/block становится активным только после явного принятия и regression evaluation.

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
