# Test results — v0.1.30-alpha

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

## Локальный MCP control plane

| Контракт | Результат |
|---|---|
| stdio newline-delimited JSON-RPC | PASS |
| legacy `initialize` для MCP 2025 | PASS |
| stateless `server/discover` для `2026-07-28` | PASS |
| server identity в `_meta` | PASS |
| детерминированный список 10 tools | PASS |
| строгая проверка tool arguments | PASS |
| workflow start/get | PASS |
| detached start по умолчанию | PASS |
| durable cancellation активного detached Run | PASS |
| revision event cursor и limit | PASS |
| typed artifact metadata и bounded content | PASS |
| approval answer и продолжение Run | PASS |
| text + structured tool result | PASS |
| JSON-RPC request cancellation context | PASS |
| process contract через собранный `bin/takt mcp` | PASS |

## Регрессионный контур

Новый MCP adapter использует общий control service поверх существующих runtime/store/locks. Повторно прошли прежние контракты:

- fake assistant, Pi и OpenCode adapters;
- Route DSL end-to-end и evaluation isolation;
- workflow composition и authoring skill;
- профиль `code`;
- Git worktree lifecycle;
- governed child Runs, policies и dynamic fan-out;
- script nodes и typed artifacts;
- documentation checks и набор example validations.

## Выполненные команды

```text
gofmt -w cmd internal                         PASS
go vet ./...                                  PASS
go test ./... -count=1                        PASS
go test -race ./... -count=1                  PASS
go test ./internal/mcp -count=1               PASS
go test -race ./internal/mcp -count=1         PASS
make mcp-contract                             PASS
make check                                    PASS
scripts/verify.sh                             PASS
verification: PASS                            PASS
example validation set                       PASS
```

## Что не проверялось

- реальные обращения к Pi, OpenCode и внешним моделям;
- подключение из фактических версий Claude Code, Codex, OpenCode и Pi — проверен MCP transport и contract, а готовые клиентские инструкции входят в следующий срез;
- реальные GitHub/Remotion операции профиля `code`;
- фактическая CI job на macOS;
- сетевой transport, daemon и восстановление detached Run после завершения MCP-процесса — эти возможности не входят в v0.1.30;
- внешний object storage и secret redaction артефактов;
- системный OS sandbox — текущая filesystem/network policy остаётся assistant-enforced.

## Проверка поставляемого архива

ZIP распакован в новый пустой каталог `/mnt/data/takt-clean-030/takt`. Непосредственно из распакованной копии прошли:

```text
sha256sum -c MANIFEST.sha256                  PASS
VERSION=0.1.30-alpha                           PASS
profile code VERSION=0.8.0                     PASS
skill VERSION=0.12.0                           PASS
make check                                     PASS
scripts/verify.sh                              PASS
verification: PASS                             PASS
exit code                                      0
```

Чистый прогон включал unit/race, fake/Pi/OpenCode contracts, Route DSL, evaluation, composition, skill/profile, worktree, child-run, policy, fan-out, script/artifact, MCP, документацию и example validation. После фиксации этого отчёта манифест и ZIP пересобираются, а финальная распакованная копия проверяется повторно без изменения исходников.
