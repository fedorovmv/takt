# Строгий authoring и локальный daemon — v0.1.33-alpha

## Цель среза

Срез закрывает два пробела локального runtime:

1. ошибки определения workflow должны обнаруживаться до создания Run и расхода модели;
2. локальный Run должен продолжаться после закрытия CLI/MCP-клиента, пока жив отдельный процесс Takt.

Реализация сохраняет файловый Store источником истины. БД, сетевой listener, Web UI и многопользовательская авторизация не вводятся.

## Authoring preflight

`takt validate` выполняет последовательно:

1. строгую загрузку YAML/JSON;
2. проверку структуры workflow;
3. разрешение command/model/assistant references;
4. рекурсивную capability-проверку встроенных и governed child workflow;
5. статический анализ шаблонов, output/artifact references и несовместимых параметров.

### Диагностика полей

Неизвестное поле остаётся ошибкой. Для близкой опечатки сообщение содержит путь и `did you mean`, например `idle_timout` → `idle_timeout`. Это действует и для вложенных объектов.

Семантические подозрения возвращаются как diagnostics. `--warnings-as-errors` переводит их в ошибки для CI.

### Ссылки и renderer

Поддерживаются три формы:

```text
${path}           обязательное значение;
${path?}          optional, пустая строка при отсутствии;
${path:-default}  default при отсутствии или пустом значении.
```

Неразрешённая обязательная переменная завершает узел ошибкой. Authoring preflight проверяет существование upstream-узла, dependency direction, поля `output_format`, status/exit code, approval, artifact references и локальные области loop/fan-out.

### Capability preflight

`takt validate` проверяет effective policy каждого локального AI-узла через фактический adapter. Проверка рекурсивно проходит `loop_group` и governed child workflow с inherited policy. Для `executor: external` окончательная возможность worker по-прежнему подтверждается при claim, потому что worker выбирается после запуска.

### Расширенный schema subset

`output_format` и входные схемы дополнительно поддерживают:

- `description`;
- `minItems`/`maxItems`;
- `minLength`/`maxLength` и `pattern`;
- `minimum`/`maximum`;
- `minProperties`/`maxProperties`.

Ограничения проверяются и при validate, и при фактическом результате.

## Новая runtime-семантика

### always_run

```yaml
- id: cleanup
  depends_on: [build, test]
  always_run: true
  bash: ./cleanup.sh
```

Узел ждёт terminal-состояния всех зависимостей и выполняется независимо от их результата. Он эквивалентен строгому cleanup/finally-пути, но не превращает failed Run в successful. `when` и другой `trigger_rule` вместе с `always_run` отклоняются как неоднозначные.

### idle_timeout

```yaml
- id: implement
  command: implement-change
  idle_timeout: 2m
  timeout: 30m
```

`timeout` ограничивает всю попытку. `idle_timeout` сбрасывается нормализованными событиями assistant и завершает попытку как `timed_out`, если adapter не показывает активность.

Для claimed `executor: external` таймер обслуживает `takt daemon`. Ожидание blocking tool approval приостанавливает idle expiry. При истечении таймера незавершённые tool calls закрываются как cancelled, узел получает `timed_out`, после чего применяются обычные retry/hooks/parent-resume semantics.

## Локальный daemon

### Управление

```bash
takt daemon start  --workspace .
takt daemon status --workspace .
takt daemon stop   --workspace .

# foreground/debug
takt daemon serve --workspace .
```

Daemon обычно использует Unix socket `.takt/daemon.sock`. Если абсолютный путь превышает безопасный Unix `sun_path`, socket переносится в детерминированный `$TMPDIR/takt-daemon/<workspace-hash>/daemon.sock`, а metadata `.takt/daemon.json`, lock `.takt/daemon.lock` и log `.takt/daemon.log`. Socket и metadata доступны только текущему пользователю. Один workspace допускает один daemon.

### Фоновые Run

```bash
takt run workflow.yaml --workspace . --daemon --json
```

Команда возвращает durable `run_id`, а исполнение продолжается в daemon после завершения клиента. Состояние, events, approvals, children и artifacts остаются в обычном `.takt/runs`.

### Events и MCP

```bash
takt events <run-id> --daemon --follow
takt mcp --daemon --workspace .
```

Event subscription передаёт NDJSON по revision cursor до terminal-состояния Run и перед закрытием дочитывает журнал до revision terminal-state, включая `run.completed`. MCP stdio-прокси допускает несколько concurrent JSON-RPC запросов; ответы могут приходить по готовности, а не в порядке запросов, что разрешено JSON-RPC, и вызывает тот же daemon control service; отдельная MCP-модель состояния не появляется.

### Несколько клиентов

CLI, несколько coding-agent hosts и external workers используют один Unix socket. Короткие изменения одного Run сериализуются bounded retry вокруг существующего файлового lock. Store API остаётся неблокирующим и пригодным для обнаружения ошибочного владения lock.

## Границы

- daemon локальный и Unix-only;
- нет TCP/HTTP listener вне Unix socket;
- нет БД и distributed locks;
- daemon переживает закрытие клиента, но не обещает продолжить работающий OS-процесс после падения или перезапуска самого daemon;
- после перезапуска доступны durable waiting/pending Runs, approvals, external tasks и leases; локальный процесс, оборванный завершением daemon, требует явного recovery/resume;
- trust model остаётся однопользовательским: владелец socket имеет полномочия текущего пользователя и доступ к workspace.

## Проверки

Срез покрыт:

- unit/race тестами strict renderer, authoring, capability validate, daemon и control locking;
- повторными package-прогонами daemon/control;
- `scripts/test-authoring.sh`;
- `scripts/test-daemon.sh`;
- полным историческим `make check` и `scripts/verify.sh`;
- чистой распаковкой релизного архива и `MANIFEST.sha256`.
