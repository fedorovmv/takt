# Результаты проверок — v0.1.34-alpha

Дата проверки: 2026-08-06.

## Среда

- Linux x86_64, kernel `6.18.35`;
- Go `1.23.2`;
- GNU Bash `5.2.37`;
- Python `3.13.5`;
- Node `22.16.0`;
- временные Git-репозитории, fake GitHub CLI и детерминированные process adapters;
- реальный macOS runner в этой среде недоступен.

В CI сохраняется матрица `ubuntu-latest` / `macos-latest`. Фактический macOS PASS в этом отчёте не заявляется.

## Исправления daemon и открытых дефектов v0.1.32

| Контракт | Результат |
|---|---|
| длинный стандартный Unix socket получает короткий хэш-путь в `$TMPDIR/takt-daemon/...` | PASS |
| слишком длинный явно заданный socket отклоняется до `bind` с понятной ошибкой | PASS |
| подписка дочитывает журнал до terminal revision и получает `run.completed` | PASS |
| `scripts/test-daemon.sh` выполнен четыре раза подряд | PASS ×4 |
| claimed external worker хранит `claimed_at`; старые claim без activity не используют фиктивные 15 минут | PASS |
| v1alpha2 worker завершается и `wait`-ится при protocol error | PASS |
| governed child/router применяет `ValidateWorkflowInput` | PASS |
| review, approval и recovery-ветви получают явные `when`-условия | PASS |
| review children получают вход родительского процесса | PASS |
| `validation_commands` выполняются последовательно через детерминированный runtime | PASS |
| отчёт команд сохраняется как типизированный JSON-артефакт | PASS |

JSON-RPC ответы MCP-прокси могут возвращаться не в порядке запросов, что допустимо протоколом и зафиксировано в документации.

## Dynamic Takt

| Контракт | Результат |
|---|---|
| решение `existing | planned` через структурированный результат планировщика | PASS |
| ограниченный `WorkflowPlan` и машинная JSON Schema | PASS |
| только разрешённые блоки `discover/investigate/implement/validate/review/adversarial-verify/synthesize` | PASS |
| компиляция сегмента в обычные governed child workflow и fan-out | PASS |
| preview с фазами, capabilities и жёсткими бюджетами | PASS |
| обязательное подтверждение planned-процесса | PASS |
| лимиты child Runs, параллельности, редакций и токенов | PASS |
| перепланирование только в явных checkpoint; завершённые фазы неизменяемы | PASS |
| durable plan revisions, steering и phase results | PASS |
| MCP `takt.plan`, `takt.plan.get`, `takt.execute`, `takt.run.steer`, `takt.plan.promote` | PASS |
| Takt skill и пример skill для Pi/OpenCode | PASS |
| отображение фаз, execution Runs, usage и artifact count | PASS |
| продвижение успешного плана в `.takt/workflows/generated/` | PASS |
| promoted workflow параметризован через `${input}` и проходит `takt validate` | PASS |

Сквозной `scripts/test-dynamic-takt.sh` выполняет реальный пользовательский путь на fake process agent:

```text
natural-language goal
→ planner Run
→ preview + confirmation
→ daemon execution
→ discover worker Run
→ checkpoint + replan
→ synthesize worker Run
→ two typed artifacts
→ completed plan
→ promote + validate
```

## Проверки исходного дерева

```text
gofmt                                      PASS
go vet ./...                               PASS
go test ./... -count=1                     PASS
race: daemon/authoring/runtime/control/
      yamlmini/assistant                   PASS
race: cmd/takt и остальные test packages  PASS
fake-assistant contracts                   PASS
Pi adapter contracts                       PASS
OpenCode adapter contracts                 PASS
Route DSL end-to-end                       PASS
Route DSL evaluation/isolation             PASS
workflow composition                       PASS
Takt authoring skill                       PASS
code profile 0.10.0                        PASS
Git worktree lifecycle                     PASS
governed child Runs                        PASS
node capability policies                   PASS
governed child fan-out                     PASS
script and typed artifacts                 PASS
local MCP control plane (27 tools)         PASS
external controlled executor               PASS
deep six-workflow Git fixture              PASS
authoring contract                         PASS
daemon contract                            PASS ×4
dynamic Takt contract                      PASS
documentation check                        PASS
```

Монолитный `go test -race ./... -count=1` был остановлен внешним лимитом tool-сеанса во время выполнения. Тот же набор тестируемых пакетов затем выполнен двумя race-группами без изменения исходного дерева; обе группы прошли. Полный `scripts/verify.sh` поэтому как единая команда не заявляется.

## Что не проверялось

- фактическая GitHub Actions job на macOS;
- реальные сетевые вызовы GitHub, GitLab или корпоративных систем;
- реальные сессии OpenCode и Pi: использовался детерминированный process adapter;
- Claude и Codex;
- системный OS sandbox, redaction секретов и distributed locking;
- сохранение выполняющегося дочернего OS-процесса после аварийного завершения самого daemon;
- предметный Route DSL benchmark на обезличенных реальных заданиях.

## Границы версии

- планировщик выбирает `existing` или `planned`; простые задачи `direct` остаются решением основной сессии кодинг-агента;
- `WorkflowPlan` является ограниченным планом компиляции, а не вторым runtime или полным вторым DSL;
- replanning изменяет только невыполненный хвост процесса;
- CLI `plan` создаёт preview напрямую, а надёжное фоновое исполнение выполняется через daemon/MCP;
- domain adapters для SCM, tracker и CI остаются следующим крупным направлением.

## Проверка поставляемого ZIP

Предварительный релизный ZIP был распакован в новый пустой каталог. Непосредственно из распакованной копии прошли:

```text
sha256sum -c MANIFEST.sha256              PASS
VERSION=0.1.34-alpha                      PASS
profile code VERSION=0.10.0               PASS
skill VERSION=0.16.0                      PASS
go test ./... -count=1                     PASS
daemon contract                            PASS
dynamic Takt end-to-end                    PASS
local MCP contract                         PASS
code profile catalog                       PASS
deep six-workflow Git fixture              PASS
external executor                          PASS
authoring contract                         PASS
documentation check                        PASS
```

После добавления этого фактического раздела финальный архив пересобран с новым `MANIFEST.sha256`, распакован ещё раз и повторно проверен. После финальной проверки исходные файлы архива не изменялись.
