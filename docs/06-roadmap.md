# План развития

Документ показывает приоритеты после `v0.1.28-alpha`. Server, Web UI и БД остаются proposal-направлением для возможного нелокального режима и не определяют ближайший локальный runtime.

## Выполнено в v0.1.27-alpha. Политики и возможности узлов

Реализованы `allowed_tools`, `denied_tools`, explicit empty allowlists, skills, MCP, assistant-enforced filesystem/network policy, `requires`, capability preflight, fingerprints ресурсов, persistence и наследование governed child Run. Неподдерживаемая гарантия отклоняется до вызова adapter.

## Выполнено в v0.1.28-alpha. Динамический fan-out governed children

Реализованы массив элементов из структурированного output upstream-узла, один child Run на элемент, `max_parallel`, устойчивые IDs и fingerprints, частичный resume, ordered aggregation, `all_success|all_done|one_success`, выборочная и каскадная отмена. Smart review и comprehensive review профиля `code` используют этот механизм.

## Приоритет 1. Script nodes и типизированные артефакты

- единый `script`-узел с runtime `command`, затем адаптеры Python/Node;
- stdout/stderr, timeout, cancellation и структурированный output по тем же правилам, что у других узлов;
- fingerprint файла скрипта, зависимостей и runtime-настроек;
- `output_type`, MIME, checksum и producer metadata;
- CLI `takt artifacts`;
- передача типизированных артефактов между parent/child Run без соглашений о случайных именах файлов.

Это следующий крупный срез: он расширит детерминированную часть workflow и устранит зависимость каталога от inline bash для обработки данных.

## Приоритет 2. Локальная интеграция с кодовыми агентами

- локальный MCP-сервер Takt: list/describe/start/status/answer/cancel/artifacts;
- skills для OpenCode, Pi, Codex и Claude Code;
- поток событий Run для вызывающего агента;
- режим внешнего исполнителя одного узла при сохранении оркестрации в Takt.

## Приоритет 3. Усиление runtime

- раннее завершение `one_success` с отменой оставшихся детей;
- строгий renderer с optional/default values;
- нормализованные diagnostics и fingerprint ошибок;
- retry с backoff;
- расширение JSON Schema и языка условий;
- защита секретов в state/events;
- реальный OS sandbox для недоверенных процессов.

## Предметная проверка

Отдельно от системных функций нужен Route DSL benchmark со штатным валидатором и обезличенными реальными заданиями: success@1, final success, число попыток, стоимость и стабильность на неизменных fingerprints.

## Отложенные proposals

Server, Web UI, БД, удалённые workers, message adapters, notifications и многопользовательская авторизация рассматриваются только после появления задачи нелокального использования и отдельной threat model.
