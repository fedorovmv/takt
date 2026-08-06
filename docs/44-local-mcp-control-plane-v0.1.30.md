# Локальный MCP control plane v0.1.30-alpha

> Продолжение: v0.1.31-alpha устраняет transient torn reads файлового store, добавляет `events.idx`, нормализованные assistant/tool events и инструменты внешнего executor. См. `45-agent-events-external-executor-v0.1.31.md`.


## 1. Назначение среза

Срез закрывает первый крупный пункт roadmap после `v0.1.29-alpha`: кодовый агент может управлять Takt как локальным MCP-сервером, не вызывая CLI-команды вручную и не создавая отдельный сервер, БД или второй runtime.

MCP-интерфейс использует тот же файловый store, runtime, fingerprints, locks, governed child Runs, worktree lifecycle и typed artifacts, что и CLI. Состояние не дублируется в отдельной модели.

## 2. Транспорт и версии протокола

Команда:

```bash
takt mcp --workspace . --config .takt/config.yaml
```

запускает stdio-сервер с newline-delimited JSON-RPC. Stdout принадлежит только MCP transport; диагностика сервера выводится в stderr.

Поддерживаются два поколения MCP:

- legacy initialization для `2025-03-26`, `2025-06-18` и `2025-11-25` через `initialize`;
- stateless discovery `2026-07-28` через `server/discover` и server identity в `_meta` ответа.

Инструменты возвращаются в детерминированном порядке. Сервер принимает `notifications/cancelled` и отменяет соответствующий активный JSON-RPC request context.

## 3. Инструменты

| Tool | Назначение |
|---|---|
| `takt.workflow.list` | список селекторов установленного профиля |
| `takt.workflow.describe` | публичный DAG и типы узлов выбранного workflow |
| `takt.run.start` | проверка определений и запуск Run; detached по умолчанию |
| `takt.run.get` | текущее публичное состояние Run |
| `takt.run.resume` | продолжение resumable Run после внешнего исправления |
| `takt.run.answer` | ответ approval и продолжение child/parent chain |
| `takt.run.cancel` | durable cancellation корня и активного дерева детей |
| `takt.run.children` | прямые governed children и fan-out metadata |
| `takt.run.artifacts` | typed artifacts, checksum, provenance и опциональное содержимое |
| `takt.run.events` | события после revision cursor с bounded long polling |

Каждый `tools/call` возвращает одновременно:

- текстовый JSON-блок для совместимых legacy-клиентов;
- `structuredContent` для машинного чтения;
- `resultType: complete` для MCP 2026;
- `isError: true` для ошибки инструмента без нарушения JSON-RPC transport.

## 4. Detached start и наблюдение

`takt.run.start` по умолчанию запускает workflow в фоне текущего MCP-процесса и возвращает устойчивый `run_id`, как только Run сохранён либо принят к запуску. Дальнейшая работа:

```text
run.start
→ run.get
→ run.events(after_revision=N, wait_ms=30000)
→ run.answer / run.cancel
→ run.artifacts
```

`run.events` использует revision из существующего `events.jsonl`. Cursor является номером последней обработанной revision, поэтому повторный запрос не теряет события и не требует server-side MCP session.

Отсоединённый запуск остаётся локальным: завершение процесса `takt mcp` завершает его in-process execution contexts. Это не daemon и не удалённая очередь.

## 5. Approval, children и cancellation

MCP не вводит новую семантику управления Run:

- ответ по корневому Run разрешается в фактический approval дочернего Run;
- несколько ожидающих fan-out children требуют явного выбора child Run;
- fingerprints проверяются до потребления ответа;
- cancel marker записывается в тот же store;
- waiting child и parent chain продолжаются теми же runtime-функциями, что CLI.

## 6. Артефакты

`takt.run.artifacts` поддерживает фильтры `node_id`, `type`, `recursive`. При `include_content: true` сервер читает локальный snapshot из `ArtifactRef.path` и добавляет:

- UTF-8 для текстовых MIME;
- base64 для бинарных MIME;
- `content_truncated`, если достигнут `max_bytes`.

Размер включаемого содержимого ограничен 1 MiB на артефакт. SHA-256, producer Run/Node и исходный абсолютный путь сохраняются.

## 7. Граница безопасности

MCP-сервер не расширяет trust model Takt. Он предназначен для локального кодового агента того же пользователя и получает полномочия процесса `takt` на:

- запуск доверенных workflow и scripts;
- чтение state/events/artifacts;
- запись approval и cancellation marker;
- создание worktree и запуск внешних assistants.

Сервер не содержит аутентификации и не должен публиковаться по сети или использоваться как многопользовательский service endpoint.

## 8. Проверки

Добавлены:

- unit tests dual-era initialize/discover и детерминированного `tools/list`;
- lifecycle test `start → events → artifact content → answer → completed`;
- detached-by-default test с durable cancellation активного Run;
- strict rejection неизвестных tool arguments;
- store test revision cursor и limit;
- process contract `scripts/test-mcp.sh` через собранный `bin/takt`;
- target `make mcp-contract`, включённый в `make check` и `scripts/verify.sh`.

## 9. Архив roadmap

Исходный roadmap-пункт «локальный MCP-сервер Takt: list/describe/start/status/answer/cancel/artifacts» закрыт этим срезом и перенесён в раздел выполненных работ `docs/06-roadmap.md`.

Следующие самостоятельные направления не входят в этот релиз:

- поток внутренних agent tool-call events;
- внешний исполнитель одного узла;
- daemon, HTTP transport, Web UI и БД;
- server/untrusted mode;
- клиентские installer-команды для конкретных IDE/агентов.
