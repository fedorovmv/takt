# Разработка Takt

## Первый запуск

```bash
go test ./...
go vet ./...
go build -o bin/takt ./cmd/takt
./bin/takt version
```

Проверка примеров:

```bash
./scripts/verify.sh
```

## Что прочитать перед изменением ядра

1. `docs/12-document-map.md`;
2. `docs/08-target-v0.2.md`;
3. `docs/05-implementation-status.md`;
4. `docs/09-runtime-semantics.md`;
5. `ARCHITECTURE_DECISIONS.md`.

## С чего начать реализацию

Первая рекомендуемая ветка работ:

```text
typed runtime errors
→ timeout/cancel process adapter
→ capability contract
→ Pi или OpenCode adapter
→ Route DSL end-to-end
```

Подробная декомпозиция находится в `docs/11-implementation-plan.md`.

## Правила изменения

- сохранять границу: Takt оркестрирует внешних агентов, но не реализует собственного coding-agent loop;
- добавлять общую абстракцию после появления минимум двух сценариев использования;
- сопровождать изменение семантики тестом и обновлением `docs/09-runtime-semantics.md`;
- сопровождать изменение YAML-контракта обновлением `docs/03-specification.md` и `schemas/*.json`;
- оформлять изменение архитектурной границы новым ADR;
- не записывать секреты в `state.json` и `events.jsonl`.

## Тесты

```bash
go test ./...
go test ./internal/runtime -run TestName -v
go test -race ./...
go vet ./...
```

Реальные интеграционные тесты Pi/OpenCode должны быть opt-in и пропускаться в обычном CI при отсутствии бинарника или credentials.

## Структура задач

Хорошая задача на доработку должна содержать:

- изменяемый контракт;
- ожидаемые переходы состояния;
- события;
- ошибки;
- критерии приёмки;
- unit tests;
- сквозной пример.
