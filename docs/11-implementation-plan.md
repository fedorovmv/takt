# План реализации Takt v0.2

Актуальный план после `v0.1.46-alpha`. Исторические этапы и реализованные возможности перечислены в `05-implementation-status.md`; здесь остаются только действия, которые влияют на решение о стабилизации `v0.2`.

## 1. Принцип

Следующий механизм добавляется только тогда, когда существующий evaluation или production-сценарий показывает измеримый пробел. До появления evidence приоритет имеют проверка качества, упрощение контрактов и устранение расхождений между схемой, кодом и документацией.

Порядок:

```text
измерить
→ понять реальный gap
→ исправить минимальный слой
→ повторить benchmark
→ только затем стабилизировать контракт
```

## 2. Веха E0 — измерительный контур task-level — выполнено в v0.1.46-alpha

Реализован `takt eval task-benchmark`, который запускает настоящий control path:

```text
Task
→ Task Router
→ workflow | template | dynamic
→ Dynamic Plan
→ checkpoint
→ bounded replan
→ terminal result
```

Он измеряет route accuracy, final success, plan revisions, replanner/execution Runs, router fallback, needs-input, usage и попарные переходы baseline/candidate. Regression gates записываются после формирования полного отчёта.

Deterministic fixture доказывает correctness измерений, а не качество модели.

## 3. Веха E1 — Live Route DSL evidence

Нужно прогнать уже существующий `EvaluationMatrix` на реальном обезличенном corpus и штатном `route-tool`.

Минимум сравнить:

- `baseline-direct`;
- `feedback-repair`;
- `simple-reliable`;
- Dynamic Takt, если задача допускает task-level маршрут.

Для каждого case — не менее трёх повторов на одинаковых fingerprints.

Фиксировать:

- success@1 и final success;
- time-to-valid;
- attempts/retries;
- tokens/cost;
- stable/unstable cases;
- manual correction, если она была нужна.

**Gate:** без этого результата нельзя утверждать, что Dynamic/repair стратегия полезнее простого agent loop.

## 4. Веха E2 — проверка универсальности

Два production-like набора, не требующие изменения runtime:

### Go

```text
Task Router
→ inspect/implement
→ deterministic go checks
→ optional repair/review
→ evidence
```

### Document

```text
Task Router
→ draft
→ deterministic structure checks
→ approval/comment
→ revise
→ final result
```

Цель — подтвердить исходную границу Takt: Route DSL, Go и документы используют одно ядро, а предметная логика живёт в workflow/blocks/skills/adapters.

## 5. Веха S1 — contract/schema audit v0.2

После E1/E2:

1. составить inventory публичных `v1alpha1/v1alpha2` контрактов;
2. для каждого поля указать `keep | deprecate | migrate | remove`;
3. синхронизировать Go validation и JSON Schema;
4. решить судьбу nested `loop_group`;
5. определить, нужна ли полная JSON Schema для `output_format`;
6. сформировать полную модель iteration history вместо только последней итерации;
7. зафиксировать adapter/host compatibility matrix.

Результат — проект `v1beta1`, а не новая runtime-фича.

## 6. Веха S2 — migration в v1beta1

- версионированные схемы;
- migration guide `v1alpha1 → v1beta1`;
- при необходимости автоматический migrator;
- contract tests старых документов;
- compatibility policy для пакетов и adapters;
- changelog несовместимых изменений.

## 7. Веха X1 — доказать внешние seams

После стабилизации интерфейсов:

- live host conformance для одной фиксированной версии Pi/OpenCode;
- один внешний coding-agent wrapper, использующий публичный `sdk/agentadapter`;
- один production-like SCM adapter, предпочтительно GitHub как reference implementation;
- один task source adapter, например Issue/Tracker item → Task.

Это должно доказать SDK, а не расширять core.

## 8. Веха L1 — human-reviewed learning loop

Использовать накопленные Run/evaluation для предложения улучшений:

```text
Run history
→ повторяющийся pattern/failure
→ candidate skill/block
→ provenance + affected cases
→ human review
→ package update
→ evaluation gate
```

Takt не должен автоматически изменять trusted package без review.

## 9. UX после стабилизации

Полезные, но не блокирующие `v0.2` функции:

- `takt workflow graph`;
- `takt workflow explain`;
- `takt workflow scaffold`;
- `takt plan explain`;
- декларативный `approval.on_reject` / revise flow для статических процессов.

## 10. Пока не начинать

Без отдельного use case и threat model:

- Web UI;
- серверную очередь и отдельную БД;
- remote workers;
- marketplace;
- Slack/Telegram gateways;
- multi-user auth/RBAC;
- собственный LLM/tool-calling agent loop;
- собственные filesystem/LSP инструменты кодинг-агента.
