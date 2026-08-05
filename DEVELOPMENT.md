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

1. `AGENTS.md`;
2. `docs/12-document-map.md`;
3. `docs/05-implementation-status.md`;
4. `skills/takt/SKILL.md`;
5. `docs/32-takt-authoring-skill-v0.1.18.md`;
6. `docs/31-quality-stdout-separation-v0.1.17.md`;
7. `docs/30-quality-envelope-semantics-v0.1.16.md`;
8. `docs/29-benchmark-metric-semantics-v0.1.15.md`;
9. `docs/28-benchmark-identity-quality-v0.1.14.md`;
10. `docs/13-evaluation-plan.md`;
11. `docs/09-runtime-semantics.md`;
12. `docs/10-assistant-adapter-spec.md`;
13. `ARCHITECTURE_DECISIONS.md`.


## С чего продолжать реализацию

Текущая рекомендуемая ветка работ:

```text
Pi adapter и Route DSL contract suites — выполнено
→ evaluation identity и quality contract — выполнено
→ baseline на реальном Pi, штатном validator и обезличенных заданиях
→ сравнение моделей и стратегий на одинаковых fingerprints
→ manual-correction metric и CLI сравнения отчётов
→ OpenCode adapter при подтверждённой необходимости
```

Реальный benchmark запускается отдельно:

```bash
make build
TAKT_CONFIG=/path/to/config.yaml \
TAKT_ROUTE_VALIDATOR=/path/to/route-tool \
TAKT_REPEAT=3 \
make route-benchmark
```

Подробная декомпозиция находится в `docs/11-implementation-plan.md` и `docs/14-backlog-v0.2.md`.

## Правила изменения

- сохранять границу: Takt оркестрирует внешних агентов, но не реализует собственного coding-agent loop;
- считать текущий scope локальным и доверенным;
- добавлять общую абстракцию после появления минимум двух сценариев;
- сопровождать изменение семантики contract test и обновлением `docs/09-runtime-semantics.md`;
- сопровождать изменение YAML или JSON-контракта обновлением спецификации и `schemas/*.json`;
- оформлять изменение архитектурной границы новым ADR;
- не записывать секреты в `state.json`, `events.jsonl` и evaluation report;
- хранить credentials в окружении или внешнем secret source, а не в `models.*.params`;
- не игнорировать ошибки persistence;
- не использовать `allow_failure` для transport/runtime errors;
- отделять infrastructure contract suite от quality benchmark.
- сохранять per-attempt execution identity при любом retry;
- не трактовать амортизированную длительность benchmark как time-to-valid;
- декодировать доступный validation envelope независимо от exit code; считать успехом только `completed && valid=true`.
- декодировать validation envelope только из stdout quality-node; stderr сохранять как отдельную диагностику.

## Тесты

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/takt
go build ./cmd/takt-fake-assistant
go build ./cmd/takt-fake-pi
./scripts/test-fake-assistant.sh
./scripts/test-pi-adapter.sh
./scripts/test-route-dsl-e2e.sh
./scripts/test-route-dsl-eval.sh
./scripts/test-takt-skill.sh
./scripts/check-docs.sh
./scripts/verify.sh
```

Реальные интеграционные тесты Pi/OpenCode и quality benchmark должны быть opt-in и пропускаться в обычном CI при отсутствии бинарника, credentials, модели или штатного валидатора.

## Структура задачи

Хорошая задача содержит:

- изменяемый контракт;
- ожидаемые переходы Run и Node;
- события;
- классы ошибок;
- критерии приёмки;
- unit/contract tests;
- сквозной пример;
- обновление спецификации, схем и status.

## OpenCode adapter

Run the deterministic contract suite with:

```bash
make opencode-contracts
```

A real smoke test is opt-in:

```bash
TAKT_OPENCODE_SMOKE=1 \
TAKT_OPENCODE_SMOKE_PROVIDER=<provider> \
TAKT_OPENCODE_SMOKE_MODEL=<model> \
make opencode-contracts
```

Проверка reusable workflow и последовательного foreach:

```bash
make composition
```

## CI matrix

`.github/workflows/ci.yml` запускает `make check` на `ubuntu-latest` и `macos-latest`. Worktree/path changes должны проходить обе ОС; локальный Linux-прогон не заменяет macOS job.
