# Результаты проверки v0.1.15-alpha

## Семантика benchmark-метрик

Проверено:

- измеренные нулевые показатели `success_at_1` и `final_success_rate` всегда присутствуют в `report.json` как `0`;
- недоступные средние значения сериализуются как `null`, а не исчезают из отчёта;
- `NodeState.executions` сохраняет отдельную execution-запись для каждого фактического вызова узла;
- каждая запись содержит assistant, версию assistant, requested/resolved model, Session ID, resume, usage и классификацию завершения;
- смена assistant, версии или модели между retry помечает узел `mixed_execution_identity`;
- токены и стоимость распределяются по `summary.usage_by_execution_identity`, а не приписываются последней попытке;
- aggregate `NodeState.usage` остаётся суммой для обратной совместимости;
- `valid: true` учитывается только от quality-node со статусом `completed`;
- stdout неуспешного, прерванного или пропущенного quality-node не повышает показатели качества;
- `benchmark.fingerprint` меняется при изменении validator ID или version;
- неоднозначное поле `duration_per_valid_ms` заменено на `amortized_end_to_end_ms_per_valid`;
- opt-in Pi smoke требует непустой фактический `ResolvedModel`.

## Согласованность схем

Проверено:

- `schemas/run-state.schema.json` описывает `nodes.*.executions`;
- `schemas/evaluation-report.schema.json` требует нулевые счётчики и допускает `null` только для недоступных средних;
- схема содержит `execution`, `usage_by_execution_identity`, mixed identity и новое имя временной метрики;
- старое `duration_per_valid_ms` в текущей схеме отсутствует;
- все JSON Schema синтаксически корректны;
- сформированный Route DSL evaluation report успешно проверен по Draft 2020-12 schema с локальным `validation-result.schema.json`;
- проверка документации требует ADR-027 и документ `29-benchmark-metric-semantics-v0.1.15.md`.

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

Все перечисленные проверки прошли на рабочем дереве, восстановленном из опубликованного `v0.1.14-alpha`, и повторно — после чистой распаковки релизного архива. `MANIFEST.sha256` и версия CLI `takt v0.1.15-alpha` подтверждены отдельно.

Реальный Pi smoke и реальный Route DSL benchmark в среде сборки не запускались: бинарник Pi, пользовательская авторизация, модель и штатный предметный валидатор отсутствуют. Opt-in smoke остаётся в наборе и теперь дополнительно проверяет фактически разрешённую модель.
