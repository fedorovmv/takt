# Test results — v0.1.45-alpha

Дата: 2026-08-08.

## Release scope

`v0.1.45-alpha — Route DSL Evaluation & Strategy Benchmark` расширяет существующий evaluation runner без изменения scheduler/runtime semantics:

- `EvaluationMatrix` и `CaseManifest`;
- `takt eval benchmark` и `takt eval compare`;
- repeat, парное baseline/candidate сравнение и category breakdown;
- true `time_to_valid_ms`, retry/failed-execution cost и diagnostic fingerprints;
- stable/unstable case aggregation;
- regression gates;
- agent-neutral Route DSL strategy workflows;
- 25-case corpus: 10 regression + 15 production-shaped synthetic cases.

## Go gates

```text
gofmt                        PASS
go vet ./...                 PASS
go test ./... -count=1       PASS
go build ./...               PASS
```

Изменённый измерительный контур отдельно проверен с race detector:

```text
go test -race ./internal/evaluation ./cmd/takt -count=1   PASS
```

Попытка последовательного race-прогона всех пакетов была остановлена внешним лимитом инструментальной среды при `internal/assistant`; тестового FAIL до остановки не было. Поэтому непрерывный `go test -race ./...` как PASS не заявляется.

## Contract / E2E gates

```text
24 JSON schemas                         PASS
scripts/test-route-dsl-eval.sh          PASS
scripts/test-route-dsl-benchmark.sh     PASS
scripts/test-runtime-reliability-security.sh PASS
scripts/test-multi-repo.sh              PASS
scripts/test-package-distribution.sh    PASS
scripts/test-adapter-platform.sh        PASS
scripts/test-takt-skill.sh              PASS
scripts/test-code-profile.sh            PASS
scripts/test-dynamic-takt.sh            PASS
scripts/test-evidence-routing.sh        PASS
make agent-adapter-conformance          PASS
scripts/check-docs.sh                   PASS
```

`test-route-dsl-benchmark.sh` использует fake coding-agent + deterministic Route DSL validator и проверяет фактический comparative path: baseline остаётся invalid, feedback-repair становится valid после bounded feedback/retry; `candidate_only_valid` считается попарно, matrix report открывается через `eval report`, `eval compare` возвращает те же пары, а невозможный gate возвращает non-zero с сохранённым `benchmark.json` и `passed:false`.

## Evaluation contract notes

- `time_to_valid_ms` вычисляется по durable `node.completed` quality-node, а не из амортизированной длительности всего эксперимента.
- Experiment fingerprint не зависит от временного пути matrix-файла; он зависит от benchmark ID, repeat и strategy/benchmark fingerprints.
- CaseManifest входит в benchmark fingerprint.
- `baseline_strategy` обязателен; отрицательные допустимые проценты regression gate отклоняются.
- Matrix gate failure сохраняет полный отчёт до возврата non-zero.
- Новые 15 cases обозначены как `production-shaped`, а не как реальные обезличенные production ТЗ.

## Live benchmark boundary

Live numbers `success@1`, final success, tokens/cost и time-to-valid для реальной модели в этом релизном прогоне не измерялись: в окружении нет штатного корпоративного Route DSL validator и выбранной авторизованной model configuration. `examples/route-dsl-benchmark/run.sh` предназначен для такого следующего запуска и использует тот же versioned matrix/corpus contract.

## Clean archive verification

Первый release ZIP был распакован в чистый каталог и проверен как поставка:

```text
MANIFEST.sha256                         537 files — PASS
bin/                                    absent
gofmt                                   PASS
go vet ./...                            PASS
go test ./... -count=1                  PASS
go build ./...                          PASS
24 JSON schemas                         PASS
Route DSL evaluation                    PASS
Route DSL strategy benchmark            PASS
documentation                           PASS
go test -race ./internal/evaluation     PASS
go test -race ./cmd/takt                PASS
```

После добавления этого раздела в release report финальный ZIP пересобирается заново; код и тестовые артефакты при этом не меняются, а final manifest/ZIP checksum проверяются повторно.
