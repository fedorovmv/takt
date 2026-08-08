# План реализации Takt v0.2

## 1. Принцип выполнения

Каждый этап завершается работающим сквозным сценарием. Новые абстракции добавляются только при наличии минимум двух процессов, которые их используют.

## 2. Этап A. Стабилизация runtime — завершён в v0.1.4-alpha

Реализовано:

- классификация execution errors;
- `allow_failure` только для exit code;
- продолжение DAG после failure и рабочий `all_done`;
- единый scheduler для root DAG и `loop_group`;
- timeout всей попытки, включая hooks;
- thread-safe общий output limit stdout/stderr;
- fingerprints workflow/config/commands;
- безопасный `answer`, lock и `resume`;
- обязательная обработка persistence errors;
- revisions state/event;
- сохранение block scalar;
- единый JSON envelope CLI;
- контрактные тесты отказов;
- запрет nested `loop_group` в `v1alpha1`;
- строгая семантика `until` только для `completed` child node.

Осталось в рамках стабилизации v0.2:

- строгие template variables;
- `takt cancel`;
- stale-lock recovery;
- schema version/attempt/iteration в event.

## 3. Этап B. Adapter protocol contract suite — завершён в v0.1.6-alpha

### Задачи

- добавить fake assistant binary;
- формализовать request/result envelope;
- проверить fresh/resume;
- проверить invalid/malformed result;
- проверить timeout, cancellation, stdout/stderr и output limit;
- добавить единый набор contract tests.

### Критерии

- process adapter проходит весь fake suite;
- failure классифицируется одинаково независимо от конкретного adapter;
- resume rejection не превращается в fresh;
- session ID сохраняется в NodeState.

## 4. Этап C. Первый реальный adapter — Pi реализован в v0.1.8-alpha и стабилизирован в v0.1.9–v0.1.12-alpha

Выбран Pi. Adapter использует официальный RPC mode, contract tests с fake Pi и отдельный opt-in smoke test. OpenCode также реализован через официальный JSON CLI mode и покрыт отдельным contract suite.

### Задачи

- реализовать специализированный adapter;
- добавить capability discovery;
- поддержать fresh/resume;
- нормализовать stdout/stderr/session/error;
- добавить opt-in smoke test с реальным агентом.

### Критерии

- command node выполняется реальным агентом;
- модель узла действительно меняет модель;
- retry `fresh` создаёт новую сессию;
- retry `resume` продолжает предыдущую либо возвращает явную ошибку;
- timeout работает.

## 5. Этап D. Route DSL end-to-end и evaluation — сравнительный benchmark реализован к v0.1.45-alpha

```text
prepare input
→ agent implement
→ fast validator hook
→ retry with feedback
→ full validation node
→ approval
```

### Реализовано

- workflow на `type: pi` с `session: resume`;
- обязательный validator hook и полный validation node;
- diagnostics попадают в `${feedback}` следующей попытки;
- тест требует двух попыток и проверяет продолжение Session ID;
- `route.yaml` и validation report сохраняются как artifacts;
- approval/resume проходит через CLI;
- `takt eval run/report/benchmark/compare` прогоняет corpus, сравнивает стратегии и применяет regression gates;
- отчёт фиксирует strategy/benchmark/workspace/validator fingerprints и per-attempt assistant version, requested model и фактический resolved model;
- quality node возвращает `takt-validation/v1alpha1`, а summary рассчитывает success@1, final success, score и cost/time per valid;
- `NodeState` и evaluation report сохраняют attempts, execution records, duration, usage, resume, feedback, diagnostic output, approvals и errors;
- usage группируется по execution identity; mixed retry не приписывается последней модели;
- нулевые измеренные quality-метрики сериализуются явно, а недоступные значения — как `null`;
- preflight отклоняет коллизии нормализованных `case_id` и пересечение workspace template/output;
- infrastructure fake benchmark и live Route DSL benchmark разделены; corpus содержит 10 regression и 15 production-shaped synthetic cases, live quality требует штатный валидатор.

### Остаётся

- подключить штатный `route-tool` к `examples/route-dsl-benchmark`;
- дополнить versioned synthetic corpus отдельным реальным обезличенным corpus, когда он будет доступен;
- получить baseline и сравнить модели/стратегии на одинаковых fingerprints;
- учитывать manual corrections результата и при необходимости расширить предметные checks.

### Критерии

- хотя бы одно задание требует реального исправления;
- agent изменяет существующий файл;
- итоговый успех определяется валидатором;
- Run воспроизводится с теми же fingerprints.

## 6. Этап E. Пакеты профилей, композиция и governed lifecycle — реализовано в v0.1.21–v0.1.26-alpha

- пакеты профилей и `takt init/validate/run <profile>`;
- именованные workflow, селектор `profile:workflow`, `workflow list/describe`;
- Markdown-профиль `code` без обязательного task AST;
- reusable `subworkflow`;
- последовательный и параллельный `foreach` по inline-списку или внешнему YAML/JSON-массиву;
- fingerprints подключённых определений;
- 19 процессов разработки и умный роутер;
- проверяемый `output_format`, JSON-пути и approval внутри `loop_group`;
- параллельные DAG-волны простых независимых узлов;
- governed child Run, parent/child approval-resume, cancellation tree и worktree isolation;
- ограничения инструментов/MCP/skills/sandbox, динамический fan-out, script/artifacts, local MCP, event protocol v2 и внешний executor реализованы в v0.1.27–v0.1.32-alpha; authoring preflight и локальный daemon — в v0.1.33-alpha.

## 7. Проверка универсальности

### Go workflow

```text
agent fix
→ go test
→ retry
→ optional review
```

### Document workflow

```text
agent draft
→ approval with comment
→ agent revise
→ final approval
```

Оба процесса добавляются без изменения runtime ядра.

## 8. Outputs и iteration history

Реализовано компактное `output_format`, JSON-пути, агрегированный `foreach` output и сохранение активной loop iteration. Остаётся:

- строгий template renderer;
- расширение до полного JSON Schema;
- полная история всех loop iterations, а не только последней;
- публичный агрегированный output `loop_group`;
- capability requirements и per-node tool policy.

## 9. Подготовка v1beta1

- собрать изменения семантики по реальным запускам;
- добавить мигратор `v1alpha1 → v1beta1`;
- зафиксировать JSON Schemas;
- сформировать compatibility matrix adapters;
- отделить production backlog от core runtime.

## 10. Текущий порядок ближайших задач

Завершённые крупные срезы после базовой стабилизации:

1. локальный MCP control plane, external executor, authoring daemon и Dynamic Takt — `v0.1.30–v0.1.34`;
2. trusted blocks, host control, autonomous Run operations и Simple Reliable — `v0.1.35–v0.1.38`;
3. Role/Brief controls и Evidence/baseline/failure routing — `v0.1.39–v0.1.40`;
4. Adapter Platform и Portable Package Distribution — `v0.1.41–v0.1.42`;
5. Multi-repo Dynamic Workflows — `v0.1.43`;
6. Runtime Reliability & Local Security — `v0.1.44`: durable retry/backoff, diagnostic fingerprints, early-exit fan-out, SecretRef/redaction, local OS sandbox для deterministic nodes и NodePath.

Активный порядок:

1. Выполнить зафиксированную v0.1.45 Route DSL matrix со штатным валидатором и доступными моделями; сохранить baseline/candidate evidence и использовать его для решений v0.2;
2. более полный JSON Schema, полная iteration history и проектирование `v1beta1`;
3. только после результатов benchmark — точечные изменения runtime semantics, подтверждённые реальными Run.

## 11. Пока не начинать

- Web UI;
- серверную очередь;
- распределённые workers;
- marketplace;
- GitHub App;
- собственные файловые tools;
- собственный LLM agent loop;
- untrusted/server scope до threat model и sandbox.
