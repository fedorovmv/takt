# План развития

Документ показывает приоритеты после `v0.1.27-alpha`. Server, Web UI и БД остаются proposal-направлением для возможного нелокального режима и не определяют ближайший локальный runtime.

## Выполнено в v0.1.27-alpha. Политики и возможности узлов

Реализованы `allowed_tools`, `denied_tools`, explicit empty allowlists, skills, MCP, assistant-enforced filesystem/network policy, `requires`, capability preflight, fingerprints ресурсов, persistence и наследование governed child Run. Неподдерживаемая гарантия отклоняется до вызова adapter. Реальный OS sandbox остаётся отдельным усилением безопасности.

## Приоритет 2. Динамический fan-out governed children

- массив элементов из структурированного output предыдущего узла;
- один child Run на элемент;
- ограничение конкурентности;
- стабильные iteration/child IDs и fingerprint списка;
- resume после частичного завершения;
- join policies и ordered aggregation;
- отмена отдельного элемента и всего fan-out;
- использование в smart review и Ralph.

Этот этап также расширит scheduler: независимые `workflow`-узлы смогут выполняться параллельно.

## Приоритет 3. Script nodes и типизированные артефакты

- общий `script` contract для executable, Python, Node/Bun и при необходимости `go run`;
- timeout, stdout/stderr, structured output, dependencies и fingerprints;
- `output_type`, MIME, checksum, producer Run/Node и metadata;
- `takt artifacts <run-id>`;
- передача артефактов между parent/child Run без угадывания имён файлов.

## Приоритет 4. Локальная интеграция с кодовыми агентами

- MCP server: list/describe/start/get/children/answer/cancel/artifacts;
- готовые skills для Pi, OpenCode, Codex и Claude Code;
- Codex/Claude Code adapters;
- поток событий Run для вызывающего агента;
- режим caller, в котором внешний агент выполняет отдельный узел, а Takt управляет workflow.

## Приоритет 5. Усиление runtime

- параллельные волны для hooks/retry и governed nodes;
- retry backoff;
- строгие неизвестные template variables и defaults;
- более полный JSON Schema и richer expressions;
- нормализованные diagnostics и fingerprints ошибок;
- полная история loop iterations;
- secret redaction в state/events;
- recovery stale locks и interrupted commits.

## Предметная проверка

Параллельно с системной разработкой:

- заменить минимальный Route DSL validator штатным `route-tool`;
- прогнать не менее десяти обезличенных заданий;
- фиксировать success@1, final success, attempts, duration, tokens/cost и ручные правки;
- сравнивать стратегии только на неизменных workflow/config/cases/workspace/validator fingerprints;
- добавить реальные Go и document workflow-наборы.

## Отложенные proposals

После решения о нелокальном или многопользовательском использовании отдельно проектируются:

- server и Web UI;
- БД и distributed locking;
- remote workers;
- authentication/authorization;
- message adapters и notifications;
- quotas, tenancy и audit retention.
