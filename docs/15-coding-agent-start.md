# Стартовая инструкция для кодового агента

## Контекст

Takt — Go-runtime для оркестрации готовых кодовых агентов, моделей, детерминированных проверок, hooks, loops и approval. Takt не реализует собственный coding-agent tool loop.

Текущая версия предназначена для локального trusted runtime.

## Перед работой

Прочитай:

1. `docs/12-document-map.md`;
2. `docs/05-implementation-status.md`;
3. `docs/18-audit-remediation-v0.1.4.md`;
4. `docs/17-audit-remediation-v0.1.3.md`;
5. `docs/16-audit-remediation-v0.1.2.md`;
6. `docs/09-runtime-semantics.md`;
7. `docs/10-assistant-adapter-spec.md`;
8. `docs/14-backlog-v0.2.md`;
9. `ARCHITECTURE_DECISIONS.md`.

Проверь исходное состояние:

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/takt
./scripts/verify.sh
```

## Первая рекомендуемая задача

Начни с `TAKT-008`: fake assistant protocol suite.

Требования:

- добавь небольшой тестовый бинарник в `internal/testassistant` или `cmd/takt-test-assistant`;
- реализуй сценарии success, exit, timeout, huge output, malformed result, session и resume;
- сформируй единый набор contract tests;
- не подключай Pi/OpenCode до прохождения fake suite;
- не меняй YAML-контракт без необходимости;
- обнови adapter spec при выявленном расхождении;
- сохрани разделение `exit/start/timed_out/cancelled/protocol/internal`.

## Ограничения

- не добавляй собственные read/write/bash tools для модели;
- не добавляй общий plugin framework;
- не меняй `apiVersion` без мигратора;
- не добавляй Web UI, сервер или БД;
- не скрывай невозможность resume переходом на fresh;
- не считай текст агента доказательством успеха;
- не расширяй scope до недоверенных пользователей без threat model и sandbox.

## Definition of Done

- код отформатирован;
- unit и contract tests проходят;
- `go test -race ./...` проходит;
- `go vet ./...` проходит;
- сквозные примеры работают;
- документация отражает фактическое состояние;
- изменение сохраняет границу с внешним coding agent.
