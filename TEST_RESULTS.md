# Результаты проверки v0.1.17-alpha

## AGENTS.md

Проверено:

- корневой `AGENTS.md` присутствует в релизе;
- инструкция фиксирует границы Takt, runtime/protocol/benchmark-инварианты и порядок изменения контрактов;
- `README.md`, `DEVELOPMENT.md`, карта документов и проверка документации ссылаются на инструкцию.

## Разделение stdout и stderr

Проверено:

- bash runtime сохраняет stdout и stderr отдельно;
- совместимое поле `output` сохраняет объединённое диагностическое представление;
- assistant и approval output записываются как логический stdout;
- quality-node декодирует `takt-validation/v1alpha1` только из stdout;
- корректный `valid:false` в stdout вместе с произвольным stderr и exit code 1 сохраняет score и diagnostics;
- stderr не вызывает `quality_contract` и остаётся доступным отдельно и в `diagnostic_output`;
- схемы Run State и evaluation report содержат `stdout` и `stderr`.

## Регрессии

Повторно проверено:

- process и Pi contract suites;
- timeout/cancel/output overflow;
- Pi `agent_settled`, fresh/resume и usage delta;
- Route DSL feedback, retry/resume, artifacts и approval;
- evaluation isolation, fingerprints и предметные метрики;
- per-attempt execution identity и раздельная атрибуция usage;
- validation envelope при любом terminal status и success gate `completed && valid=true`;
- явные нулевые показатели и `null` для недоступных средних значений.

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

Все проверки прошли на рабочем дереве. Тот же набор повторно выполняется из чистой распаковки релиза вместе с проверкой `MANIFEST.sha256`.

Реальный Pi smoke и реальный Route DSL benchmark для `v0.1.17-alpha` в среде сборки не запускались: отсутствуют Pi, пользовательская авторизация, модель и штатный предметный валидатор.
