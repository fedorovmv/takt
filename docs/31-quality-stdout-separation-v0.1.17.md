# Разделение stdout и stderr quality-node в v0.1.17-alpha

## Причина изменения

До `v0.1.17-alpha` bash runtime объединял stdout и stderr в одно поле `output`. Evaluation декодировал validation envelope из этого объединённого текста. Корректный JSON в stdout становился ошибкой `quality_contract`, если валидатор одновременно писал предупреждение или служебное сообщение в stderr.

## Контракт

Bash-узел сохраняет три представления результата:

- `stdout` — фактический стандартный вывод;
- `stderr` — фактический поток ошибок и предупреждений;
- `output` — объединённое диагностическое представление, совместимое с шаблонами `${nodes.<id>.output}` и feedback.

Quality envelope `takt-validation/v1alpha1` декодируется только из `stdout`. Exit code и terminal status по-прежнему оцениваются отдельно. Stderr не влияет на разбор envelope.

## Пример

Валидатор может вернуть:

```text
stdout: {"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":false,"score":37,...}
stderr: validator cache is cold
exit:   1
```

Evaluation сохраняет score и diagnostics из stdout, статус `failed`, stderr и объединённый diagnostic output. Результат остаётся невалидным и не повышает success rate.

## Совместимость

Поле `output` сохранено. Существующие workflow, hooks, `${feedback}` и `output_contains` продолжают использовать объединённое представление. Новые поля `stdout` и `stderr` добавлены в state и evaluation report без изменения `apiVersion` alpha-контракта.

## Проверки

Регрессии покрывают:

- раздельное сохранение stdout и stderr bash-процесса;
- сохранение объединённого diagnostic output;
- `valid:false` в stdout вместе с произвольным stderr и exit code 1;
- сохранение score и diagnostics без `quality_contract`;
- JSON Schemas state и evaluation report.
