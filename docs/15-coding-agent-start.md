# Стартовая инструкция для кодового агента

## Контекст

Takt — Go-runtime для оркестрации готовых кодовых агентов, моделей, детерминированных проверок, hooks, loops и approval. Takt не реализует собственный coding-agent tool loop.

Текущая версия предназначена для локального trusted runtime.

## Перед работой

Прочитай:

1. `docs/12-document-map.md`;
2. `docs/05-implementation-status.md`;
3. `docs/22-pi-adapter-v0.1.8.md`;
4. `docs/21-protocol-hardening-v0.1.7.md`;
5. `docs/20-fake-assistant-contract-v0.1.6.md`;
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

Начни с `TAKT-011`: Route DSL end-to-end на уже реализованном Pi adapter.

Требования:

- сначала выполни opt-in Pi smoke при доступных credentials;
- замени mock в Route DSL workflow на `type: pi`;
- подключи реальный validator и передавай его diagnostics в retry;
- не меняй timeout, output limit, fresh/resume и error classification без обновления спецификации и ADR;
- сохраняй контракт `takt-assistant/v1alpha1` для универсальных process adapters; Pi использует отдельный RPC adapter;
- не допускай тихого fallback с resume на fresh;
- сохрани границу: внутренний tool loop остаётся внутри Pi/OpenCode.

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


Перед изменением Pi adapter прочитайте `docs/23-pi-rpc-alignment-v0.1.9.md`: `agent_settled` является финальной границей попытки, а usage возвращается как дельта накопленной статистики.
