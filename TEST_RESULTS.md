# Test results — v0.1.47-alpha

Дата: 2026-08-08.

Среда этого прогона: Linux amd64, Go 1.23.2. Фактический macOS-прогон этой поставки в текущей среде не выполнялся; замечания пользователя к v0.1.46 были получены из отдельного macOS review и закрыты кодом/регрессиями здесь.

## Release scope

`v0.1.47-alpha — v0.2 Stabilization: Iteration History & Contract Audit`:

- начинает стабилизационный этап перед `v0.2`, не вводя новый runtime и не объявляя `v1beta1`;
- добавляет bounded first-class `loop_iterations[]` со всеми завершёнными итерациями и сохраняет `loop_previous` как compatibility alias последней итерации;
- ограничивает `loop_group.max_iterations` диапазоном `1..64` и фиксирует решение оставить nested `loop_group` запрещённым в `v0.2`;
- классифицирует внешние contracts как `stable-candidate | supported-alpha | deprecated | internal` и фиксирует draft migration policy `v1alpha1 → v1beta1`;
- закрывает остаточные redaction gaps v0.1.46: фактический Run/Plan config path, evaluation approval, validation output report и control/WorkflowPlan persistence;
- выравнивает task-evaluation semantics и ужесточает report schemas;
- актуализирует README, status, roadmap, implementation plan, backlog, target-v0.2 и authoring skill.

## Обычный Go gate

На рабочей копии успешно выполнены:

```text
gofmt -w cmd internal sdk      PASS
go vet ./...                   PASS
go test ./... -count=1         PASS — 33 пакета с тестами
go build ./...                 PASS
scripts/check-docs.sh          PASS
JSON Schema Draft 2020-12     PASS — 27 schemas
```

## Race detector

Все реально изменённые runtime/state/control пакеты завершились под race detector:

```text
./internal/control             PASS
./internal/evaluation          PASS
./internal/redact              PASS
./internal/runtime             PASS
./internal/store               PASS
./internal/workflow            PASS
```

Агрегированный `go test -race ./... -count=1` был также запущен. Он успел завершить пакеты от `cmd/takt` через `internal/rolecontract` без test FAIL, после чего был остановлен внешним timeout инструментальной среды. Оставшийся хвост был прогнан отдельными завершившимися командами:

```text
./internal/runtime             PASS
./internal/store               PASS
./internal/taskroute           PASS
./internal/validation          PASS
./internal/workflow            PASS
./internal/workspacecatalog    PASS
./internal/yamlmini            PASS
./sdk/agentadapter             PASS
./sdk/domainadapter            PASS
```

Дополнительная попытка `go test -race -p 1 ./...` также была остановлена внешним timeout без test FAIL. Поэтому один непрерывный aggregate race PASS не заявляется; race-покрытие подтверждено завершившимися package-level прогонами и частично завершившимся aggregate-run.

## v0.1.46 review fixes

Прямыми регрессиями и code-path проверками подтверждено:

- control redactor строится из `RunState.config_path`, а не service default config;
- per-run/profile-resolved config SecretRef защищается даже при неэвристическом имени env;
- evaluation approval path коммитит redacted clone, не live RunState;
- prior Output/Stdout/Stderr не возвращаются на диск в сыром виде через evaluation approval;
- task `min_plan_revisions: 1` означает initial plan; replan expectation начинается с `2`;
- `needs_input` вычисляется после durable plan reload;
- `final_success=true` не сохраняется одновременно с execution error;
- task benchmark `repeat: 2` проверяется структурированным Go assertion helper;
- workspace fingerprint и copy используют один runtime-directory exclusion contract;
- validation report через `node.output_path` редактируется до записи;
- production Run-state commits в control используют общий `commitRedacted`;
- production `WorkflowPlan` writes из control используют run-specific redacted persistence helper;
- task/evaluation report schemas типизируют nested summaries/runs/comparisons; execution-identity usage имеет строгий schema record.

## First-class iteration history

Новый durable state для `loop_group`:

```text
loop_iterations[]   все завершённые snapshots
loop_previous       совместимый snapshot последней итерации
max_iterations      1..64
```

`scripts/test-iteration-history.sh` подтверждает:

```text
iteration 1          satisfied=false
iteration 2          satisfied=false
iteration 3          satisfied=true
reload               сохраняет всю историю
public view           скрывает internal expanded IDs
loop_previous         совпадает с последней итерацией
max_iterations=65     fail-closed
```

Unit/regression tests дополнительно проверяют redaction вложенных iteration nodes и JSON-schema state contract.

## Contract audit / v0.2 decision

`docs/61-v0.2-stabilization-iteration-history-v0.1.47.md` фиксирует:

- stable-candidate: Workflow/base node semantics, Config/Profile/BlockPackage, durable lifecycle/events/artifacts, public five `takt.task.*`, neutral domain operations, package lock/integrity semantics;
- supported-alpha: TaskRoute/WorkflowPlan, evaluation formats, Adapter SDK, host-control integrations и advanced MCP surfaces;
- deprecated: `takt-assistant/v1alpha1` для новых wrappers, при сохранении read compatibility;
- internal: `.takt` storage layout, expanded IDs, commit protocol и fake fixtures;
- nested `loop_group` остаётся запрещённым в v0.2;
- `v1beta1` не замораживается до production evidence.

## Contract / E2E scripts

Следующие сценарии завершились PASS как отдельные прогоны:

```text
test-fake-assistant.sh
test-pi-adapter.sh
OpenCode adapter subtests (normal/race/runtime) separately PASS
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
test-iteration-history.sh
test-route-dsl-benchmark.sh
test-task-evaluation.sh
go test ./sdk/agentadapter -count=1
scripts/check-docs.sh
```

`test-opencode-adapter.sh` как одна длинная shell-команда был остановлен внешней оболочкой после первой успешной подпроверки; его оставшиеся normal/race/runtime Go-команды были затем запущены отдельно и завершились PASS. Аналогично длинные группы contract scripts иногда достигали общего timeout уже после нескольких PASS; оставшиеся скрипты запускались отдельно.

Также повторно валидированы shipped examples: Route DSL, hook-retry, Pi/OpenCode smoke, composition, external executor и authoring-daemon.

## macOS boundary

В проекте сохраняются macOS regressions из v0.1.44/v0.1.46: symlink-aware workspace paths, `python3` fallback, `sandbox-exec` profile и executable fail-closed test при доступном backend. Текущая release verification выполняется на Linux, поэтому наличие этих тестов не объявляется фактическим macOS-run этой версии.

## Production evidence boundary

`v0.1.47` сознательно не подменяет следующий P0 gate synthetic workload-ом. Для live Route DSL evidence по-прежнему требуются:

- реальный обезличенный corpus;
- штатный Route DSL validator;
- фактические model/agent configurations;
- несколько повторов на одинаковых fingerprints.

До этого `v1beta1` остаётся draft target, а не замороженным контрактом.

## Clean archive verification

Первый release ZIP был собран без `bin/`, распакован в новый каталог и проверен как поставка:

```text
MANIFEST.sha256                         548 files — PASS
bin/                                    absent
VERSION                                 0.1.47-alpha
skills/takt/VERSION                     0.29.0
gofmt                                   PASS
go vet ./...                            PASS
go test ./... -count=1                  PASS
go build ./...                          PASS
27 JSON schemas Draft 2020-12          PASS
scripts/check-docs.sh                   PASS
test-runtime-reliability-security.sh    PASS
test-iteration-history.sh               PASS
test-task-evaluation.sh                 PASS
test-route-dsl-benchmark.sh             PASS
test-adapter-platform.sh                PASS
changed-package race set                PASS
```

При race-проверке распакованного ZIP длинная объединённая команда пакетов была остановлена внешним timeout уже после PASS `control/evaluation/redact`; `runtime`, `store` и `workflow` затем завершились отдельным PASS.

После этой clean verification меняется только данный release report. `MANIFEST.sha256` и ZIP пересчитываются; финальный архив повторно проверяется по manifest, без повторного изменения исходного кода.
