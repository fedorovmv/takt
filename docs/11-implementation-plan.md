# План реализации Takt v0.2

## 1. Принцип выполнения

Каждый этап завершается работающим сквозным сценарием. Новые абстракции добавляются только при наличии минимум двух процессов, которые их используют.

## 2. Этап A. Стабилизация runtime — завершён в v0.1.3-alpha

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

## 3. Этап B. Adapter protocol contract suite

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

## 4. Этап C. Первый реальный adapter

Рекомендуемый первый вариант: Pi, если он используется в основном сценарии. OpenCode выбирается первым, если его API стабильнее для нужной среды.

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

## 5. Этап D. Route DSL end-to-end

```text
prepare input
→ agent implement
→ fast validator hook
→ retry with feedback
→ full validation node
→ approval
```

### Задачи

- заменить mock на реальный adapter;
- подключить route-tool;
- нормализовать diagnostics;
- сохранить route.yaml и validation report как artifacts;
- добавить eval-набор минимум из 10 заданий;
- собирать iterations, tokens, duration, validation errors и manual corrections.

### Критерии

- хотя бы одно задание требует реального исправления;
- agent изменяет существующий файл;
- итоговый успех определяется валидатором;
- Run воспроизводится с теми же fingerprints.

## 6. Этап E. Проверка универсальности

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

## 7. Этап F. Outputs и iteration history

- строгий template renderer;
- structured output с JSON Schema;
- полная история loop iterations;
- публичный агрегированный output loop node;
- capability requirements.

## 8. Этап G. Подготовка v1beta1

- собрать изменения семантики по реальным запускам;
- добавить мигратор `v1alpha1 → v1beta1`;
- зафиксировать JSON Schemas;
- сформировать compatibility matrix adapters;
- отделить production backlog от core runtime.

## 9. Текущий порядок ближайших задач

1. Fake assistant protocol suite.
2. Specialized Pi/OpenCode adapter.
3. Session resume contract.
4. Route DSL end-to-end.
5. Go workflow.
6. Document workflow.
7. Strict templates и structured outputs.
8. v1beta1 design.

## 10. Пока не начинать

- parallel DAG;
- Web UI;
- серверную очередь;
- распределённые workers;
- marketplace;
- GitHub App;
- собственные файловые tools;
- собственный LLM agent loop;
- untrusted/server scope до threat model и sandbox.
