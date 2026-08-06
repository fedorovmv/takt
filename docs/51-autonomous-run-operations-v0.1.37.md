# Autonomous Run Operations — v0.1.37-alpha

## Цель

Срез превращает локальный daemon из технического способа удерживать Run в пользовательский контур автономного исполнения:

```text
запустить
→ закрыть клиент
→ увидеть активные и требующие внимания процессы
→ безопасно приостановить или продолжить
→ повторить неуспешный участок
→ получить уведомление
→ открыть агрегированный результат
```

Веб-интерфейс и БД не вводятся. Источником истины остаётся файловый Store в workspace.

## Реестр запусков

CLI:

```bash
takt runs --active --workspace . --daemon
takt attention --workspace . --daemon
takt run list --status failed --root-only --workspace . --daemon
takt run summary <run-id> --workspace . --daemon
takt run watch <run-id> --workspace . --daemon
```

`run list` возвращает workflow, фактический и вычисленный статус, текущие узлы, usage, число артефактов и детей, ошибку и причину внимания. В список можно включать root и child Run; `--root-only` оставляет только пользовательские корни.

При detached start каталог Run может появиться на несколько миллисекунд раньше атомарной публикации `state.json`. Registry и notification dispatcher пропускают только такой временный `ENOENT`; повреждённое опубликованное состояние остаётся ошибкой.

## Attention queue

`takt attention` объединяет состояния, где требуется действие человека:

- approval или вопрос;
- tool approval;
- failed Run;
- paused Run;
- Dynamic Plan, ожидающий ответа вне активного segment Run.

Каждая запись содержит reason, message, node и команду/ID для открытия состояния. Pi/OpenCode host extensions показывают непрочитанные уведомления и предоставляют `/takt-attention`.

## Безопасная пауза

```bash
takt run pause <run-id> --daemon
takt run resume <run-id> --daemon
```

Pause является безопасной границей, а не остановкой посередине provider/tool call:

1. новые узлы и новые партии fan-out не запускаются;
2. текущие попытки доходят до границы узла;
3. дочерние Run получают pause marker;
4. root и Dynamic Plan переходят в `paused`;
5. resume продолжает незавершённый граф и переиспользует завершённые child Run.

Если child ID уже связан с родителем, но начальный `state.json` ещё не опубликован, pause/abandon marker создаётся заранее. Дочерний Run применяет его при старте и не успевает обойти операторское решение.

Approval/waiting Run переводится в `paused` синхронно с сохранением исходного `waiting` состояния; resume возвращает его к тому же ожиданию.

## Retry, fork и abandon

```bash
takt run retry <run-id> [--node validate] --daemon
takt run fork <run-id> [--input "новая цель"] --daemon
takt run abandon <run-id> --reason "кампания остановлена" --daemon
```

- `retry` сбрасывает выбранный failed-узел и зависимый от него хвост, сохраняет историю `operator_retries` и не повторяет независимые завершённые узлы;
- `fork` создаёт новый Run с тем же workflow либо новый draft Dynamic Plan с изменённой целью;
- `abandon` — отдельное терминальное состояние, отличное от `cancelled`; история сохраняется, процесс больше не обслуживается.

Если worktree исходного Run уже удалён, retry in-place отклоняется и требуется fork.

## Восстановление после daemon restart

Daemon при старте вызывает recovery:

```text
running/pausing Run
→ executor PID больше не существует
→ текущая attempt помечается worker_lost
→ consumed attempt возвращается
→ node снова pending
→ recovery_count увеличивается
→ scheduler продолжает Run
```

Дети восстанавливаются раньше родителей. После их завершения Takt продолжает parent chain.

Это локальная PID-based recovery. Она не доказывает, что внешний provider-процесс также завершён: после аварии daemon возможно повторное выполнение недетерминированного внешнего действия. Идемпотентность внешних операций должна обеспечиваться adapter или самим workflow.

Команда ручного запуска:

```bash
takt run recover --workspace . --daemon
```

## Уведомления

Конфигурация `.takt/notifications.yaml`:

```yaml
apiVersion: takt/v1alpha1
kind: NotificationConfig

events:
  - approval.required
  - question.required
  - tool_approval.required
  - run.completed
  - run.failed
  - run.paused
  - run.abandoned
  - worker.lost

sinks:
  - type: coding_agent_host
  - type: desktop
  - type: process
    command: corp-notify-adapter
    args: [--stdin-json]
```

Поддержаны sinks:

- `coding_agent_host` — durable inbox;
- `desktop` — `osascript` на macOS или `notify-send` на Linux;
- `process` — доверенная локальная программа получает JSON в stdin без shell-интерполяции.

CLI:

```bash
takt notify list --unread --workspace .
takt notify ack <notice-id> --workspace .
takt notify test --message "Takt ready" --workspace .
takt notify dispatch --workspace .
```

Daemon вызывает dispatcher периодически. Snapshot состояния обеспечивает дедупликацию переходов; inbox хранится в `.takt/notifications/inbox/` и требует явного ack.

## Итоговая сводка

`run summary` агрегирует root и, по умолчанию, все descendant Run:

- длительность;
- число completed/failed/waiting узлов;
- direct и recursive child Run;
- tokens/cost;
- артефакты с provenance;
- output и terminal error;
- recovery и operator retry counts;
- текущую причину внимания.

Та же проекция используется командами `/takt-result` в host extensions и может передаваться в корпоративный SCM/tracker adapter.

## MCP и daemon API

Добавлены MCP tools:

- `takt.run.list`;
- `takt.run.attention`;
- `takt.run.summary`;
- `takt.run.pause`;
- `takt.run.resume_paused`;
- `takt.run.retry`;
- `takt.run.fork`;
- `takt.run.abandon`;
- `takt.run.recover`;
- `takt.notify.list`;
- `takt.notify.ack`;
- `takt.notify.test`.

Всего локальный MCP содержит 48 tools.

## Команды кодинг-агента

Pi:

```text
/takt-runs
/takt-attention
/takt-pause
/takt-resume
/takt-result
```

OpenCode поставляет соответствующие command files. Эти команды используют host session либо явный run-id, не передавая управление основной LLM.

## Исправления Coding Agent Host Control

- terminal host session не переиспользуется новым `/takt`;
- transport failure не сбрасывает managed cache и не открывает инструменты;
- Pi и OpenCode bundled integrations заявляют `guarded`, а не неподтверждённый `strict`;
- Pi не использует неработающий `before_agent_start`;
- steering failure остаётся перехваченным и не попадает основной LLM;
- OpenCode перекрывает пользовательский shell;
- positional text отделён `--` от флагов;
- точные зависимости и TypeScript contract заменяют плавающий `next`;
- host MCP покрыт контрактными тестами и confirm работает detached;
- повреждённые host-session records не пропускаются;
- begin сериализуется межпроцессной блокировкой.

## Границы

- pause не прерывает уже выполняющийся provider/tool call;
- recovery локальная и PID-based, без распределённого lease coordinator;
- desktop sink зависит от утилит ОС;
- process sink считается доверенной конфигурацией пользователя;
- уведомления не являются гарантированной внешней доставкой: durable inbox остаётся источником истины;
- Web UI, удалённые workers и многопользовательская авторизация не входят в срез.
