# Усиление Pi adapter в v0.1.10-alpha

## Причина изменения

Аудит `v0.1.9-alpha` выявил два пограничных случая:

1. одновременный timeout/cancellation и переполнение общего лимита stdout/stderr классифицировались как `protocol`, потому что truncation проверялся раньше parent context;
2. исчезновение cumulative usage во втором `get_session_stats` принималось как отсутствие статистики, даже если первый снимок содержал валидные ненулевые значения.

## Реализованная семантика

### Приоритет context

Финальная классификация Pi attempt выполняется в следующем порядке:

1. deadline родительского context → `timed_out`;
2. внешняя отмена → `cancelled`;
3. output overflow без завершённого parent context → `protocol`;
4. прочие ошибки RPC/процесса.

При совпадении причин `Result.Truncated` сохраняется, но не меняет основной execution kind. Это предотвращает ошибочный запуск retry/on_failure для отменённой или просроченной попытки.

### Последовательность usage snapshots

`get_session_stats` остаётся накопительным источником. Допустимы:

- usage отсутствует в обоих снимках — `Result.Usage` не заполняется;
- usage появляется после prompt — первый снимок считается нулевым;
- usage присутствует в обоих снимках и не уменьшается — возвращается дельта;
- явные нулевые значения — валидный usage с нулевой дельтой.

Если usage присутствовал до prompt и исчез после `agent_settled`, результат считается malformed snapshot и классифицируется как `protocol`.

## Регрессии

Добавлены проверки:

- timeout + output overflow → `timed_out`;
- cancel + output overflow → `cancelled`;
- исчезновение cumulative usage → `protocol`;
- явный нулевой cumulative usage → успешный нулевой `Result.Usage`.

