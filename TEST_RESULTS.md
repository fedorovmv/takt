# Результаты проверки v0.1.13-alpha

## Исправления аудита evaluation

Проверено:

- коллизии нормализованных `case_id` отклоняются до создания output;
- `--replace` не может удалить workspace другого задания с тем же нормализованным именем;
- `workspace-template` и `output` не могут совпадать или быть вложены друг в друга;
- проверка учитывает существующие символические ссылки и выполняется до `MkdirAll`;
- `NodeState.resumed` сохраняет подтверждённое продолжение сессии;
- `report.json` содержит resume, feedback, node error и diagnostic output;
- Route DSL eval assertion подтверждает две попытки, resume, `ROUTE_INVALID` и успешный вывод полного валидатора.

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

Реальный Pi smoke с `aihub/Qwen/Qwen3.6-27B` подтверждён внешним аудитом `v0.1.12-alpha` за 4,83 секунды. В среде сборки `v0.1.13-alpha` он повторно не запускался, поскольку пользовательская авторизация и модель недоступны.
