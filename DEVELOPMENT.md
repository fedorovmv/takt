# Разработка Takt

## Первый запуск

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o bin/takt ./cmd/takt
./scripts/verify.sh
```

## Что прочитать перед изменением ядра

1. `docs/12-document-map.md`;
2. `docs/05-implementation-status.md`;
3. `docs/18-audit-remediation-v0.1.4.md`;
4. `docs/17-audit-remediation-v0.1.3.md`;
5. `docs/16-audit-remediation-v0.1.2.md`;
6. `docs/09-runtime-semantics.md`;
7. `ARCHITECTURE_DECISIONS.md`.

## С чего продолжать реализацию

Текущая рекомендуемая ветка работ:

```text
Pi adapter contract suite и opt-in smoke
→ Route DSL end-to-end на Pi
→ OpenCode adapter при необходимости сравнения
→ Go и document workflows
```

Подробная декомпозиция находится в `docs/11-implementation-plan.md` и `docs/14-backlog-v0.2.md`.

## Правила изменения

- сохранять границу: Takt оркестрирует внешних агентов, но не реализует собственного coding-agent loop;
- считать текущий scope локальным и доверенным;
- добавлять общую абстракцию после появления минимум двух сценариев;
- сопровождать изменение семантики contract test и обновлением `docs/09-runtime-semantics.md`;
- сопровождать изменение YAML-контракта обновлением `docs/03-specification.md` и `schemas/*.json`;
- оформлять изменение архитектурной границы новым ADR;
- не записывать секреты в `state.json`, `events.jsonl` и JSON CLI;
- не игнорировать ошибки persistence;
- не использовать `allow_failure` для transport/runtime errors.

## Тесты

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/takt
go build ./cmd/takt-fake-pi
./scripts/test-pi-adapter.sh
./scripts/verify.sh
```

Реальные интеграционные тесты Pi/OpenCode должны быть opt-in и пропускаться в обычном CI при отсутствии бинарника или credentials.

## Структура задачи

Хорошая задача содержит:

- изменяемый контракт;
- ожидаемые переходы Run и Node;
- события;
- классы ошибок;
- критерии приёмки;
- unit/contract tests;
- сквозной пример;
- обновление спецификации и status.
