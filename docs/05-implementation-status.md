# Состояние реализации

Статус: `v0.1.6-alpha`.

## Реализовано

- YAML/JSON loader со строгой проверкой неизвестных полей;
- документированный YAML subset с корректными `|`, `|-`, `|+`, `>`, `>-`, `>+` и пустыми строками;
- статическая проверка ID, ссылок, timeout и циклов DAG;
- модельный каталог;
- `mock` и `process` assistants;
- реализованный JSON-протокол `takt-assistant/v1alpha1` для process assistant;
- fake assistant binary и contract suite для success/exit/start/timeout/cancel/concurrent output/malformed/fresh/resume;
- Markdown commands;
- узлы `command`, `prompt`, `bash`, `approval`, `loop_group`;
- `when` и `trigger_rule` в корневом и дочернем DAG;
- продолжение DAG после failed/errored node для выполнения `all_done`;
- разделение `failed`, `errored`, `timed_out`, `cancelled`, `blocked`;
- `allow_failure` только для штатного ненулевого exit code;
- hooks и retry с feedback;
- timeout всей попытки узла, включая portable hooks;
- сохранение `timed_out`/`cancelled` на родительском `loop_group`;
- общий thread-safe output limit stdout/stderr process assistant;
- завершение process group при cancellation на Unix;
- pause/resume approval;
- fingerprints workflow, config и разрешённых Markdown-команд;
- блокировка Run для `answer` и `resume`;
- ревизии `state.json` и `events.jsonl` с обнаружением рассогласования;
- обязательная обработка ошибок persistence;
- единый JSON envelope CLI;
- CLI `validate`, `run`, `answer`, `resume`, `status`, `command run`;
- JSON Schemas текущего `v1alpha1`;
- unit-, race-, vet-, build- и сквозные проверки.

## Осознанно упрощено

- последовательное выполнение готовых узлов DAG;
- локальное файловое состояние;
- ограниченный язык выражений;
- собственный документированный YAML subset вместо полной YAML 1.2;
- approval и вложенный `loop_group` внутри `loop_group` запрещены;
- `until` требует статус дочернего узла `completed`;
- отсутствуют server, Web UI, MCP и worktree orchestration.

## Граница безопасности

Текущая версия рассчитана на локального доверенного пользователя. До server/untrusted scope нужны:

- sandbox процессов;
- политика допустимых путей;
- ограничения файловой системы и сети;
- управление секретами и redaction;
- более сильная межпроцессная блокировка и recovery stale lock;
- аутентификация и авторизация.

## Основные незакрытые задачи v0.2

- специализированный Pi или OpenCode adapter;
- capability discovery специализированного adapter;
- opt-in smoke tests с реальным агентом;
- строгий template renderer;
- команда `takt cancel`;
- capability negotiation;
- structured outputs;
- Route DSL end-to-end на реальной модели;
- eval-набор для Route DSL, Go и документов.

## Следующий практический этап

Fake assistant contract suite завершён. Следующий этап — специализированный Pi либо OpenCode adapter, который обязан пройти тот же набор контрактных тестов, затем Route DSL workflow переводится с `mock` на реальный adapter.
