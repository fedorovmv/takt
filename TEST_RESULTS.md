# Test results — v0.1.31-alpha

Дата проверки: 2026-08-05.

## Среда

- Linux x86_64, kernel `6.18.35`;
- Go `1.23.2`;
- GNU Bash `5.2.37`;
- Python `3.13.5`;
- Node `22.16.0`;
- штатные fake assistants для process, Pi и OpenCode contracts;
- реальный macOS runner в этой среде недоступен.

В CI сохраняется матрица `ubuntu-latest` / `macos-latest`. Фактический macOS PASS в этом локальном отчёте не заявляется.

## Исправления ревью

| Контракт | Результат |
|---|---|
| конкурентный `FS.Load` во время `Commit` не выдаёт transient inconsistency | PASS |
| устойчивый revision mismatch остаётся `InconsistentError` | PASS |
| `events.idx` и чтение после revision без полного scan | PASS |
| MCP race-тест `-count=8` | PASS |
| CLI lifecycle делегирует общему control service | PASS |
| numeric JSON-RPC ID больше `2^53` не схлопываются | PASS |
| extension fields envelope допускаются, invalid request получает `-32600` | PASS |
| duplicate fan-out отклоняется, явный `allow_duplicates` работает | PASS |
| пустой/дублированный smart-review output блокируется schema | PASS |
| pre-start cancellation marker сохраняется | PASS |
| static subworkflow ребейзит script path/dependencies | PASS |
| единый artifact type contract | PASS |

## Agent events и внешний executor

| Контракт | Результат |
|---|---|
| `executor: external` только для command/prompt | PASS |
| durable pending task и `waiting.kind=external_node` | PASS |
| capability preflight при claim | PASS |
| bounded lease и reclaim после истечения | PASS |
| opaque token и redaction из public Run | PASS |
| нормализованные message/tool/usage/diagnostic/terminal events | PASS |
| successful structured completion | PASS |
| raw stdout сохраняется отдельно | PASS |
| обычные `output_format`, hooks, artifacts и downstream DAG после submission | PASS |
| external failure → retry → новый attempt и claim | PASS |
| 15 MCP tools в детерминированном порядке | PASS |
| live stdio MCP worker contract | PASS |

## Релизный шлюз рабочего дерева

```text
gofmt -w cmd internal                         PASS
go vet ./...                                  PASS
go test ./...                                 PASS
go test -race ./...                           PASS
fake-assistant contracts                      PASS
Pi adapter contracts                          PASS
OpenCode adapter contracts                    PASS
Route DSL end-to-end/evaluation/isolation     PASS
workflow composition                          PASS
Takt authoring skill                          PASS
code profile catalog                          PASS
Git worktree lifecycle                        PASS
governed child Runs                           PASS
node capability policies                      PASS
governed child fan-out                        PASS
script and typed artifacts                    PASS
local MCP control plane                       PASS
external node executor                        PASS
documentation and examples                    PASS
scripts/verify.sh                             PASS
verification: PASS                            PASS
exit code                                     0
```

Один интерактивный `make check` был остановлен внешним лимитом при входе в Pi contract после успешных unit/race/fake-assistant этапов. Все оставшиеся неизменённые цели выполнены отдельно, а полный `scripts/verify.sh` затем завершился с кодом `0`.

## Что не проверялось

- реальные обращения к Pi, OpenCode и внешним моделям;
- фактическая CI job на macOS;
- реальные GitHub/Remotion операции профиля `code`;
- сетевой transport, удалённая аутентификация и multi-user worker;
- переживание закрытия MCP-клиента отдельным daemon — daemon не входит в этот срез;
- системный OS sandbox и secret redaction state/events/artifacts;
- внешний object storage.

## Проверка поставляемого архива

Архив распакован в новый пустой каталог. Непосредственно из распакованной копии прошли:

```text
sha256sum -c MANIFEST.sha256              PASS
VERSION=0.1.31-alpha                      PASS
profile code VERSION=0.8.1                PASS
skill VERSION=0.13.0                      PASS
go test ./... -count=1                    PASS
go test -race ./... -count=1              PASS
fake-assistant contracts                  PASS
Pi adapter contracts                      PASS
OpenCode adapter contracts                PASS
Route DSL contracts                       PASS
workflow composition                      PASS
authoring skill and code profile          PASS
Git worktree and governed child Runs      PASS
node policies and dynamic fan-out         PASS
script and typed artifacts                PASS
local MCP control plane                   PASS
external node executor                    PASS
documentation and examples                PASS
scripts/verify.sh                         PASS
verification: PASS                        PASS
exit code                                 0
```

Проверка выполнена на содержимом поставляемого ZIP. После изменения только этого отчёта архив и `MANIFEST.sha256` пересобраны; финальная распаковка повторно проходит манифест, ключевые race-тесты, MCP, внешний executor и проверку документации.
