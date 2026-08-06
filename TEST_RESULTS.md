# Результаты проверок — v0.1.35-alpha

Дата проверки: 2026-08-06.

## Среда

- Linux x86_64, kernel `6.18.35`;
- Go `1.23.2`;
- GNU Bash `5.2.37`;
- Python `3.13.5`;
- Node `22.16.0`;
- временные Git-репозитории и детерминированные fake/process adapters;
- реальный macOS runner в этой среде недоступен.

В CI сохраняется матрица `ubuntu-latest` / `macos-latest`. Фактический macOS PASS в этом отчёте не заявляется.

## Исправления замечаний v0.1.34

| Контракт | Результат |
|---|---|
| `takt execute` без daemon выполняет план в переднем режиме до terminal либо устойчивого `waiting` | PASS |
| `takt execute --daemon` сохраняет отсоединённый режим | PASS |
| underlying Run связывается с планом до освобождения межпроцессной блокировки | PASS |
| daemon и foreground/MCP не могут одновременно создать одинаковый следующий сегмент | PASS |
| `ask_user → steer` соблюдает `max_iterations` | PASS |
| steering помечается применённым только после валидного решения репланировщика | PASS |
| ещё не созданный artifact path под `/var` сопоставляется с существующим `/private/var`-префиксом macOS | PASS, unit regression |
| authoring анализирует каждую часть составного `when` с `&&` и `||` | PASS |
| `max_parallel` ограничивает независимые task-фазы и fan-out | PASS |
| planner, replanner, segment Run и их governed children входят в `max_child_runs` | PASS |
| usage planner/replanner/execution входит в `max_tokens` | PASS |
| `max_tokens: 0` нормализуется в конечный предел и не даёт безлимитный MCP candidate | PASS |
| promote отказывается перезаписывать существующий workflow без `--force` | PASS |
| `reason` обязателен в Go и schema validation | PASS |
| `adversarial-verify` использует отдельный блок | PASS |
| map source проверяется по типизированному output доверенного блока до Run | PASS |
| ошибки вторичного сохранения состояния возвращаются вызывающему коду | PASS |

JSON-RPC ответы MCP-прокси могут возвращаться не в порядке запросов; это разрешено протоколом и зафиксировано в документации.

## Доверенные пакеты корпоративных блоков

| Контракт | Результат |
|---|---|
| профиль явно подключает упорядоченный список `block_packages` | PASS |
| `BlockPackage` проверяется по `schemas/block-package.schema.json` и Go-валидатору | PASS |
| встроенный пакет содержит семь самостоятельных Dynamic Takt блоков | PASS |
| корпоративный пример содержит research, implementation и mandatory validation | PASS |
| пакет объявляет capabilities, integrations и типизированные `output_paths` | PASS |
| governance объединяет обязательные блоки и проверки | PASS |
| лимиты нескольких пакетов сужаются до минимального положительного значения | PASS |
| allowed integrations и разрешающие политики пересекаются | PASS |
| запрещающие политики и требования объединяются | PASS |
| корпоративная политика применяется также к встроенным блокам | PASS |
| конфликтующие branch rules и change-request templates отклоняются | PASS |
| workflow блока не может выйти за каталог пакета | PASS |
| блок имеет ровно один публичный terminal output | PASS |
| governed child Run внутри доверенного блока запрещён | PASS |
| `map` принимает только точно объявленный output типа `array` | PASS |
| план хранит пути пакетов и SHA-256 fingerprint каталога | PASS |
| изменение пакета после preview блокирует execute, replan и promote | PASS |
| preview показывает capabilities, integrations, governance и бюджеты | PASS |
| CLI `takt block validate/list/describe` | PASS |
| MCP `takt.block.list` и `takt.block.describe` | PASS |

Полная доставка пакетов — install/update/uninstall, удалённый реестр, lock-файл, зависимости и подписи — сознательно остаётся следующим самостоятельным направлением.

## Проверки исходного дерева

```text
gofmt                                      PASS
go vet ./...                               PASS
go test ./... -count=1                     PASS
race group A                               PASS
race group B                               PASS
build takt и fake adapters                 PASS
example validation set                     PASS
fake-assistant contracts                   PASS
Pi adapter contracts                       PASS
OpenCode adapter contracts                 PASS
Route DSL end-to-end                       PASS
Route DSL evaluation/isolation             PASS
workflow composition                       PASS
Takt authoring skill                       PASS
code profile 0.11.0                        PASS
Git worktree lifecycle                     PASS
governed child Runs                        PASS
node capability policies                   PASS
governed child fan-out                     PASS
script and typed artifacts                 PASS
local MCP control plane (29 tools)         PASS
external controlled executor               PASS
deep six-workflow Git fixture              PASS
authoring contract                         PASS
daemon contract                            PASS ×4
dynamic Takt contract                      PASS
trusted block package contract             PASS
documentation check                        PASS
```

Все Go-пакеты выполнены с race detector двумя группами без изменения исходного дерева. Монолитный `scripts/verify.sh` как единый запуск не заявляется: в этой среде длинные последовательности несколько раз останавливались внешним лимитом уже после успешно завершённых групп. Каждая его неизменённая команда выполнена отдельно или в меньшей группе.

После финального исправления межпроцессной гонки повторно прошли:

```text
go test ./... -count=1                     PASS
race обеих групп                           PASS
dynamic Takt end-to-end                    PASS
daemon contract                            PASS
trusted block package contract             PASS
local MCP contract                         PASS
```

## Что не проверялось

- фактическая GitHub Actions job на macOS;
- реальные GitHub, GitLab, корпоративные SCM, tracker и CI;
- реальные пользовательские сессии OpenCode и Pi: использовались детерминированные adapters;
- системный OS sandbox и полная redaction секретов;
- crash recovery дочернего OS-процесса после принудительного завершения самого daemon;
- полный менеджер установки и обновления пакетов;
- предметный Route DSL benchmark на обезличенных реальных заданиях.

## Границы версии

- `BlockPackage` является локальным доверенным каталогом, явно подключённым профилем, а не удалённым marketplace;
- семантика пакета не создаёт второй runtime: Dynamic Takt компилирует план в обычный Workflow;
- доверенный блок атомарен относительно бюджета динамического плана и не скрывает governed child Runs;
- проектный и корпоративный пакеты считаются доверенными файлами текущего пользователя;
- domain adapters SCM/tracker/CI остаются следующим крупным продуктовым направлением.

## Проверка поставляемого ZIP

Предварительный релизный ZIP распакован в новый пустой каталог. Непосредственно из распакованной копии прошли:

```text
sha256sum -c MANIFEST.sha256              PASS
VERSION=0.1.35-alpha                      PASS
profile code VERSION=0.11.0               PASS
skill VERSION=0.17.0                      PASS
go test ./... -count=1                     PASS
build takt и fake adapters                 PASS
daemon contract                            PASS
dynamic Takt end-to-end                    PASS
trusted block package contract             PASS
local MCP contract                         PASS
code profile catalog                       PASS
deep six-workflow Git fixture              PASS
external executor                          PASS
authoring contract                         PASS
documentation check                        PASS
```

Длинная объединённая последовательность была остановлена внешним лимитом во время deep-workflows. Deep-workflows и оставшиеся команды затем выполнены отдельно в той же чистой распаковке и завершились успешно.

После добавления этого фактического раздела финальный архив пересобран с новым `MANIFEST.sha256`, распакован ещё раз и проверен по манифесту и версиям. Исходный код после сквозных проверок не изменялся.
