# План развития

Документ показывает приоритеты после `v0.1.32-alpha`. Server, Web UI и БД остаются proposal-направлением для возможного нелокального режима и не определяют ближайший локальный runtime.

## Выполнено в v0.1.27-alpha. Политики и возможности узлов

Реализованы `allowed_tools`, `denied_tools`, explicit empty allowlists, skills, MCP, assistant-enforced filesystem/network policy, `requires`, capability preflight, fingerprints ресурсов, persistence и наследование governed child Run. Неподдерживаемая гарантия отклоняется до вызова adapter.

## Выполнено в v0.1.28-alpha. Динамический fan-out governed children

Реализованы массив элементов из структурированного output upstream-узла, один child Run на элемент, `max_parallel`, устойчивые IDs и fingerprints, частичный resume, ordered aggregation, `all_success|all_done|one_success`, выборочная и каскадная отмена. Smart review и comprehensive review профиля `code` используют этот механизм.

## Выполнено в v0.1.29-alpha. Script nodes и типизированные артефакты

Реализованы runtime `command|python|node|go`, file/inline source, args/env/working directory, fingerprints исходника и dependencies, structured output, `output_type`/MIME/SHA-256/producer metadata, CLI `takt artifacts` и передача ссылок parent/child/fan-out. Профиль `code` использует script и plan/PRD artifacts.

## Выполнено в v0.1.30-alpha. Локальный MCP control plane

Реализованы stdio MCP server, dual-era protocol negotiation, workflow list/describe, detached Run start, get/resume/answer/cancel, children, artifacts с содержимым и revision event polling. Реализация использует существующий runtime/store и не вводит daemon, HTTP, Web UI или БД. Полный архив среза: `44-local-mcp-control-plane-v0.1.30.md`.

## Выполнено в v0.1.31-alpha. Агентные события и внешний исполнитель

Реализованы provider-neutral базовые `assistant.*`/tool-call events, `Request.Emit`, durable `executor: external`, claim с capability attestation и lease/token, MCP tools pending/claim/event/complete/fail и возврат результата в обычные retry/hooks/output/artifact semantics. Одновременно устранён torn-read store при конкурентном polling и добавлен индекс событий. Полный архив среза: `45-agent-events-external-executor-v0.1.31.md`.

## Выполнено в v0.1.32-alpha. Управляемый agent lifecycle и глубокие workflow

Завершён event protocol v2: session lifecycle, tool request/allow/deny/start/complete, отдельная отмена вызова, artifact declaration с `call_id`, usage/diagnostic/terminal events и capability declaration adapter. Внешний executor обеспечивает блокирующий policy/approval до запуска инструмента и не может завершить узел с незакрытыми tool calls.

Шесть основных процессов профиля `code` 0.9.0 получили строгие JSON-входы, специализированные предметные команды, обязательные checkpoint artifacts, domain error codes, Git decision trees, validation recovery и сквозной локальный Git/GitHub fixture. Полный архив среза: `46-controlled-agent-events-deep-workflows-v0.1.32.md`.

## Приоритет 1. Улучшение authoring

- предупреждения о подозрительных полях и `did you mean` для опечаток;
- capability preflight в `takt validate`, а не только перед запуском узла;
- статическая диагностика `${nodes.*}` и artifact references;
- подсказки по несовместимым параметрам;
- более полный JSON Schema;
- `always_run`;
- `idle_timeout`;
- строгий renderer;
- optional/default expressions.

## Приоритет 2. Опциональный локальный daemon

```bash
takt daemon
```

Daemon нужен только для локальных сценариев, которым недостаточно времени жизни одного CLI/MCP-процесса:

- фоновые Run;
- продолжение после закрытия клиента;
- event subscriptions;
- постоянный MCP endpoint;
- несколько локальных coding agents;
- восстановление активных external leases после перезапуска.

Файловый Store остаётся источником истины; БД не требуется. HTTP, удалённый доступ и многопользовательская авторизация не входят в этот срез.

## Приоритет 3. Усиление runtime и безопасность локального исполнения

- раннее завершение `one_success` и `all_success` с отменой ненужных детей;
- нормализованные diagnostics и fingerprints ошибок;
- retry с backoff;
- защита секретов в state/events/artifacts;
- реальный OS sandbox для недоверенных процессов;
- path-based namespace для вложенных циклов.

## Приоритет 4. Предметная проверка

Отдельно от системных функций нужен Route DSL benchmark со штатным валидатором и обезличенными реальными заданиями: success@1, final success, число попыток, стоимость и стабильность на неизменных fingerprints.

## Отложенные proposals

Server, Web UI, БД, удалённые workers, message adapters, notifications и многопользовательская авторизация рассматриваются только после появления задачи нелокального использования и отдельной threat model.
