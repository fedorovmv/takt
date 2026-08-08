# Test results — v0.1.48-alpha

Дата: 2026-08-08.

Среда этого прогона: Linux amd64, Go 1.23.2. Фактический macOS-прогон этой поставки в текущей среде не выполнялся; macOS-specific regressions остаются в suite, но не выдаются за live macOS verification.

## Release scope

`v0.1.48-alpha — Contract Convergence & Compatibility Matrix` продолжает стабилизацию `v0.2` без нового scheduler/runtime:

- фиксирует общий structured JSON contract `takt-schema-subset/v1` для `input.schema` и `output_format` вместо неявного обещания полного JSON Schema;
- выносит validator schema subset в общий `internal/schemasubset`, используемый workflow authoring и runtime;
- добавляет `takt compatibility matrix|fields|schema|check`;
- разделяет session adapter, coding-agent host integration и domain adapter compatibility;
- добавляет optional live version/Describe probes, не смешивая version detection с conformance;
- добавляет machine-readable field-by-field audit будущей границы `v1beta1`;
- формально оставляет `takt-assistant/v1alpha1` read-compatible/deprecated для новых wrappers, целевой process protocol — `v1alpha2`;
- обновляет README, roadmap, implementation status/plan, backlog, specification, runtime semantics, assistant adapter spec, schema registry и authoring skill.

## Schema subset decision

`input.schema` и `output_format` используют один контракт:

```text
takt-schema-subset/v1
```

Поддерживаются типы `object|array|string|number|integer|boolean` и ограниченный список keywords, опубликованный через:

```bash
takt compatibility schema
```

В `v0.2` не заявляется произвольный JSON Schema. `$ref`, `$defs`, combinators (`oneOf/anyOf/allOf`), `const/default/format`, conditional schemas и schema-valued `additionalProperties` не входят в этот контракт. Расширение возможно только явно и по production evidence.

## Compatibility contract

Новые команды:

```text
takt compatibility matrix
takt compatibility fields
takt compatibility schema
takt compatibility check [--live] [--strict]
```

Матрица отдельно описывает:

- assistant session adapters;
- coding-agent host integrations;
- domain adapters;
- MCP surfaces.

`--live` выполняет доступный probe версии Pi/OpenCode и `Describe()` domain adapter. Версия бинарника не переводит host integration в `strict`: bundled Pi/OpenCode host integrations остаются `guarded` до live conformance на конкретной pinned версии.

`--strict` делает warning/error ненулевым результатом для CI/preflight.

## v1beta1 field audit

`takt compatibility fields` публикует field-level решения `keep|migrate-value|defer` для stable-candidate authoring/config contracts. Contract-test отражением проверяет точный набор JSON-полей: новое публичное поле требует явного обновления audit.

Ключевые решения:

- Workflow/base Node/Config/BlockPackage/Policy/WorkflowRunSpec/OutputFormat — stable-candidate;
- `apiVersion` — будущий `migrate-value`, v0.2 продолжает читать `takt/v1alpha1`;
- `Node.executor`, `Node.native_hooks`, `Node.tool_approval` — supported-alpha/defer;
- nested `AssistantSpec` и `DomainAdapterSpec` остаются alpha seams;
- `OutputFormat` замораживается как `takt-schema-subset/v1`, а не arbitrary JSON Schema.

## Ordinary Go gate

На рабочей копии завершились:

```text
gofmt                          PASS
go vet ./...                   PASS
go test ./... -count=1         PASS — 33 пакета с тестами
go build ./...                 PASS
scripts/check-docs.sh          PASS
JSON Schema Draft 2020-12      PASS — 31 schema
```

Дополнительно реальные JSON-ответы CLI `compatibility matrix`, `compatibility fields` и `compatibility check` были провалидированы соответствующими опубликованными Draft 2020-12 schemas. Все schemas автономны для offline validation; runtime `$ref` между compatibility schema-файлами отсутствует.

## Race detector

Изменённый контур завершился под race detector:

```text
./internal/schemasubset        PASS
./internal/compatibility       PASS
./internal/assistant           PASS
./internal/workflow            PASS
./internal/runtime             PASS
./cmd/takt                     PASS
```

Первый объединённый запуск этого набора был остановлен внешним timeout во время `internal/runtime`; повторный `go test -race ./internal/runtime -count=1 -timeout 120s -v` завершил весь пакет PASS за ~17 секунд. `cmd/takt` затем отдельно завершился PASS.

Агрегированный `go test -race ./... -count=1 -timeout 180s` также запускался. Он завершил пакеты от `cmd/takt` через `internal/rolecontract` без test FAIL, после чего был остановлен внешним 10-минутным timeout инструментальной среды. Поэтому один непрерывный aggregate race PASS не заявляется.

## Compatibility tests

Прямые regression tests подтверждают:

- matrix разделяет Pi session adapter и Pi host integration;
- process `v1alpha1` помечается deprecated;
- raw process mode даёт legacy warning;
- `v1alpha2` capability declaration сохраняется;
- `--live` Pi version probe получает synthetic `9.8.7`, но host остаётся `guarded`, `live_verified=false`, `strict_allowed=false`;
- live domain adapter check использует настоящий `Describe()` и сверяет configured operations/reconcile;
- `--strict` отклоняет deprecated/legacy warning;
- field audit покрывает точный набор публичных полей и удерживает external seams в supported-alpha/defer.

`scripts/test-compatibility.sh`: PASS.

## Contract / E2E regressions

Отдельными завершившимися прогонами подтверждены:

```text
test-fake-assistant.sh                  PASS
test-pi-adapter.sh                      PASS
OpenCode normal/race/runtime subtests   PASS
test-route-dsl-e2e.sh                   PASS
test-route-dsl-eval.sh                  PASS
test-composition.sh                     PASS
test-takt-skill.sh                      PASS
test-code-profile.sh                    PASS
test-worktree.sh                        PASS
test-child-runs.sh                      PASS
test-policies.sh                        PASS
test-child-fanout.sh                    PASS
test-script-artifacts.sh                PASS
test-mcp.sh                             PASS
test-external-executor.sh               PASS
test-deep-code-workflows.sh             PASS
test-authoring.sh                       PASS
test-daemon.sh                          PASS
test-dynamic-takt.sh                    PASS
test-block-packages.sh                  PASS
test-host-control.sh                    PASS
test-host-integrations-typescript.sh    PASS
test-autonomous-runs.sh                 PASS
test-simple-reliable-router.sh          PASS
test-evidence-routing.sh                PASS
test-adapter-platform.sh                PASS
test-package-distribution.sh            PASS
test-multi-repo.sh                      PASS
test-runtime-reliability-security.sh    PASS
test-iteration-history.sh               PASS
test-compatibility.sh                    PASS
test-route-dsl-benchmark.sh             PASS
test-task-evaluation.sh                 PASS
go test ./sdk/agentadapter -count=1     PASS
scripts/check-docs.sh                    PASS
```

`test-opencode-adapter.sh` как один длинный shell invocation снова был остановлен внешней оболочкой после первой успешной подпроверки. Те же оставшиеся normal/race/runtime Go-команды были затем запущены отдельно и завершились PASS.

Один ранний `test-policies.sh` был ошибочно запущен до сборки `bin/takt-fake-assistant` и закономерно получил `no such file or directory`; после сборки binaries в том же порядке, что `scripts/verify.sh`, сам policy contract завершился PASS. Это не считается продуктовым test failure.

## Production evidence boundary

`v0.1.48` не повышает synthetic contract fixtures до production compatibility. До финальной заморозки `v1beta1` остаются внешние gates:

- live Route DSL corpus + штатный validator + реальные models;
- production-like Go/Document evaluations;
- live Pi/OpenCode host conformance на pinned versions;
- хотя бы один production external session wrapper и один reference domain adapter.

## Clean archive verification

Первый release ZIP был собран без `bin/`, распакован в новый каталог и проверен как фактическая поставка:

```text
MANIFEST.sha256                         560 files — PASS
bin/                                    absent
VERSION                                 0.1.48-alpha
skills/takt/VERSION                     0.30.0
gofmt                                   PASS
go vet ./...                            PASS
go test ./... -count=1                  PASS
go build ./...                          PASS
31 JSON schemas Draft 2020-12          PASS
scripts/test-compatibility.sh           PASS
scripts/check-docs.sh                   PASS
test-runtime-reliability-security.sh    PASS
test-iteration-history.sh               PASS
test-route-dsl-benchmark.sh             PASS
test-task-evaluation.sh                 PASS
test-adapter-platform.sh                PASS
test-package-distribution.sh            PASS
test-multi-repo.sh                      PASS
```

Clean-archive race verification:

```text
./internal/schemasubset                 PASS
./internal/compatibility                PASS
./internal/assistant                    PASS
./internal/workflow                     PASS
./internal/runtime                      PASS
./cmd/takt                              PASS
```

Первая объединённая clean-race команда была остановлена общим внешним timeout во время assistant после PASS первых двух пакетов; assistant/workflow/runtime/CLI затем были запущены отдельно из той же распаковки и завершились PASS.

После этой clean verification меняется только данный release report. `MANIFEST.sha256` и ZIP пересчитываются; финальный архив повторно проверяется по manifest, без изменения исходного кода.
