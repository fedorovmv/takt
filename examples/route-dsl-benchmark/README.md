# Реальный benchmark генерации Route DSL

Этот каталог отделён от `route-dsl-eval`: старый набор остаётся инфраструктурным contract suite с fake Pi, а здесь выполняется сравнение реального Pi, реальной модели и штатного валидатора на десяти заданиях.

## Требования

- собранный `bin/takt`;
- установленный и авторизованный Pi;
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

`report.json` фиксирует идентичность стратегии, набора заданий и валидатора, assistant/provider/requested/resolved model, usage, resume, diagnostics и предметные метрики: success@1, итоговую долю корректных результатов, среднюю оценку, число попыток, стоимость и время на корректный результат.

Штатный валидатор должен оценивать результат по требованиям конкретного задания, а не только проверять синтаксис YAML. Версия или содержимое workflow, config, Markdown-команд, набора заданий и валидатора изменяют соответствующие fingerprints.
