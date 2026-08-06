# Результаты проверок — v0.1.37-alpha

Дата: 2026-08-06

## Реализованный срез

- Autonomous Run Operations поверх локального файлового Store и daemon;
- реестр и история root/child Run;
- attention queue для approval, question, tool approval, failed и paused;
- безопасные `pause/resume` на границах узлов и партий fan-out;
- `retry`, `fork`, отдельное terminal-состояние `abandoned`;
- PID-based recovery потерянных локальных executor после перезапуска daemon;
- агрегированная сводка Run с descendants, usage и provenance артефактов;
- durable notification inbox, дедупликация, ack и sinks `coding_agent_host|desktop|process`;
- команды автономного управления в Pi/OpenCode host extensions;
- исправления замечаний к Coding Agent Host Control v0.1.36-alpha.

## Go

```text
gofmt cmd/internal                             PASS
go vet ./...                                  PASS
go build ./...                                PASS
go test ./... -count=1                        PASS
race: первая группа Go-пакетов                PASS
race: runtime/store/validation/workflow/yaml  PASS
```

Единый race-запуск был разделён из-за внешнего лимита продолжительности команды. Все пакеты с тестами прошли `go test -race -count=1` в группах.

## Контракты

```text
fake assistant                                PASS
Pi adapter                                    PASS
OpenCode adapter                              PASS
workflow composition                          PASS
Takt authoring skill                          PASS
code profile 0.12.0                           PASS
Git worktree                                  PASS
governed child Runs                           PASS
governed child fan-out                        PASS
node capability policy                        PASS
script and typed artifacts                    PASS
local MCP control plane (48 tools)             PASS
external node executor                        PASS
deep code workflows                           PASS
authoring                                     PASS
Dynamic Takt                                  PASS
trusted block packages                        PASS
Coding Agent Host Control                     PASS
host integrations TypeScript                  PASS
Autonomous Run Operations                     PASS
documentation                                 PASS
Route DSL end-to-end                          PASS
Route DSL evaluation + isolation              PASS
local daemon                                  PASS ×4
```

## Autonomous Run Operations

```text
run registry/list filters                     PASS
attention queue                               PASS
recursive summary and artifact aggregation    PASS
safe pause at node boundary                   PASS
pause before next fan-out batch               PASS
pause/resume governed child tree              PASS
pre-armed pause marker before child state     PASS
pre-armed abandon marker before child state   PASS
resume waiting approval                       PASS
operator retry dependent closure              PASS
fork static Run and Dynamic Plan              PASS
abandon as distinct terminal state            PASS
PID-based worker_lost recovery                PASS
notification deduplication and ack             PASS
notification process sink                     PASS
MCP run/notification operations               PASS
```

## Исправления Coding Agent Host Control

```text
completed host session not reused             PASS
begin serialized by interprocess lock         PASS
corrupt host record fails closed              PASS
host store rename + directory fsync            PASS
unknown session_id fails closed               PASS
exact control-tool allowlist                  PASS
arbitrary takt.* name denied                  PASS
edit spoofed as read-only denied              PASS
strict requires all five capabilities         PASS
Pi/OpenCode transport loss keeps cached lock  CONTRACT PASS
terminal state distinguished from outage      CONTRACT PASS
Pi steering failure remains intercepted       CONTRACT PASS
Pi ineffective before_agent_start removed     PASS
Pi 0.73.1 dependencies pinned                 PASS
OpenCode floating next removed                PASS
OpenCode user shell hook present              PASS
OpenCode integration marked guarded/unverified PASS
host MCP operations covered                   PASS
host.confirm over MCP detached                PASS
daemon restart preserves host binding         PASS
```

## Ограничения проверки

- Фактический macOS-runner недоступен. Переносимый `EvalSymlinks` regression test и Linux-набор проходят; macOS PASS не заявляется.
- Реальные Pi/OpenCode binaries с авторизованными моделями не запускались. Pi extension компилируется против зафиксированных типов `0.73.1`; OpenCode V2 остаётся beta и помечен `verified:false`.
- Bundled Pi/OpenCode extensions обеспечивают `guarded`, а не `strict`: подтверждённого fail-closed completion hook для Pi нет, а отмена model dispatch через OpenCode V2 context hook требует live smoke на точной корпоративной сборке.
- Safe pause не прерывает уже выполняющийся provider/tool call. Recovery создаёт новую attempt и не гарантирует exactly-once для внешних побочных эффектов.
- Desktop notification sink зависит от системной утилиты ОС; durable inbox остаётся источником истины доставки.

## Версии

```text
Takt                              0.1.37-alpha
code profile                      0.12.0
Takt skill                        0.19.0
MCP tools                         48
```

## Проверка поставки

После формирования итогового ZIP он распаковывается в чистый каталог. Из поставки повторно проверяются `MANIFEST.sha256`, полный `go test ./...`, Autonomous Run Operations, Coding Agent Host Control, TypeScript-контракт, MCP, daemon и документация.
