# Результаты проверок — v0.1.36-alpha

Дата: 2026-08-06

## Реализованный срез

- Coding Agent Host Control для Pi/OpenCode;
- durable host sessions и уровни `advisory|guarded|strict`;
- CLI/MCP/daemon begin/confirm/get/find/tool guard/completion guard/release;
- перехват `/takt` и последующего input до основной LLM;
- блокировка mutating tools и premature final основной сессии;
- восстановление managed mode после перезапуска хоста;
- транзитивный fingerprint доверенных пакетов;
- исправления замечаний ревью v0.1.35-alpha.

## Go

```text
gofmt cmd/internal                             PASS
go vet ./...                                  PASS
go test ./... -count=1                        PASS
race: cmd, assistant, authoring, catalog,
      command, config, control, daemon,
      definition, dynamicplan, evaluation,
      execution, gitworktree, MCP, profile    PASS
race: runtime, store, validation,
      workflow, yamlmini                       PASS
```

Монолитный race-запуск был разделён из-за внешнего лимита выполнения команды. Все пакеты с тестами прошли race-проверку в отдельных группах.

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
node capability policy                        PASS
governed child fan-out                        PASS
script and typed artifacts                    PASS
local MCP control plane (36 tools)             PASS
external node executor                        PASS
deep code workflows                           PASS
authoring                                     PASS
Dynamic Takt                                  PASS
trusted block packages                        PASS
Coding Agent Host Control                     PASS
host integrations TypeScript structure        PASS
documentation                                 PASS
Route DSL end-to-end                          PASS
Route DSL evaluation + isolation              PASS
local daemon                                  PASS ×4
```

## Замечания v0.1.35-alpha

```text
B1 macOS test canonical expected path          PASS (unit regression)
B2 transitive package fingerprint              PASS
S1 explicit empty allowed_integrations         PASS
S2 zero-limit semantics documented             PASS
S3 steering revision threshold running/waiting PASS
S4 foreground read under advance lock          PASS
S5 no-block_packages migration note            PASS
M1 exact governed-child rejection test         PASS
M2 advisory governance fields documented       PASS
M3 planner ListWorkflows/Marshal errors         PASS
M4 promote rollback errors                     PASS
M5 catalog cache explicitly deferred           PASS
M6 Detached transport contract documented      PASS
M7 takt block --json default documented        PASS
M8 obsolete taste.md remains absent             PASS
```

## Host-control assertions

```text
strict without five host capabilities          REJECTED
strict durable session recovery                PASS
edit spoofed as read-only                      DENIED
known grep/read inspection                     ALLOWED
final while preview/running                    DENIED
final after completed                          ALLOWED
normal active input routed to steering         CONTRACT PASS
main model dispatch bypassed by host hooks      CONTRACT PASS
```

## Ограничения проверки

- Фактический macOS-runner недоступен. Исправлен непереносимый unit test, а Linux regression suite проходит; macOS PASS не заявляется.
- Реальные Pi/OpenCode binaries и авторизованные модели в этой среде недоступны. Расширения прошли TypeScript structural contract и Go/CLI end-to-end; перед корпоративным внедрением требуется smoke на зафиксированных версиях хостов.
- OpenCode V2 plugin API имеет beta-статус, поэтому его версию требуется фиксировать.

## Версии

```text
Takt                              0.1.36-alpha
code profile                      0.12.0
Takt skill                        0.18.0
MCP tools                         36
```

## Проверка поставки

Итоговый ZIP распакован в чистый каталог. Из него прошли `sha256sum -c MANIFEST.sha256`, полный `go test ./...`, Coding Agent Host Control, TypeScript structural contract, Dynamic Takt, trusted packages, MCP, daemon и documentation checks.
