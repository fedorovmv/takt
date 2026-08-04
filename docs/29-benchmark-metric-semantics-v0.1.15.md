# Семантика benchmark-метрик и execution identity в v0.1.15-alpha

## Цель

`v0.1.14-alpha` добавила идентичность стратегии, benchmark и предметный результат качества. Аудит выявил три смысловых риска: измеренный ноль исчезал из JSON, usage нескольких retry приписывался последней модели, а `valid: true` мог учитываться из неуспешного quality-node. `v0.1.15-alpha` устраняет эти неоднозначности.

## Нулевые и недоступные показатели

Поля summary больше не используют исчезновение ключа как неявное значение.

- измеренный нулевой результат сериализуется числом `0`;
- показатель, который нельзя вычислить, сериализуется как `null`;
- счётчики quality, valid, invalid и scored всегда присутствуют;
- пустые распределения сериализуются пустыми объектами.

Пример стратегии без успешных результатов:

```json
{
  "quality_runs": 10,
  "valid": 0,
  "success_at_1": 0,
  "final_success_rate": 0,
  "average_attempts_to_valid": null,
  "average_score": null,
  "cost_per_valid": null,
  "amortized_end_to_end_ms_per_valid": null
}
```

Такой отчёт отличается от запуска без quality-node, где `quality_runs` равен нулю, а доли успеха имеют значение `null`.

## Execution records

`NodeState` сохраняет агрегированные поля узла для совместимости, а также массив `executions`. Одна запись соответствует одному фактическому вызову действия узла и содержит:

- номер попытки;
- результат выполнения;
- assistant и его версию;
- запрошенную и фактически использованную модель;
- Session ID и признак resume;
- exit/error classification;
- output truncation;
- usage этой попытки.

Retry больше не уничтожает идентичность предыдущего выполнения. Агрегированное `node.usage` остаётся суммой всех попыток, но распределение затрат строится по `executions`.

## Mixed execution identity

Execution identity определяется сочетанием:

- assistant;
- версии assistant;
- requested model;
- resolved model.

Если эти значения отличаются между попытками одного узла, report выставляет:

```json
{
  "mixed_execution_identity": true
}
```

Summary увеличивает `mixed_execution_identity_nodes` и группирует usage в `usage_by_execution_identity`. Токены и стоимость разных моделей больше не обозначаются как usage последней попытки.

## Quality-node

Результат `takt-validation/v1alpha1` принимается только от узла со статусом `completed`.

Следующие состояния не могут повысить success rate, даже если stdout содержит `valid: true`:

- `failed`;
- `errored`;
- `timed_out`;
- `cancelled`;
- `skipped`;
- `blocked`.

Для них report сохраняет `quality_error`, а запуск учитывается как невалидный предметный результат. Нарушение JSON-контракта успешно завершившимся quality-node остаётся ошибкой измерительного контура.

## Fingerprint валидатора

Общий `benchmark.fingerprint` включает:

- ID валидатора;
- объявленную версию;
- fingerprint файла или каталога;
- quality/generation node;
- версию validation protocol;
- dataset и workspace fingerprints.

Изменение только `--validator-version` или `--validator-id` теперь создаёт другую идентичность benchmark.

## Длительность

Поле `duration_per_valid_ms` удалено из текущего отчёта. Оно делило суммарную длительность всех Run на число валидных результатов и не являлось фактическим time-to-valid.

Новое имя:

```text
amortized_end_to_end_ms_per_valid
```

Показатель отражает амортизированную end-to-end стоимость времени всего benchmark, включая неуспешные задания, approval и узлы после проверки. Настоящий time-to-valid потребует отдельных временных отметок завершения quality-node и остаётся будущим расширением.

## Проверки

Регрессии покрывают:

- явный `0` и `null` в сериализованном report;
- смену resolved model и версии assistant между retry;
- раздельную атрибуцию usage;
- mixed marker;
- `valid: true` из failed quality-node;
- изменение benchmark fingerprint при смене ID или версии валидатора;
- наличие `ResolvedModel` в opt-in Pi smoke.
