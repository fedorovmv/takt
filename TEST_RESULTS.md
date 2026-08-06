# Результаты проверок — v0.1.33-alpha

Дата проверки: 2026-08-06.

## Среда

- Linux x86_64, kernel `6.18.35`;
- Go `1.23.2`;
- GNU Bash `5.2.37`;
- Python `3.13.5`;
- Node `22.16.0`;
- локальные детерминированные adapters, временные Git-репозитории и изолированный fake GitHub CLI;
- реальный macOS runner в этой среде недоступен.

В CI сохраняется матрица `ubuntu-latest` / `macos-latest`. Фактический macOS PASS в этом отчёте не заявляется.

## Строгий authoring

| Контракт | Результат |
|---|---|
| неизвестные YAML/JSON-поля отклоняются с путём и `did you mean` для близкой опечатки | PASS |
| `takt validate` проверяет effective capabilities локальных adapters до создания Run | PASS |
| governed child workflow и `loop_group` участвуют в рекурсивном capability preflight | PASS |
| ссылки `${nodes.*}` проверяются по направлению зависимостей и `output_format` | PASS |
| loop-узел может ссылаться на upstream-узел контейнера; добавлена отдельная регрессия | PASS |
| approval, artifact, fan-out и loop references получают предметную диагностику | PASS |
| `${path}` fail-closed, `${path?}` optional, `${path:-default}` использует fallback | PASS |
| предупреждения можно перевести в ошибки через `--warnings-as-errors` | PASS |
| расширенные ограничения JSON Schema проверяются при validate и на результате | PASS |
| `always_run` выполняет cleanup после failed dependency, не меняя итог failed Run | PASS |
| `idle_timeout` сбрасывается activity events и завершается как `timed_out` | PASS |
| противоречивые `always_run`, `trigger_rule`, `when`, timeout/idle_timeout диагностируются заранее | PASS |

## Локальный daemon

| Контракт | Результат |
|---|---|
| `takt daemon start/status/stop/serve` поверх Unix socket | PASS |
| один daemon на workspace, lock и stale-socket recovery | PASS |
| background Run продолжает работу после завершения запускающего CLI | PASS |
| несколько локальных клиентов используют один control service | PASS |
| event subscription передаёт NDJSON по revision cursor до terminal-состояния | PASS |
| `takt mcp --daemon` проксирует полный локальный MCP control/worker plane | PASS |
| claimed external worker обслуживается daemon и получает activity-based idle timeout | PASS |
| незавершённые tool calls закрываются при idle expiry, узел становится `timed_out` | PASS |
| monitor goroutine завершается до shutdown и не обращается к удалённому workspace | PASS |
| короткие конкурирующие изменения Run повторяют transient lock с ограниченным ожиданием | PASS |
| Store остаётся файловым, неблокирующим и без БД | PASS |

## Совместимость существующих процессов

| Контракт | Результат |
|---|---|
| шесть глубоких workflow профиля `code` проходят Git fixture | PASS |
| условные recovery/revalidation references переведены на явные optional expressions | PASS |
| profile `code` 0.9.1 и authoring skill 0.15.0 | PASS |
| Route DSL, включая ссылку loop-команды на upstream контейнера, проходит strict validate | PASS |
| worktree, governed child Runs, fan-out, policies, scripts/artifacts и MCP | PASS |

## Полный шлюз рабочего дерева

```text
gofmt                                      PASS
JSON schema parse                          PASS
bash syntax                                PASS
go vet ./...                               PASS
go test ./... -count=1                     PASS
go test -race ./... -count=1               PASS
fake-assistant contracts                   PASS
Pi adapter contracts                       PASS
OpenCode adapter contracts                 PASS
Route DSL end-to-end/evaluation/isolation  PASS
workflow composition                       PASS
Takt authoring skill                       PASS
code profile 0.9.1                         PASS
Git worktree lifecycle                     PASS
governed child Runs                        PASS
node capability policies                   PASS
governed child fan-out                     PASS
script and typed artifacts                 PASS
local MCP control plane                    PASS
external controlled executor               PASS
deep six-workflow Git fixture              PASS
authoring contract                         PASS
daemon contract                            PASS
documentation and examples                 PASS
```

Прямой race-вызов через ограниченный tool-сеанс был остановлен по времени после успешных пакетов до `internal/profile`; оставшиеся пакеты были выполнены отдельной неизменённой группой и прошли. Затем стандартный `scripts/verify.sh`, включающий полный `go test -race ./...`, был запущен одним отсоединённым процессом и завершился с кодом `0` и строкой `verification: PASS`. Он выполнялся на окончательном рабочем дереве после исправления статического анализа loop-scope.

## Что не проверялось

- фактическая GitHub Actions job на macOS;
- реальные сетевые вызовы GitHub;
- реальные сеансы Claude, Codex, OpenCode и Pi;
- TCP, удалённые workers и многопользовательская авторизация: daemon намеренно Unix-only;
- продолжение работающего локального OS-процесса после падения или перезапуска самого daemon;
- системный OS sandbox, redaction секретов и distributed locking;
- полноценный Route DSL benchmark на обезличенных реальных заданиях.

## Проверка поставляемого архива

Первый релизный ZIP распакован в новый пустой каталог. Непосредственно из распакованной копии прошли:

```text
sha256sum -c MANIFEST.sha256              PASS
VERSION=0.1.33-alpha                      PASS
profile code VERSION=0.9.1                PASS
skill VERSION=0.15.0                      PASS
go vet ./...                              PASS
go test ./...                             PASS
go test -race ./...                       PASS
все adapter/runtime/MCP contracts         PASS
deep six-workflow Git fixture             PASS
authoring contract                        PASS
daemon contract                           PASS
documentation and examples                PASS
scripts/verify.sh                          PASS
verification: PASS                        PASS
exit code                                 0
```

После добавления этого фактического результата изменились только `TEST_RESULTS.md` и `MANIFEST.sha256`. Финальный ZIP пересобран детерминированно, распакован в ещё один чистый каталог и повторно прошёл тот же полный `scripts/verify.sh` с кодом `0`; после этой проверки исходный код и функциональные файлы не изменялись.
