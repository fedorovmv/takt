# Диагностика

## `must define exactly one of ...`

В узле одновременно указаны два действия или не указано ни одного. Оставь ровно одно из `command`, `prompt`, `bash`, `approval`, `loop_group`.

## `references unknown model`

Значение `node.model`, frontmatter или `workflow.model` отсутствует в
`config.models`. Добавь alias в config или исправь имя.

## `does not resolve an assistant`

Provider не задан в узле, Markdown-команде и `workflow.provider`. Добавь
binding из `config.assistants`.

## `command not found`

Проверь имя без `.md` и порядок каталогов:

1. `<workspace>/.takt/commands/`;
2. `commands/` рядом с workflow;
3. `~/.takt/commands/`.

## Retry не происходит

Проверь одновременно:

- `attempts.max` больше 1;
- проверка находится в hook;
- у hook есть `on_failure.action: retry`;
- валидатор действительно возвращает ненулевой exit code;
- попытка не завершилась timeout/cancellation/runtime error.

`hook.on_failure.session` принимает только `fresh` или `resume` и используется
только вместе с `action: retry`.

## Модель не та

Проверь приоритет:

1. model в узле;
2. model во frontmatter команды;
3. `workflow.model`.

В `state.json` и evaluation report смотри `requested_model` и `resolved_model`. Провайдер может маршрутизировать запрос на другую фактическую модель.

## Resume не работает

- для retry используй `attempts.retry_session: reuse` или hook
  `on_failure.session: resume`; `context: shared` — для продолжения совместимого
  upstream session;
- assistant должен поддерживать resume;
- предыдущая попытка должна сохранить Session ID;
- Pi должен открыть тот же Session ID;
- не добавляй fallback на fresh.

## Validator JSON не декодируется

- stdout должен содержать только один JSON envelope;
- предупреждения и логи отправляй в stderr;
- проверь `protocol_version`, `type`, обязательное `valid` и допустимые severity;
- JSON не должен содержать неизвестных полей.

## Workflow валиден, но не запускается

`takt validate` проверяет структуру, но не гарантирует наличие Pi, credentials, model/provider, shell-инструментов и предметных файлов. Проверь внешние зависимости отдельно.

## Полезные команды

```bash
takt validate <workflow> --config <config> --workspace <dir> --json
takt run <workflow> --config <config> --workspace <dir> --input <file> --json
takt status <run-id> --workspace <dir> --json
takt resume <run-id> --workspace <dir> --json
takt answer <run-id> <node-id> --workspace <dir> --value <text> --json
```

## Subworkflow не загружается

Путь вычисляется относительно workflow, в котором указан `subworkflow`. Проверь `path`, рекурсивные ссылки и `output_node`. Если у подключённого workflow несколько terminal-узлов, укажи один из их исходных ID как `output_node`.

## Foreach выполняется не по ожидаемому списку

Текущая версия принимает только `foreach.items`, записанный в YAML. Она не извлекает пункты из Markdown и не читает произвольный JSON-файл автоматически. Для Markdown используй обычный профиль и передавай документ агенту целиком.

## Неизвестное поле или подозрительная ссылка

`takt validate` показывает путь поля и `did you mean` для близкой опечатки. Обязательная `$path` должна разрешиться статически и во время Run. Для осознанно отсутствующего значения используй `$path?`, для fallback — `$path:-default`. В CI запускай `takt validate ... --warnings-as-errors`.

## Daemon не запускается

Проверь `takt daemon status --workspace <dir>` и `.takt/daemon.log`. Один workspace допускает один daemon. Путь `.takt/daemon.sock` должен быть Unix socket и принадлежать текущему пользователю. После аварийного завершения следующий запуск удаляет только неактивный stale socket.
