# Результаты проверки v0.1.9-alpha

## Исправления Pi RPC

Проверены:

- ожидание `agent_settled` после нескольких `agent_end`;
- automatic retry без возврата частичного результата;
- полный deny-list session/mode-флагов Pi;
- fire-and-forget `set_editor_text`;
- per-attempt usage delta для fresh и resume;
- protocol error при уменьшении cumulative stats;
- старые timeout/cancel/output-limit/resume/runtime контракты.

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
