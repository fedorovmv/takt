# План развития Takt

Актуальный план после `v0.1.51-alpha`. История реализованных срезов находится в `05-implementation-status.md` и релизных документах `docs/18-*.md` … `docs/65-*.md`.

## Текущая позиция

Takt уже закрывает основной локальный control plane: workflow/runtime, child Runs и fan-out, worktree/multi-repo, Dynamic Takt, host control, autonomous operations, evidence/failure routing, adapters/packages, local security и сравнительный evaluation.

Главный риск теперь — продолжать добавлять механизмы быстрее, чем появляется evidence их пользы. Поэтому ближайший порядок меняется с feature-driven на evidence-driven.

## P0. Evidence

### 1. Live Route DSL benchmark

Первый внешний gate перед стабилизацией: реальные обезличенные задачи + штатный validator + реальные модели/агенты. `v0.1.45` уже предоставляет matrix/repeat/compare/gates; synthetic corpus остаётся только regression fixture.

### 2. Task-level Dynamic Takt benchmark — v0.1.46

`v0.1.46-alpha` добавляет воспроизводимую проверку полного пути:

```text
Task → Router → template/dynamic → checkpoint → replan → result
```

Benchmark отдельно измеряет route accuracy, terminal success, plan revisions, replanner runs, unexpected needs-input, router fallback и usage. Это позволяет отличать «задача как-то завершилась» от «Takt выбрал и адаптировал правильный процесс».

### 3. Go + Document production scenarios

Тем же evaluation-подходом доказать исходную универсальность Takt на Go-разработке и подготовке технического документа.

## P1. v0.2 Stabilization

Stabilization начат в `v0.1.47` и продолжен в `v0.1.48`, но `v1beta1` не замораживается до production evidence.

В `v0.1.47` уже выполнены:

- первичный audit внешних contracts (`stable-candidate | supported-alpha | deprecated | internal`);
- first-class bounded iteration history;
- решение оставить nested `loop_group` запрещённым в `v0.2`;
- draft migration policy `v1alpha1 → v1beta1`.

В `v0.1.48` выполнены:

- текущий structured contract заморожен как `takt-schema-subset/v1`, без обещания полного JSON Schema;
- добавлены `compatibility matrix|check` для session/host/domain seams;
- добавлен machine-readable field-by-field audit stable-candidate API;
- зафиксирована deprecation boundary process `v1alpha1` → целевой `v1alpha2` для новых wrappers.

Остаётся после production evidence:

- финальная migration `v1alpha1 → v1beta1` по фактически используемым полям;
- при необходимости migrator и cleanup deprecated compatibility fields;
- live host/external seam evidence, которое может уточнить supported-alpha части до freeze.

Цель v0.2 — не максимальное число функций, а небольшой набор доказанных стабильных контрактов.

## P2. External seams

P2 продолжен в `v0.1.50-alpha`. Уже реализованы:

- reference Qwen Code wrapper через `sdk/agentadapter`;
- reference GitHub SCM adapter через `sdk/domainadapter`;
- execution `workspace` как нейтральный domain-request context;
- conservative process-v1alpha2 capability negotiation без неявного `tool_control`;
- Structured Task Sources: внешний issue/tracker/PRD contract → normalized Task → существующий Router/Dynamic Takt;
- reference GitHub Issue source через public `sdk/tasksource`.

Остаётся основной внешний gate:

- live Pi/OpenCode host conformance на pinned versions.

Дополнительные tracker/OpenSpec/PRD source adapters являются integration-level расширениями уже проверенного `takt-task-source/v1alpha1`, а не новой core-фичей.

Reference implementations живут поверх публичных SDK/контрактов; provider-specific логика не переносится в runtime.

## P3. Learning и UX

P3 открыт в `v0.1.51-alpha`. Реализован первый самостоятельный срез:

- Run history → repeated fingerprint → immutable candidate skill/block snapshot;
- human review с rationale;
- существующий matrix evaluation + regression gates;
- staged ready candidate без автоматической установки trusted package/skill.

Следующие P3-срезы:

- `workflow graph/explain/scaffold` и расширенный `plan explain`;
- bounded reject/revise для статических approvals.

Learning loop сохраняет trust boundary: принятие и evaluation обязательны, а активация staged candidate остаётся отдельным явным действием.

## Отложено

Server, Web UI, БД, remote workers, Slack/Telegram и multi-user authorization не входят в локальный v0.2. Они рассматриваются только при появлении нелокального сценария и отдельной threat model.
