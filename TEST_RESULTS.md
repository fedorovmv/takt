# Результаты проверки v0.1.14-alpha

## Benchmark identity и предметное качество

Проверено:

- `report.json` использует формат `takt-evaluation/v1alpha1`;
- strategy fingerprint объединяет fingerprints workflow, config и используемых Markdown-команд;
- benchmark fingerprint включает упорядоченный набор заданий, копируемый workspace template, quality/generation nodes, протокол качества и fingerprint валидатора;
- изменение файла или каталога валидатора меняет его SHA-256;
- `NodeState` и evaluation report сохраняют assistant, его версию, requested model и фактический Pi `responseModel`;
- Pi version probe переносится в `assistant_version`;
- строгий decoder `takt-validation/v1alpha1` отклоняет неизвестные поля, отсутствующий `valid`, явный `null`, неверные диапазоны, неизвестную severity и второй JSON-объект;
- malformed validator result получает `quality_contract` и останавливает benchmark;
- неуспешный workflow с пропущенным quality node учитывается как предметно невалидный результат, а не теряется из denominator;
- `success_at_1`, `final_success_rate`, score, diagnostics, attempts/cost/time per valid рассчитываются по всем запускам;
- стоимость и время неуспешных заданий входят в стоимость и время одного корректного результата;
- infrastructure fake-Pi suite и реальный Route DSL benchmark разделены.

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
sha256sum -c MANIFEST.sha256
```

Дополнительно сформированный evaluation report проверен локально по `schemas/evaluation-report.schema.json` и `schemas/validation-result.schema.json` валидатором JSON Schema Draft 2020-12.

Реальный benchmark `v0.1.14-alpha` с Pi и штатным Route DSL validator в среде сборки не запускался: отсутствуют бинарник Pi, пользовательская авторизация, доступная модель и предметный валидатор. Успешный внешний Pi smoke предыдущей версии подтверждает transport-контур, но не является baseline качества этого релиза.
