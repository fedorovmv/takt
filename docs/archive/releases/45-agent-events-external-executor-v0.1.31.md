# Нормализованные события и внешний исполнитель узла — v0.1.31-alpha

## Назначение среза

Локальный MCP control plane v0.1.30 позволял запускать и наблюдать Run, но каждый AI-узел по-прежнему исполнялся только встроенным adapter Takt. В v0.1.31 один `command` или `prompt` можно передать внешнему coding agent, сохранив Takt источником истины для DAG, retries, hooks, approvals, artifacts, child Runs и fingerprints.

Срез одновременно устраняет конфликт файлового store с постоянными конкурентными читателями MCP и вводит provider-neutral журнал agent/tool events.

## Workflow contract

```yaml
- id: implement
  command: implement-change
  executor: external
  allowed_tools: [read, grep, edit, write, bash]
  requires: [tool_policy]
  output_format:
    type: object
    properties:
      summary:
        type: string
      tests_passed:
        type: boolean
    required: [summary, tests_passed]
  output_type: implementation-report
  output_mime: application/json
```

`executor: external` разрешён только для `command` и `prompt`. Takt до hand-off:

1. разрешает Markdown-команду, assistant/model/session и шаблоны;
2. вычисляет effective node policy;
3. сохраняет prompt, workspace, requested model, output schema и required capabilities;
4. переводит Run в `waiting` с `kind: external_node`.

Попытка приостанавливается без расходования номера. После результата та же попытка продолжается обычным runtime-путём. Protocol/output failure участвует в `attempts.retry_on`, hooks получают feedback, successful result проходит artifact capture.

## MCP tools внешнего исполнителя

К десяти control tools добавлены:

- `takt.node.pending` — найти pending-задачи или задачи с истёкшим lease;
- `takt.node.claim` — заявить worker, capabilities и срок lease, получить opaque token;
- `takt.node.event` — добавить нормализованное событие под активным token;
- `takt.node.complete` — передать успешный output/structured/stdout/stderr/usage/model/session;
- `takt.node.fail` — передать классифицированный отказ.

Типовой цикл:

```text
takt.run.start
  → takt.node.pending
  → takt.node.claim
  → takt.node.event ...
  → takt.node.complete | takt.node.fail
  → takt.run.get / takt.run.events
```

Claim является durable lease, а не владением MCP-сессии. Истёкший lease можно забрать другим worker. Token возвращается только claim-вызову и удаляется из публичного `RunState`.

Capability attestation проверяется до выдачи claim. Внешний worker не может заявить выполнение узла без требуемых `tool_policy`, `skills`, `mcp` или sandbox capabilities.

## Нормализованные события

Takt сохраняет события:

- `assistant.started`;
- `assistant.message`;
- `assistant.tool.started`;
- `assistant.tool.completed`;
- `assistant.usage`;
- `assistant.diagnostic`;
- `assistant.completed`;
- `assistant.failed`.

Поля события не зависят от конкретного provider: `message`, `tool`, `call_id`, JSON `input/output`, usage, provider, session и расширяемые data. Встроенные adapters получают `Request.Emit`; runtime также гарантированно добавляет lifecycle, финальное message и usage. Во время параллельной DAG-волны события буферизуются внутри исполнения и фиксируются сериализованно в порядке узлов, сохраняя единый авторитетный event log.

Внешний executor отправляет tool events непосредственно через MCP. Каждое событие проверяется, получает локальный sequence внутри claim и durable Run revision.

## Конкурентное чтение store

`Store.Commit` сохраняет event journal перед state. Конкурентный читатель мог попасть между двумя atomic rename и увидеть `events@N+1` вместе с `state@N`.

`FS.Load` теперь:

- перечитывает state после event revision;
- повторяет transient mismatch с коротким bounded backoff;
- возвращает `InconsistentError`, если mismatch устойчив и похож на прерванный commit.

Регрессия покрыта конкурентным `Load` во время серии `Commit` под race detector.

`events.idx` хранит фиксированный byte offset каждой revision. `ReadEvents(after_revision)` делает seek к следующему событию. Старые Runs без индекса остаются читаемыми через полный scan.

## Дополнительные исправления

- CLI `answer/resume/status/children/artifacts/cancel` использует тот же control service, что MCP.
- JSON-RPC numeric IDs не проходят через `float64`; extension fields envelope допускаются.
- fan-out отклоняет одинаковые канонические элементы, если явно не задано `allow_duplicates: true`;
- smart review требует минимум одну уникальную перспективу;
- cancel marker, созданный до старта linked fan-out child, больше не очищается;
- static subworkflow ребейзит `script.path`, `script.dependencies`, MCP и path skills относительно дочернего файла;
- Go script runtime запускает один source file; `dependencies` участвуют в fingerprint, но не добавляются к `go run` автоматически.

## Границы

- Внешний executor доступен через локальный stdio MCP; отдельного daemon пока нет. Закрытие MCP-клиента не останавливает уже запущенный встроенный Run, но новый внешний claim требует живого клиента/worker.
- Claim/token — механизм координации локальных доверенных процессов, а не удалённая аутентификация.
- Нормализованный event contract не пытается без потерь сохранить все provider-specific records; raw stdout/stderr остаются в результате узла.
- OS sandbox и secret redaction остаются отдельными задачами runtime hardening.

## Проверки

- конкурентный `Load` во время `Commit` и persistent inconsistency;
- indexed incremental event reads и fallback старого Run;
- MCP claim capability preflight, token redaction, event, structured completion, raw stdout, usage, artifact и downstream node;
- normalized events встроенного adapter;
- duplicate fan-out, explicit duplicate opt-in и pre-start cancellation;
- static subworkflow с script path/dependency;
- полный unit/race/contract gateway и чистая проверка release ZIP.
