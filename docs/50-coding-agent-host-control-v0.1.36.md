# Coding Agent Host Control — v0.1.36-alpha

## Задача

Skill и MCP сами по себе не заставляют основную LLM вызвать Takt: модель может продолжить обычный agent loop. Этот срез переносит начало workflow в хост кодинг-агента. Команда `/takt` перехватывается до отправки запроса модели, после подтверждения сессия переходит в managed mode, а Takt становится единственным владельцем переходов и изменяющих код фаз.

```text
/takt <задача>
→ host intercept
→ host.begin / plan preview
→ подтверждение
→ host.confirm / Takt execute
→ отдельные worker Run
→ terminal state
→ итог основной сессии
```

## Уровни контроля

- `advisory` — интеграция только показывает состояние; обход не блокируется.
- `guarded` — хост применяет часть ограничителей.
- `strict` — принимается только при заявленных `command_interception`, `input_interception`, `tool_call_blocking`, `completion_blocking` и `session_recovery`.

Заявление возможностей является контрактом доверенного локального расширения, а не удалённой аттестацией. Takt отклоняет `strict`, если хотя бы одна возможность не объявлена.

## Durable host session

Связь хоста и плана хранится в `.takt/host-sessions/<id>.json`:

- host и стабильный ID его сессии;
- `plan_id`;
- статус `preview|managed|waiting|completed|failed|released`;
- уровень контроля и заявленные возможности;
- время создания, обновления и явного release.

После перезапуска расширение вызывает `host.find` и восстанавливает managed mode. `release` является явным операторским выходом и не отменяет уже запущенный Takt Run.

## API

CLI:

```bash
takt host begin "<задача>" --host pi --host-session <id> \
  --enforcement strict --command-interception --input-interception \
  --tool-call-blocking --completion-blocking --session-recovery --daemon

takt host confirm <session-id> --confirm --daemon
takt host status <session-id> --daemon
takt host find --host pi --host-session <id> --daemon
takt host guard-tool <session-id> --tool edit --daemon
takt host guard-completion <session-id> --kind final --daemon
takt host release <session-id> --daemon
```

MCP публикует те же операции как `takt.host.begin|confirm|get|find|guard_tool|guard_completion|release`. Всего локальный MCP содержит 36 инструментов.

## Контроль основной сессии

Пока host session имеет активный статус:

- Takt control tools и известные read-only инструменты разрешены;
- `edit`, `write`, shell, Git и неизвестные инструменты блокируются до выполнения;
- поле `read_only` от хоста считается подсказкой и не может превратить неизвестный инструмент в разрешённый;
- обычный пользовательский ввод направляется в `takt steer`, не вызывая основную модель;
- final completion блокируется; разрешены только status, а в `waiting` — question/status.

Основная сессия не выполняет рабочие фазы. Их исполняют отдельные Pi/OpenCode worker-сессии под обычным Takt DAG, output contracts, hooks, policies, budgets и artifacts.

## Pi

`integrations/coding-agent-host-control/pi/index.ts` использует нативную команду `/takt`, события `input`, `before_agent_start`, `tool_call`, `user_bash` и session lifecycle. Расширение:

- перехватывает задачу до main agent loop;
- сохраняет только read-only active tools;
- отправляет последующий ввод как steering;
- блокирует обходные tool calls и прямой user bash;
- восстанавливает durable session на `session_start`.

## OpenCode

`integrations/coding-agent-host-control/opencode/index.ts` использует V2 plugin API:

- command transform помещает управляющий маркер;
- session `context` hook обрабатывает маркер и активный ввод непосредственно перед model dispatch и fail-closed прерывает dispatch;
- `tool.execute.before` проверяет каждый tool через Takt.

OpenCode V2 plugin API пока beta, поэтому корпоративное внедрение должно фиксировать совместимую версию и включать live smoke в своей среде.

## Исправления v0.1.35

- macOS unit test использует тот же `EvalSymlinks`, что и runtime;
- fingerprint пакета включает транзитивные команды, subworkflow, script source/dependencies, path skills и MCP-конфигурации;
- отсутствующий `allowed_integrations` не ограничивает каталог, явный `[]` запрещает все интеграции;
- steering из `running` и `waiting` использует единый порог `>= max_iterations`;
- foreground advance читает план только после межпроцессной блокировки;
- ошибки workflow listing, JSON encoding и promote rollback возвращаются вызывающему коду;
- профиль без `block_packages` сохраняет статические workflow, но Dynamic Plan требует явной миграции;
- `Detached` выбирается транспортом: daemon — detached, прямой CLI/MCP — foreground.

## Границы

- Pi/OpenCode binaries в release CI не запускаются: TypeScript integrations проходят структурный контракт, Go host API — unit и end-to-end tests. Перед внедрением нужен smoke с целевой версией хоста.
- Host guard контролирует наблюдаемые действия, но не внутреннее рассуждение LLM.
- Кеш каталога по fingerprint отложен; `plan.get/preview` пока повторно загружают локальный каталог.
