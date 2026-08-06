# План развития

Документ показывает приоритеты после `v0.1.31-alpha`. Server, Web UI и БД остаются proposal-направлением для возможного нелокального режима и не определяют ближайший локальный runtime.

## Выполнено в v0.1.27-alpha. Политики и возможности узлов

Реализованы `allowed_tools`, `denied_tools`, explicit empty allowlists, skills, MCP, assistant-enforced filesystem/network policy, `requires`, capability preflight, fingerprints ресурсов, persistence и наследование governed child Run. Неподдерживаемая гарантия отклоняется до вызова adapter.

## Выполнено в v0.1.28-alpha. Динамический fan-out governed children

Реализованы массив элементов из структурированного output upstream-узла, один child Run на элемент, `max_parallel`, устойчивые IDs и fingerprints, частичный resume, ordered aggregation, `all_success|all_done|one_success`, выборочная и каскадная отмена. Smart review и comprehensive review профиля `code` используют этот механизм.

## Выполнено в v0.1.29-alpha. Script nodes и типизированные артефакты

Реализованы runtime `command|python|node|go`, file/inline source, args/env/working directory, fingerprints исходника и dependencies, structured output, `output_type`/MIME/SHA-256/producer metadata, CLI `takt artifacts` и передача ссылок parent/child/fan-out. Профиль `code` использует script и plan/PRD artifacts.

## Выполнено в v0.1.30-alpha. Локальный MCP control plane

Реализованы stdio MCP server, dual-era protocol negotiation, workflow list/describe, detached Run start, get/resume/answer/cancel, children, artifacts с содержимым и revision event polling. Реализация использует существующий runtime/store и не вводит daemon, HTTP, Web UI или БД. Полный архив среза: `44-local-mcp-control-plane-v0.1.30.md`.

## Выполнено в v0.1.31-alpha. Агентные события и внешний исполнитель

Реализованы provider-neutral `assistant.*`/tool-call events, `Request.Emit`, durable `executor: external`, claim с capability attestation и lease/token, MCP tools pending/claim/event/complete/fail и возврат результата в обычные retry/hooks/output/artifact semantics. Одновременно устранён torn-read store при конкурентном polling и добавлен индекс событий. Полный архив среза: `45-agent-events-external-executor-v0.1.31.md`.

## Приоритет 1. Усиление runtime и безопасность локального исполнения

- раннее завершение `one_success` с отменой оставшихся детей;
- строгий renderer с optional/default values;
- нормализованные diagnostics и fingerprint ошибок;
- retry с backoff;
- расширение JSON Schema и языка условий;
- защита секретов в state/events;
- реальный OS sandbox для недоверенных процессов.

## Приоритет 2. Предметная проверка

Отдельно от системных функций нужен Route DSL benchmark со штатным валидатором и обезличенными реальными заданиями: success@1, final success, число попыток, стоимость и стабильность на неизменных fingerprints.

## Отложенные proposals

Server, Web UI, БД, удалённые workers, message adapters, notifications и многопользовательская авторизация рассматриваются только после появления задачи нелокального использования и отдельной threat model.
