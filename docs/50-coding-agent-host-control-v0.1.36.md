# Coding Agent Host Control — v0.1.36-alpha

## Задача

Skill и MCP сами по себе не заставляют основную LLM вызвать Takt. Срез вводит host-control API: хост кодинг-агента может перехватить `/takt` до основной модели, создать durable managed session, проверять инструменты и восстановить связь с активным Dynamic Plan после перезапуска.

```text
/takt <задача>
→ host intercept
→ host.begin / preview
→ подтверждение
→ host.confirm / Takt execute
→ worker Run под управлением Takt
```

## Уровни контроля

- `advisory` — хост только показывает состояние;
- `guarded` — хост применяет доступные ограничители, но не заявляет полную гарантию;
- `strict` — Takt принимает режим только при одновременной декларации `command_interception`, `input_interception`, `tool_call_blocking`, `completion_blocking` и `session_recovery`.

`strict` является контрактом доверенного host adapter. Само заявление capabilities не доказывает фактическое поведение стороннего хоста. Для выпуска строгой интеграции нужен live contract test на зафиксированной версии Pi/OpenCode.

## Durable host session

Связь хоста и плана хранится в `.takt/host-sessions/<id>.json` с правами `0600`:

- host и стабильный ID сессии;
- `plan_id`;
- статус `preview|managed|waiting|paused|completed|failed|released`;
- уровень контроля и capabilities;
- время создания и обновления.

`host.find` возвращает только активные сессии. Завершённая или ошибочная сессия не переиспользуется для следующего `/takt`; новый запрос получает новый plan и host-session record. Повреждённая запись является ошибкой, а не молча пропускается. Запись выполняется атомарно с `fsync` каталога.

## API

CLI:

```bash
takt host begin "<задача>" --host pi --host-session <id> \
  --enforcement guarded --command-interception --input-interception \
  --tool-call-blocking --session-recovery --daemon

takt host confirm <session-id> --confirm --daemon
takt host status <session-id> --daemon
takt host find --host pi --host-session <id> --daemon
takt host guard-tool <session-id> --tool edit --daemon
takt host guard-completion <session-id> --kind final --daemon
takt host release <session-id> --daemon
```

MCP публикует `takt.host.begin|confirm|get|find|guard_tool|guard_completion|release`. `takt.host.confirm` через daemon запускает workflow отсоединённо и не удерживает MCP-вызов на время всего процесса.

## Tool guard

Пока host session активна:

- разрешены только точные известные имена read-only инструментов и операций управления Takt;
- `edit`, `write`, shell, Git и неизвестные инструменты блокируются;
- `read_only` из запроса клиента не влияет на решение;
- префикс `takt.` или `takt_` сам по себе не даёт разрешение.

Серверный guard работает fail-closed при неизвестном `session_id`.

## Встроенная интеграция Pi

`integrations/coding-agent-host-control/pi/` зафиксирована на контракте Pi `0.73.1` и заявляет только `guarded`:

- нативная команда `/takt`;
- перехват последующего `input`;
- блокирующий `tool_call` и `user_bash`;
- локальный кеш последнего managed-решения;
- при потере daemon кеш не сбрасывается: ввод и изменяющие инструменты остаются заблокированными;
- steering передаётся после `--`, поэтому текст пользователя не разбирается как флаги Takt.

Pi `0.73.1` не предоставляет подтверждённого fail-closed completion hook. `before_agent_start` не используется и capability `completion_blocking` не заявляется. Поэтому bundled Pi extension не является strict-интеграцией.

## Встроенная интеграция OpenCode

`integrations/coding-agent-host-control/opencode/` также заявляет только `guarded`:

- command marker обрабатывается в `context` hook;
- `tool.execute.before` проверяет инструменты;
- `shell.hook("create.before")` блокирует пользовательский shell;
- транспортные ошибки сохраняют managed cache и блокируют свободное продолжение;
- package manifest не использует плавающий `next` и помечен `verified: false`.

Надёжное прекращение model dispatch через ошибку `context` hook не подтверждено live smoke на целевой версии OpenCode V2. До такого теста bundled OpenCode plugin нельзя считать strict.

## Что гарантирует Go-ядро

- `strict` отклоняется без полного набора пяти capabilities и при begin, и при reuse;
- Begin сериализуется межпроцессной блокировкой;
- tool guard — default deny;
- terminal host sessions не переиспользуются;
- host-state восстанавливается после daemon restart;
- completion guard возвращает решения `allow|deny` для `final|question|status`.

## Исправления аудита

- package fingerprint включает транзитивные Markdown-команды, subworkflow, scripts/dependencies, path skills и MCP-конфигурации;
- macOS-тест использует канонический `EvalSymlinks`;
- явный `allowed_integrations: []` отличается от отсутствующего поля;
- steering использует единый предел редакций;
- пользовательский текст передаётся после `--`;
- transport failure bundled extensions обрабатывается fail-closed относительно сохранённого managed state;
- TypeScript проверяется в strict-режиме против локальных contract `.d.ts`; отсутствие компилятора не ломает обычный Go build, а release verification может потребовать его через `TAKT_REQUIRE_TYPESCRIPT=1`.

## Границы

- Фактический live smoke Pi/OpenCode в release-среде не выполняется.
- Bundled adapters имеют уровень `guarded`, а не `strict`.
- Host guard контролирует наблюдаемые действия, но не внутреннее рассуждение LLM.
- `host release` освобождает интерфейсную сессию и не отменяет активный Run.
