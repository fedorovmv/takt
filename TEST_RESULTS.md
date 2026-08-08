# Test results — v0.1.46-alpha

Дата: 2026-08-08.

## Release scope

`v0.1.46-alpha — Task-level Dynamic Evaluation & Security Closure`:

- закрывает обходы persistence redaction на control/external worker paths;
- редактирует approval, external assistant/tool/result payloads и domain receipts;
- external text artifacts редактируются до записи, non-text artifact с known secret отклоняется;
- templated `secret://ENV_NAME` регистрируется после render adapter env;
- добавляет `takt eval task-benchmark` поверх настоящего Task Router / Dynamic Plan / checkpoint replan path;
- закрывает замечания evaluation v0.1.45 по schemas/verify/repeat/stability/cost/time-to-valid/gates/immediate retry fingerprint;
- актуализирует README, status, roadmap, implementation plan и backlog.

## Обычный Go gate

На рабочей копии успешно выполнены:

```text
gofmt check                 PASS
go vet ./...                PASS
go test ./... -count=1      PASS — 33 пакета с тестами
go build ./...              PASS
scripts/check-docs.sh       PASS
JSON parse schemas/*.json   PASS — 27 schemas
```

## Race detector

Изменённые Go-пакеты прогнаны одним завершившимся race-набором:

```text
go test -race \
  ./internal/redact \
  ./internal/assistant \
  ./internal/control \
  ./internal/diagnostic \
  ./internal/evaluation \
  ./internal/localsandbox \
  ./internal/runtime \
  ./cmd/takt -count=1       PASS
```

`./scripts/verify.sh` также был запущен целиком. Обычные `go vet` и `go test ./...` внутри него завершились успешно, после чего агрегированный `go test -race ./...` был остановлен внешним timeout инструментальной среды без видимого test FAIL. Поэтому непрерывный полный `go test -race ./...` как PASS не заявляется; для изменённых пакетов используется завершившийся race-набор выше.

## Security regressions v0.1.44

Прямыми тестами подтверждено:

- external tool input/output/result и assistant events проходят общий redactor до append-only persistence;
- external textual artifact сохраняется уже redacted;
- external binary artifact с known secret отклоняется fail-closed;
- `Service.Answer` не сохраняет known secret в durable approval state;
- `DomainOperationState.Receipt` входит в общий state redaction;
- explicit SecretRef, появившийся после request/template rendering assistant env, регистрируется перед adapter execution;
- runtime/public state после выполнения читается из durable redacted copy;
- `test-adapter-platform.sh` использует `python3` с fallback на `python`;
- optional sandbox test не зависит от `/bin/true`;
- `sandbox-exec` profile проверяется кроссплатформенным unit-тестом;
- macOS-only executable sandbox test запускается автоматически на macOS, когда `sandbox-exec` доступен;
- degraded sandbox decision проверяется после повторного чтения durable state;
- after-hook не обходит `sandbox.enforcement: required`.

Текущая инструментальная среда — не macOS, поэтому macOS-only executable `sandbox-exec` test здесь не исполнялся. Релиз не заявляет фактический macOS CI-run: `.github/workflows/ci.yml` содержит матрицу, но наличие файла не является доказательством запуска конкретной локальной поставки.

Дополнительно закрыты прежние покрывные пробелы:

```text
restart-like pause → new Runner → resume keeps persisted not_before   PASS
different diagnostics → different fingerprints                       PASS
fan-out short-circuit cancel_reason=fanout_result_decided              PASS
NodeState.path + event data.node_path durable                           PASS
```

## Evaluation regressions v0.1.45

Подтверждено:

- четыре workflow-evaluation schemas зарегистрированы в `schemas/README.md`;
- Route DSL strategy benchmark и новый task benchmark включены в `scripts/verify.sh`;
- explicit `benchmark.repeat: 0` отклоняется и обычным, и task matrix;
- matrix/compare report schemas типизируют основные records вместо голых objects;
- stable-valid / stable-invalid / unstable aggregation имеет прямой unit test;
- failed-execution cost имеет точный unit assert;
- `time_to_valid_ms` проверяется на точном durable event timestamp;
- gate failure имеет unit + E2E coverage и сохраняет report до non-zero;
- immediate `node.retry` сохраняет diagnostic fingerprint так же, как delayed retry.

## Task-level Dynamic Takt benchmark

`scripts/test-task-evaluation.sh` использует настоящий builtin `code` profile и настоящий `control.Plan → ExecutePlan` path с deterministic fake coding-agent.

Cases:

```text
ordinary task          → template
fixture dynamic audit  → dynamic
fixture dynamic replan → dynamic + replace_remaining + plan revision 2
```

Сравниваются две workspace strategies:

```text
force-template    baseline: Router принудительно выбирает simple-reliable
semantic-router   candidate: обычный semantic Router
```

Contract assertions:

```text
baseline route accuracy     1/3
candidate route accuracy    3/3
candidate_only_route_correct 2
replan expectation          PASS
candidate replan revision   2
task gates                  PASS
```

Обе стратегии могут завершить выполнение успешно, поэтому benchmark отдельно показывает route correctness и terminal success. Это доказывает измерительный контракт, но не является оценкой качества реальной модели.

## Contract / E2E scripts

Каждый из следующих скриптов завершён отдельно с PASS:

```text
test-fake-assistant.sh
test-pi-adapter.sh
test-opencode-adapter.sh
test-route-dsl-e2e.sh
test-route-dsl-eval.sh
test-composition.sh
test-takt-skill.sh
test-code-profile.sh
test-worktree.sh
test-child-runs.sh
test-policies.sh
test-child-fanout.sh
test-script-artifacts.sh
test-mcp.sh
test-external-executor.sh
test-deep-code-workflows.sh
test-authoring.sh
test-daemon.sh
test-dynamic-takt.sh
test-block-packages.sh
test-host-control.sh
test-host-integrations-typescript.sh
test-autonomous-runs.sh
test-simple-reliable-router.sh
test-evidence-routing.sh
test-adapter-platform.sh
test-package-distribution.sh
test-multi-repo.sh
test-runtime-reliability-security.sh
test-route-dsl-benchmark.sh
test-task-evaluation.sh
go test ./sdk/agentadapter -count=1
```

Также повторно валидированы shipped examples Route DSL, hook-retry, Pi/OpenCode smoke, composition, external executor и authoring-daemon.

## Production evidence boundary

Релиз **не подменяет** реальный Route DSL benchmark synthetic fixture-ом. В репозитории есть measurement infrastructure и production-shaped corpus, но live quality numbers требуют:

- реальный обезличенный corpus;
- штатный корпоративный `route-tool`/validator;
- фактические model configurations;
- несколько повторов на одинаковых fingerprints.

Это остаётся первым внешним P0 gate нового roadmap.

## Clean archive verification

Первый release ZIP был собран без `bin/`, распакован в новый каталог и проверен как поставка:

```text
MANIFEST.sha256                         546 files — PASS
bin/                                    absent
gofmt                                   PASS
go vet ./...                            PASS
go test ./... -count=1                  PASS
go build ./...                          PASS
27 JSON schemas                         PASS
scripts/check-docs.sh                   PASS
test-runtime-reliability-security.sh    PASS
test-adapter-platform.sh                PASS
test-route-dsl-benchmark.sh             PASS
test-task-evaluation.sh                 PASS
changed-package race set                PASS
```

После этой проверки в поставке меняется только данный release report и пересчитывается `MANIFEST.sha256`; исходный код, schemas и contract scripts не меняются. Финальный ZIP повторно проверяется по manifest после пересборки.
