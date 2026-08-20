# План реализации Takt v0.2

Актуальный план после `v0.1.64-alpha`. Исторические этапы и реализованные возможности перечислены в `05-implementation-status.md`; здесь остаются только действия, которые влияют на решение о стабилизации `v0.2`.

## 0. Веха A1 — application boundary refactor — выполнено в v0.1.52

До новых feature slices выполнен feature freeze: thin CLI transports, application services, production bootstrap, canonical daemon/MCP API, RunStore port, explicit runtime dependencies и architecture gate. Детали — `docs/archive/releases/66-application-boundary-architecture-refactor-v0.1.52.md`, ADR-085.

## 0.5. Веха A2 — Test architecture refactor — выполнено в v0.1.53

38 shell contract suites сведены к Go unit/component + black-box `tests/e2e`; после `v0.1.54` оставлен один TypeScript compiler smoke. Schema registry больше не имеет отдельной Python-семантики. Architecture gate защищает test boundary. Детали — `docs/archive/releases/67-go-native-test-architecture-v0.1.53.md`, ADR-086.

## 0.75. Веха A3 — Architecture hardening — выполнено в v0.1.54

Повторный аудит после A1/A2 закрыл остаточную связность: private acyclic application dependencies, отдельный Fork coordinator, bootstrap-only concrete wiring, explicit runtime/evaluation composition, signal-aware context propagation, durable plan/host locks, decomposition state-machine hotspots и финальная миграция shell assertions в Go. Детали — `docs/archive/releases/68-architecture-hardening-v0.1.54.md`, ADR-087.

## 1. Принцип

Следующий механизм добавляется только тогда, когда существующий evaluation или production-сценарий показывает измеримый пробел. До появления evidence приоритет имеют проверка качества, упрощение контрактов и устранение расхождений между схемой, кодом и документацией.

Текущая repo-owned последовательность после gap audit 2026-08-20:

1. единый Run/assessment/evaluation contract по
   `superpowers/specs/2026-08-20-unified-run-evaluation-design.md` реализован в
   `v0.1.63-alpha`, включая общий `run status|stats|inspect|assessment`;
2. принять результаты user-owned production workflow и `eval-feature`, не
   обращаясь к внешнему workspace и не запуская параллельный live eval;
3. повторить host conformance на выбранных pinned versions после завершения
   текущего eval;
4. формировать финальную v1beta1 migration только после production evidence.

Порядок:

```text
измерить
→ понять реальный gap
→ исправить минимальный слой
→ повторить benchmark
→ только затем стабилизировать контракт
```

## 1.5. Веха E-Run — единый evaluation Run — выполнено в v0.1.63-alpha

Evaluation запускается как один ordinary Run; case/repeat — ordered matrix
branches, а произвольный authored DAG создаёт deterministic primary assessments
с immutable target/evidence provenance. Gates отделены от technical Run status.
Legacy suite runner и directory readers оставлены только для compatibility.
Фактический контракт перечислен в `05-implementation-status.md`; здесь он не
дублируется.

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

## 5. Веха S1 — contract/schema audit v0.2 — внутренний срез завершён в v0.1.48

`v0.1.47` дал contract inventory, bounded iteration history и решение по nested `loop_group`. `v0.1.48` завершает внутреннюю часть S1:

1. field-level matrix для stable-candidate authoring/config contracts;
2. `takt-schema-subset/v1` как окончательная граница structured input/output в v0.2;
3. machine-readable compatibility matrix session/host/domain adapters;
4. CLI preflight `takt compatibility check`, включая optional live version/Describe probes.

После E1/E2 остаётся только подтвердить решения production evidence и сформировать окончательный `v1beta1` migration.

## 6. Веха S2 — migration в v1beta1

- версионированные схемы;
- migration guide `v1alpha1 → v1beta1`;
- при необходимости автоматический migrator;
- contract tests старых документов;
- compatibility policy для пакетов и adapters;
- changelog несовместимых изменений.

## 7. Веха X1 — доказать внешние seams — продолжено в v0.1.50

`v0.1.49` закрывает два пункта без расширения core:

- `qwen-takt-adapter` использует только public `sdk/agentadapter`;
- `takt-github-scm-adapter` использует только public `sdk/domainadapter`;
- domain request получил универсальный execution `workspace`;
- runtime проверяет фактическую v1alpha2 declaration и не выводит `tool_control` из версии протокола;
- `v0.1.50` добавляет public `sdk/tasksource`, protocol `takt-task-source/v1alpha1` и reference GitHub Issue source.

Остаётся core-level внешний gate:

- live host conformance для одной фиксированной версии Pi/OpenCode.

Дополнительные Issue/Tracker/OpenSpec/PRD source implementations используют уже доказанный Task Source protocol и не требуют нового core seam.

Live Qwen/GitHub smoke выполняется только при наличии реальных credentials и не подменяется release fixture.

## 8. Веха L1 — human-reviewed learning loop — выполнено в v0.1.51

Использовать накопленные Run/evaluation для предложения улучшений:

```text
Run history
→ повторяющийся pattern/failure
→ candidate skill/block
→ provenance + affected cases
→ human review
→ evaluation gate
→ staged candidate
→ explicit adoption
```

Реализовано: proposal хранит immutable candidate SHA-256 и supporting Run IDs, human review обязателен, matrix report фиксируется по hash/fingerprint, а stage пишет только в `.takt/learning/ready`. Trusted package/skill configuration автоматически не меняется.

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
