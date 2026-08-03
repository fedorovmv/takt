# Стартовая инструкция для кодового агента

Эту инструкцию можно передать Pi, OpenCode, Codex или другому агенту при начале разработки Takt.

## Контекст

Takt — Go-runtime для оркестрации готовых кодовых агентов, моделей, deterministic checks, hooks, loops и approval. Takt не должен превращаться в собственный coding agent.

## Перед работой

Прочитай:

1. `docs/12-document-map.md`;
2. `docs/08-target-v0.2.md`;
3. `docs/05-implementation-status.md`;
4. `docs/09-runtime-semantics.md`;
5. `docs/10-assistant-adapter-spec.md`;
6. `docs/11-implementation-plan.md`;
7. `ARCHITECTURE_DECISIONS.md`.

Проверь исходное состояние:

```bash
go test ./...
go vet ./...
go build ./cmd/takt
```

## Первая рекомендуемая задача

Начни с `TAKT-001` из `docs/14-backlog-v0.2.md`: типизированные ошибки runtime.

Требования:

- сохрани текущий внешний YAML-контракт;
- добавь минимальный тип ошибки с code, run ID, node ID и cause;
- переведи на него ошибки process adapter и исчерпания попыток;
- не переписывай scheduler целиком;
- добавь unit tests;
- обнови `docs/05-implementation-status.md`, если задача завершена;
- зафиксируй любое изменение семантики в `docs/09-runtime-semantics.md`.

## Ограничения

- не добавляй собственные read/write/bash tools для модели;
- не добавляй новый общий plugin framework;
- не меняй `apiVersion`, пока нет мигратора;
- не добавляй Web UI, сервер или БД;
- не скрывай невозможность resume переходом на fresh;
- не считай текст агента доказательством успеха — используй внешнюю проверку.

## Definition of Done

- код отформатирован;
- `go test ./...` проходит;
- `go vet ./...` проходит;
- новые ветви поведения покрыты тестами;
- документация отражает фактическое состояние;
- пример workflow продолжает работать;
- изменение не нарушает границу с внешним coding agent.
