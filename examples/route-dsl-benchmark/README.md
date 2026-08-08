# Реальный benchmark генерации Route DSL

Этот каталог отделён от `route-dsl-eval`: старый набор остаётся инфраструктурным contract suite, а здесь хранится agent-neutral сравнительный Route DSL benchmark на 25 заданиях: 10 regression и 15 production-shaped synthetic cases. Live качество требует штатного валидатора и выбранного coding-agent/model adapter.

## Требования

- собранный `bin/takt`;
- установленный и авторизованный coding-agent, указанный как `default_assistant`;
- конфигурация модели Takt;
- штатный Route DSL validator либо wrapper, который печатает один JSON-объект `takt-validation/v1alpha1`.

Пример результата валидатора:

```json
{
  "protocol_version": "takt-validation/v1alpha1",
  "type": "validation_result",
  "valid": true,
  "score": 94,
  "checks": {
    "syntax": {"passed": true, "score": 100, "weight": 1},
    "schema": {"passed": true, "score": 100, "weight": 1},
    "references": {"passed": true, "score": 95, "weight": 2},
    "semantics": {"passed": true, "score": 88, "weight": 4}
  },
  "diagnostics": []
}
```

Запуск:

```bash
make build
TAKT_CONFIG=/path/to/config.yaml \
TAKT_ROUTE_VALIDATOR=/path/to/route-tool \
TAKT_REPEAT=3 \
./examples/route-dsl-benchmark/run.sh
```

`benchmark.json` объединяет strategy reports и comparisons; каждый `report.json` фиксирует идентичность стратегии, набора заданий и валидатора, assistant/provider/requested/resolved model, usage, resume, diagnostics и предметные метрики: success@1, итоговую долю корректных результатов, среднюю оценку, число попыток, стоимость и время на корректный результат.

Штатный валидатор должен оценивать результат по требованиям конкретного задания, а не только проверять синтаксис YAML. Версия или содержимое workflow, config, Markdown-команд, набора заданий и валидатора изменяют соответствующие fingerprints.

## Интерпретация метрик v0.1.45

Отчёт хранит каждую фактическую попытку в `nodes.*.executions`. Токены и стоимость распределяются по `summary.usage_by_execution_identity`; узел с разными assistant, версиями или моделями между retry получает `mixed_execution_identity: true`.

Нулевые измеренные показатели сохраняются как `0`, недоступные средние — как `null`. `average_time_to_valid_ms` измеряется по durable завершению quality-node; `amortized_end_to_end_ms_per_valid` отдельно показывает стоимость времени всего эксперимента. Matrix также считает парные transitions, failed-execution cost, retry/fingerprints и стабильность cases между повторами.

Результат `valid: true` учитывается только когда quality-node завершился со статусом `completed`.

`cases.yaml` фиксирует category/difficulty/source и входит в benchmark fingerprint. Метка `production-shaped` означает синтетическую задачу, приближенную к реальным требованиям, а не обезличенное production ТЗ.
