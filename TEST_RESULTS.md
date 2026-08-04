# Результаты проверки v0.1.10-alpha

## Исправления Pi RPC

Проверены:

- timeout + output overflow сохраняет `timed_out`;
- cancellation + output overflow сохраняет `cancelled`;
- overflow без завершённого parent context остаётся `protocol`;
- исчезновение cumulative usage после валидного первого снимка возвращает `protocol`;
- явные нулевые cumulative usage-значения дают успешную нулевую дельту;
- прежние `agent_settled`, retry, resume, output-limit и process-contract сценарии.

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
./scripts/check-docs.sh
./scripts/verify.sh
```

Реальный Pi smoke test не запускался: он требует установленного и авторизованного Pi и доступной модели.
