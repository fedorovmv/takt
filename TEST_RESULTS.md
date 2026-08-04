# Результаты проверки v0.1.16-alpha

## Семантика validation envelope

Проверено:

- доступный `takt-validation/v1alpha1` декодируется независимо от exit code и terminal status quality-node;
- `valid: false` с exit code 1 сохраняет `score`, `checks` и diagnostics;
- score и diagnostics невалидного результата участвуют в `average_score` и диагностических агрегатах;
- `valid: true` из failed quality-node сохраняется в отчёте, но не повышает `success_at_1` и `final_success_rate`;
- успешным считается только сочетание `quality_node_status = completed` и `quality.valid = true`;
- malformed envelope с ненулевым exit code классифицируется как `quality_contract`;
- отсутствие envelope у completed quality-node классифицируется как ошибка измерительного контура;
- отсутствие envelope у failure-like quality-node сохраняется как невалидный результат с `quality_error`;
- evaluation report сериализует `quality_node_status`.

## Регрессии benchmark

Повторно проверено:

- явные нулевые показатели и `null` для недоступных средних;
- per-attempt execution identity и раздельная атрибуция usage;
- mixed execution identity;
- fingerprints стратегии, набора, workspace и валидатора;
- уникальность `case_id` и запрет пересечения template/output;
- Route DSL feedback, retry/resume, artifacts и approval;
- Pi `agent_settled`, cumulative usage delta, timeout/cancel и output limits.

## Команды

```text
gofmt -w cmd internal
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./cmd/takt
go build ./cmd/takt-fake-assistant
go build ./cmd/takt-fake-pi
./scripts/test-fake-assistant.sh
./scripts/test-pi-adapter.sh
./scripts/test-route-dsl-e2e.sh
./scripts/test-route-dsl-eval.sh
./scripts/check-docs.sh
./scripts/verify.sh
```

Все проверки прошли на рабочем дереве. После сборки релиза тот же набор повторно выполняется из чистой распаковки вместе с проверкой `MANIFEST.sha256`.

Реальный Pi smoke и реальный Route DSL benchmark для `v0.1.16-alpha` в среде сборки не запускались: отсутствуют Pi, пользовательская авторизация, модель и штатный предметный валидатор.
