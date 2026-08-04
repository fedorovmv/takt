# Результаты проверки v0.1.11-alpha

## Pi overflow coverage

Проверены два уровня контракта:

- fake Pi создаёт реальный concurrent output overflow внутри `Pi.Run`;
- timeout сохраняет `KindTimedOut` и `Result.Truncated=true`;
- cancellation сохраняет `KindCancelled` и `Result.Truncated=true`;
- runtime переносит эти результаты в `NodeState` со статусами `timed_out`/`cancelled` и `output_truncated=true`;
- прежние protocol-only overflow и cumulative usage сценарии продолжают проходить.

## Route DSL end-to-end

`scripts/test-route-dsl-e2e.sh` проверяет:

- запуск Pi assistant через RPC adapter;
- намеренно невалидный `route.yaml` на первой попытке;
- diagnostics валидатора в `${feedback}`;
- вторую попытку с тем же Session ID;
- успешную обязательную проверку только после исправления;
- сохранение `route.yaml` и `validation.json` в artifacts;
- остановку на approval;
- продолжение через `takt answer` до статуса `completed`.

## Команды

```text
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./cmd/takt
go build ./cmd/takt-fake-assistant
go build ./cmd/takt-fake-pi
./scripts/test-fake-assistant.sh
./scripts/test-pi-adapter.sh
./scripts/test-route-dsl-e2e.sh
./scripts/check-docs.sh
./scripts/verify.sh
```

Реальный Pi smoke с `aihub/Qwen/Qwen3.6-27B` был подтверждён внешним аудитом предыдущего релиза. В среде сборки v0.1.11-alpha он повторно не запускался, поскольку доступ к пользовательской авторизации и модели отсутствует.
