# План реализации Takt v0.2

## 1. Принцип выполнения

Каждый этап должен заканчиваться работающим сквозным сценарием. Новые абстракции добавляются только при наличии минимум двух процессов, которые их используют.

## 2. Этап A. Стабилизация текущего контракта

### Задачи

- добавить schema version в события;
- ввести типизированные ошибки runtime;
- добавить fingerprint workflow и config в RunState;
- удалить временную совместимость `HARNESS_*` после миграционного окна;
- сделать неизвестные шаблонные переменные ошибкой;
- добавить `takt cancel`;
- добавить timeout и output limit для process adapter;
- обновить тесты состояния и resume.

### Критерии

- unit tests покрывают переходы Run и Node;
- отменённый process завершается;
- resume обнаруживает изменение workflow/config;
- старые примеры проходят без ручных изменений либо имеют миграционную заметку.

## 3. Этап B. Первый реальный adapter

Рекомендуемый первый вариант: Pi, если он используется в основном сценарии. OpenCode выбирается первым, когда его API стабильнее для нужной среды.

### Задачи

- реализовать специализированный adapter;
- добавить capability discovery;
- поддержать fresh/resume;
- нормализовать stdout/stderr/session/error;
- добавить fake-binary integration suite;
- добавить opt-in smoke test с реальным агентом.

### Критерии

- один command node выполняется реальным агентом;
- модель узла действительно меняет используемую модель;
- retry `fresh` создаёт новую сессию;
- retry `resume` продолжает предыдущую либо явно сообщает о невозможности;
- timeout и cancel работают.

## 4. Этап C. Route DSL как основной сквозной тест

### Workflow

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
- подключить существующий route-tool;
- нормализовать diagnostics в feedback;
- сохранить route.yaml и validation report как artifacts;
- добавить eval-набор минимум из 10 заданий;
- собирать iterations, tokens, duration, validation errors и manual corrections.

### Критерии

- хотя бы одно задание требует реального исправления после ошибки;
- agent изменяет существующий файл, а не только печатает YAML;
- итоговый успех определяется валидатором;
- Run воспроизводится с теми же workflow/config fingerprints.

## 5. Этап D. Проверка универсальности

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

### Критерии

- оба процесса добавляются без изменения runtime ядра;
- общие новые требования оформляются отдельным ADR;
- предметные инструменты остаются командами, hooks или внешними исполнителями.

## 6. Этап E. Уточнение loops и outputs

### Задачи

- изолировать состояния дочерних узлов loop group;
- добавить агрегированный output loop node;
- формализовать current/previous iteration;
- добавить structured output с JSON Schema;
- ввести `loop_exhausted` как типизированную ошибку.

### Критерии

- повторяющиеся child ID в разных loop groups не конфликтуют;
- внешние узлы читают только публичный output loop node;
- malformed structured output приводит к контролируемой ошибке.

## 7. Этап F. Подготовка v1beta1

- собрать изменения семантики по реальным запускам;
- разделить спецификацию на стабильные документы;
- добавить мигратор `v1alpha1 → v1beta1`;
- зафиксировать JSON Schemas;
- сформировать compatibility matrix для adapters;
- определить production backlog отдельно от core runtime.

## 8. Рекомендуемый порядок первых задач

1. `TAKT_*`, timeout, cancel, typed errors.
2. Pi/OpenCode adapter contract tests.
3. Реальный adapter.
4. Route DSL end-to-end.
5. Go workflow.
6. Document workflow.
7. Loop isolation и structured outputs.
8. v1beta1 design.

## 9. Задачи, которые пока не следует начинать

- Web UI;
- серверная очередь;
- распределённые workers;
- marketplace;
- GitHub App;
- собственные файловые tools;
- собственный LLM agent loop;
- универсальный plugin ABI до появления реальных расширений.
