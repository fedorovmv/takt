# Исправление интеграционного покрытия и evaluation runner в v0.1.12-alpha

## 1. Исправления релизного шлюза

Route DSL end-to-end больше не зависит от команды `python`. JSON-ответы CLI проверяются Go helper-ом `internal/testsupport/routee2eassert`, который собирается тем же Go toolchain, что и Takt.

Интеграционные проверки `timeout + overflow` и `cancel + overflow` теперь используют обычные `context.WithTimeout` и `context.WithCancel`. Fake Pi создаёт реальное переполнение внутри `Pi.Run`, а тестовый hook синхронизирует завершение родительского context с моментом достижения лимита. Проверяются:

- итоговый execution kind;
- `Result.Truncated=true`;
- завершённый `Done()` и ненулевой `Err()` родительского context;
- перенос статуса и `output_truncated` в `NodeState` через обычный runtime scheduler.

## 2. Сохранение usage

Runtime больше не теряет `Result.Usage`. Для каждого узла `NodeState.usage` накапливает input tokens, output tokens и стоимость всех агентных попыток, включая попытки, после которых детерминированный hook запросил retry.

Usage остаётся необязательным: bash, approval и adapters без статистики не создают это поле.

## 3. Evaluation runner

Добавлены команды:

```text
takt eval run <workflow> \
  --config <config> \
  --cases <directory> \
  --workspace-template <directory> \
  --output <directory> \
  [--repeat N] \
  [--answer value] \
  [--replace] \
  [--json]

takt eval report <evaluation-output-directory> [--json]
```

Каждый Markdown-файл из `--cases` выполняется в отдельной копии workspace template. Workflow и штатный валидатор остаются обычными файлами процесса; evaluation runner не подменяет completion gate и не интерпретирует Route DSL.

`report.json` содержит:

- статус и Run ID;
- длительность;
- суммарное число попыток;
- input/output tokens и стоимость;
- число автоматизированных approval answers;
- статусы узлов, Session ID, error code и output truncation.

Workflow failures считаются результатом оценки и не останавливают остальные задания. Ошибки подготовки workspace, загрузки определений и записи отчёта считаются инфраструктурными и останавливают suite.

## 4. Route DSL eval-набор

`examples/route-dsl-eval/cases` содержит десять синтетических заданий для проверки механики. Рабочая оценка должна использовать реальные обезличенные технические задания и штатный `route-tool` либо совместимый wrapper в workspace template.

Контрактный eval-тест запускает два задания через fake Pi, требует retry/resume, автоматически отвечает approval и проверяет агрегированные usage-метрики. Он включён в `make check` и `scripts/verify.sh`.
