# Результаты проверки v0.1.12-alpha

## Исправления аудита

Проверено:

- `scripts/test-route-dsl-e2e.sh` не вызывает `python` или `python3`;
- JSON-ответы Route E2E проверяются Go helper-ом;
- timeout + overflow проходит через fake Pi и `Pi.Run` с обычным `context.WithTimeout`;
- cancel + overflow проходит через fake Pi и `Pi.Run` с обычным `context.WithCancel`;
- в обоих случаях parent context имеет согласованные `Done()` и `Err()`;
- `Result.Truncated=true` сохраняется вместе с `timed_out`/`cancelled`;
- runtime scheduler переносит статус и `output_truncated=true` в итоговый `NodeState`.

## Usage и evaluation

Проверено:

- usage двух Pi-попыток суммируется в `NodeState.usage`;
- `takt eval run` создаёт отдельную рабочую область для каждого задания;
- два Route DSL задания проходят обязательный validator retry/resume;
- evaluation автоматически отвечает approval только при заданном `--answer`;
- `report.json` содержит status, attempts, duration, tokens, cost, approvals и node details;
- `takt eval report` повторно читает сохранённый отчёт;
- `make check` и `scripts/verify.sh` включают Route DSL eval suite.

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

Реальный Pi smoke с `aihub/Qwen/Qwen3.6-27B` был подтверждён внешним аудитом `v0.1.11-alpha`. В среде сборки `v0.1.12-alpha` он повторно не запускался, поскольку пользовательская авторизация и модель недоступны.
