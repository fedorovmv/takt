# План развития Takt

Актуальный план после `v0.1.47-alpha`. История реализованных срезов находится в `05-implementation-status.md` и релизных документах `docs/18-*.md` … `docs/61-*.md`.

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

Stabilization начат в `v0.1.47`, но `v1beta1` не замораживается до production evidence.

В `v0.1.47` уже выполнены:

- первичный audit внешних contracts (`stable-candidate | supported-alpha | deprecated | internal`);
- first-class bounded iteration history;
- решение оставить nested `loop_group` запрещённым в `v0.2`;
- draft migration policy `v1alpha1 → v1beta1`.

Остаётся после первого production evidence:

- решение по границе `output_format`;
- adapter/host compatibility matrix;
- deprecation policy;
- финальная field-by-field migration `v1alpha1 → v1beta1`;
- очистка исторических полей и документации, которые больше не являются частью целевого API.

Цель v0.2 — не максимальное число функций, а небольшой набор доказанных стабильных контрактов.

## P2. External seams

После стабилизации публичных контрактов:

- live Pi/OpenCode host conformance;
- один reference external coding-agent wrapper через `sdk/agentadapter`;
- один production-like SCM adapter;
- один structured task source adapter.

Эти реализации должны жить поверх публичных SDK/контрактов и выявлять недостающие seams без provider-specific логики в runtime.

## P3. Learning и UX

Следующая крупная продуктовая тема после v0.2:

- Run history → candidate skill/block → human review → package → eval;
- `workflow graph/explain/scaffold` и расширенный `plan explain`;
- bounded reject/revise для статических approvals.

Learning loop не имеет права автоматически изменять trusted package: принятие и evaluation остаются обязательными gates.

## Отложено

Server, Web UI, БД, remote workers, Slack/Telegram и multi-user authorization не входят в локальный v0.2. Они рассматриваются только при появлении нелокального сценария и отдельной threat model.
