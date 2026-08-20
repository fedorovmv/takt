# Согласование Pi RPC adapter в v0.1.9-alpha

## Причина изменения

Аудит `v0.1.8-alpha` выявил четыре расхождения с официальной RPC-семантикой Pi 0.83.0:

1. попытка завершалась на первом `agent_end`, хотя Pi может продолжить automatic retry, compaction retry или queued continuation;
2. пользовательские `args` могли переопределить часть session/mode-флагов;
3. fire-and-forget `set_editor_text` ошибочно считался интерактивным запросом;
4. `get_session_stats` использовался как статистика одной попытки, хотя он возвращает накопленные значения сессии.

## Реализованная семантика

### Финальная граница выполнения

Takt ждёт `agent_settled`. Все предшествующие `agent_end` сохраняются как low-level runs и не завершают попытку. Fake Pi моделирует последовательность:

```text
agent_end(willRetry=true)
auto_retry_start
auto_retry_end
agent_end(willRetry=false)
agent_settled
```

Контрактный тест проверяет, что Takt получает только финальный результат и не возвращается на первой ошибке.

### Статистика

Перед prompt и после `agent_settled` выполняется `get_session_stats`. `Result.Usage` содержит:

```text
stats_after - stats_before
```

Это относится к input tokens, output tokens и cost. Уменьшение накопленных значений считается protocol error. Полные снимки сохраняются в structured result:

```json
{
  "usage_semantics": "attempt_delta",
  "stats_before": {},
  "stats_after": {},
  "low_level_runs": 2,
  "automatic_retries": 1
}
```

### Зарезервированные флаги

Пользовательские `args` не могут переопределить:

- mode, provider, model и thinking;
- session, session-id и session-dir;
- no-session, continue, resume и fork;
- print, version и help;
- project trust.

Проверяются длинные и короткие aliases, а также форма `--flag=value`.

### Extension UI

Методы, требующие ответа (`confirm`, `select`, `input`, `editor`), остаются protocol error. Fire-and-forget методы допускаются:

- `notify`;
- `setStatus`;
- `setWidget`;
- `setTitle`;
- `set_editor_text`.

## Проверки

Добавлены регрессии:

- automatic retry до `agent_settled`;
- отсутствие частичного результата после первого `agent_end`;
- счётчики low-level runs и retries;
- полный deny-list зарезервированных флагов;
- `set_editor_text`;
- per-attempt usage delta для fresh и resume;
- protocol error при уменьшении cumulative stats.

Реальный Pi smoke test остаётся opt-in и не запускался в сборочной среде без установленного CLI, credentials и доступной модели.
