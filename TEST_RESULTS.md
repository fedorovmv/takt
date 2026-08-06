# Результаты проверок — v0.1.32-alpha

Дата проверки: 2026-08-06.

## Среда

- Linux x86_64, kernel `6.18.35`;
- Go `1.23.2`;
- GNU Bash `5.2.37`;
- Python `3.13.5`;
- Node `22.16.0`;
- локальные deterministic adapters и изолированный fake GitHub CLI;
- реальный macOS runner в этой среде недоступен.

В CI сохраняется матрица `ubuntu-latest` / `macos-latest`. Фактический macOS PASS в этом отчёте не заявляется.

## Agent event protocol v2

| Контракт | Результат |
|---|---|
| единые `assistant.session.started` и `assistant.session.resumed` | PASS |
| `tool.requested → allowed/denied → started → completed` | PASS |
| policy проверяется до разрешения и запуска инструмента | PASS |
| обязательное approval блокирует tool call | PASS |
| отдельная отмена до старта и cooperative `cancel_requested` во время работы | PASS |
| внешний узел нельзя завершить с незакрытым tool call | PASS |
| `assistant.artifact.declared` содержит provenance и `call_id` | PASS |
| нормализованные usage/diagnostic/completed/failed events | PASS |
| raw stdout/stderr не заменяются нормализованными событиями | PASS |
| capability declaration adapter/worker и fail-closed `tool_control` | PASS |
| двусторонний process protocol `takt-assistant/v1alpha2` | PASS |
| OpenCode/Pi честно остаются observational adapters | PASS |

## MCP control и worker plane

| Контракт | Результат |
|---|---|
| 22 MCP tools в детерминированном порядке | PASS |
| durable external node, claim token, lease и capability attestation | PASS |
| tool request/decision/start/get/complete/cancel | PASS |
| защищённое объявление артефакта внешним worker | PASS |
| structured completion проходит обычные output/retry/artifact semantics | PASS |
| сквозной stdio MCP-сеанс с blocking approval | PASS |
| преждевременное завершение узла отклоняется | PASS |

## Шесть глубоких workflow

Проверка выполняется на временном настоящем Git-репозитории с bare remote, process adapter и изолированным fake `gh`.

| Сценарий | Результат |
|---|---|
| `fix-github-issue`: intake → root cause → reproduction → plan → implementation → validation → push/PR | PASS |
| `idea-to-pr`: research → typed plan → approval/resume → implementation → validation → PR | PASS |
| `plan-to-pr`: первая validation падает → recovery → revalidation → PR | PASS |
| `smart-pr-review`: intake существующего fake PR → governed reviewers → synthesis | PASS |
| `piv-loop`: exploration → plan approval → implementation → validation/review/acceptance | PASS |
| `ralph-dag`: backlog → story loop → validation → push/PR | PASS |
| checkpoint `blocked` останавливает plan/implementation/validation/PR и не изменяет Git | PASS |
| некорректный JSON input отклоняется до assistant/Git | PASS |
| обязательные типизированные artifacts и domain codes | PASS |
| строгие JSON-входы без неизвестных полей | PASS |

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
code profile 0.9.0                         PASS
Git worktree lifecycle                     PASS
governed child Runs                        PASS
node capability policies                   PASS
governed child fan-out                     PASS
script and typed artifacts                 PASS
local MCP control plane                    PASS
external controlled executor               PASS
deep six-workflow Git fixture              PASS
documentation and examples                 PASS
scripts/verify.sh                           PASS
verification: PASS                         PASS
exit code                                  0
```

Полный `scripts/verify.sh` запущен отдельным непрерывным процессом после окончательных изменений и завершился с кодом `0`. Разбиение тестов из-за ограничения времени одного tool-вызова не использовалось.

## Что не проверялось

- реальные сетевые вызовы GitHub;
- реальные сеансы Claude, Codex, OpenCode и Pi;
- перехват tool call через OpenCode/Pi: их текущие интерфейсы позволяют только наблюдательные события;
- фактическая GitHub Actions job на macOS;
- удалённый worker и сетевой transport;
- продолжение после завершения процесса через `takt daemon`: daemon относится к следующему направлению;
- системный OS sandbox и redaction секретов.

## Проверка поставляемого архива

Первый релизный ZIP распакован в новый пустой каталог. Непосредственно из распакованной копии прошли:

```text
sha256sum -c MANIFEST.sha256              PASS
VERSION=0.1.32-alpha                      PASS
profile code VERSION=0.9.0                PASS
skill VERSION=0.14.0                      PASS
go vet ./...                              PASS
go test ./... -count=1                    PASS
go test -race ./... -count=1              PASS
все adapter/runtime/MCP contracts         PASS
семь deep-workflow Git scenarios          PASS
documentation and examples                PASS
scripts/verify.sh                          PASS
verification: PASS                        PASS
exit code                                 0
```

После добавления этого фактического результата изменились только `TEST_RESULTS.md` и `MANIFEST.sha256`. Финальный ZIP был пересобран детерминированно, распакован в новый чистый каталог и повторно прошёл тот же полный `scripts/verify.sh` с кодом `0`. После этой проверки исходный код и функциональные файлы не изменялись.
