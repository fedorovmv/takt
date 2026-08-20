# Семантика validation envelope в v0.1.16-alpha

## Причина изменения

В `v0.1.15-alpha` quality-node проверялся по status до декодирования stdout. Валидатор мог вернуть корректный `valid: false`, score и diagnostics, затем завершиться с кодом 1. Такой результат полностью терялся из benchmark report.

## Новый контракт

Takt разделяет два результата:

- terminal status и exit code описывают выполнение процесса;
- `takt-validation/v1alpha1` описывает предметное качество результата.

Доступный validation envelope декодируется независимо от exit code и terminal status. В report сохраняются:

- `quality_node_status`;
- полный `quality`;
- `score`;
- `checks`;
- diagnostics;
- `quality_error`, если узел не завершился успешно.

## Success gate

Benchmark считает результат корректным только при одновременном выполнении условий:

```text
quality_node_status == completed
quality.valid == true
```

Примеры:

| Status | Envelope | Итог |
|---|---|---|
| `completed` | `valid: true` | успех |
| `completed` | `valid: false` | невалидный результат, score и diagnostics сохраняются |
| `failed`, exit 1 | `valid: false` | невалидный результат, score и diagnostics сохраняются |
| `failed`, exit 7 | `valid: true` | неуспешное выполнение; envelope сохраняется, success rate не растёт |
| любой | malformed envelope | ошибка измерительного контура |
| failed-like | envelope отсутствует | невалидный результат с `quality_error` |

## Агрегация

`average_score` и диагностические распределения используют любой корректно декодированный envelope, включая `valid: false` из процесса с ненулевым exit code. `success_at_1`, `final_success_rate`, attempts-to-valid и cost-per-valid используют только success gate `completed && valid=true`.

## Проверки

Регрессии покрывают:

- `valid: false + exit 1` с сохранением score и diagnostics;
- `valid: true + exit 7` без роста success rate;
- malformed envelope + exit 1 как `quality_contract`;
- успешный `completed + valid: true`;
- сериализацию `quality_node_status` в evaluation report.
