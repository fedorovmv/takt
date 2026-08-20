# Разработка Takt

## Требования и первый запуск

```bash
make build
make check
```

Нужны Go 1.23+, Git и, для обязательного TypeScript smoke, Node.js 22 с
TypeScript 5.7.2. `make check` можно запускать без `tsc`: тогда smoke явно
помечается как `SKIP`; CI устанавливает pinned compiler и требует его.

## Источники перед изменением

1. `AGENTS.md`;
2. `docs/README.md` и `docs/12-document-map.md`;
3. `docs/05-implementation-status.md`;
4. `skills/takt/SKILL.md`;
5. `docs/03-specification.md`;
6. `docs/09-runtime-semantics.md`;
7. `docs/10-assistant-adapter-spec.md`;
8. `docs/04-architecture.md`;
9. `ARCHITECTURE_DECISIONS.md`.

Для evaluation добавьте `docs/13-evaluation-plan.md` и
`docs/73-evaluation-authoring-guide.md`. Versioned release slices находятся в
`docs/archive/releases/` и нужны только для traceability конкретного решения.

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
- отделять infrastructure contract suite от quality benchmark;
- сохранять per-attempt execution identity при любом retry;
- не трактовать амортизированную длительность benchmark как time-to-valid;
- декодировать доступный validation envelope независимо от exit code; считать успехом только `completed && valid=true`;
- декодировать validation envelope только из stdout quality-node; stderr сохранять как отдельную диагностику.

## Тесты

Основной контур — стандартный Go toolchain. Быстрый developer check не
запускает чёрный ящик второй раз под `-race`; полный release/diagnostic прогон
остаётся отдельной командой:

```bash
make check        # compile all Go packages + focused contracts + build + TypeScript smoke (<1m)
make check-full   # полный обычный и race-пакетный прогон + journeys
make test         # core Go packages, без tests/e2e
make test-all     # все Go packages, включая tests/e2e
make race         # core Go packages под -race
make race-all     # все Go packages под -race
make e2e          # полный black-box Go E2E
make e2e-race     # E2E под -race
```

E2E-контракты используют `GO_TEST_PARALLEL_P=16` независимых test workers по
умолчанию; на слабой машине значение можно уменьшить.

`tests/e2e` запускает настоящий `takt` и проверяет CLI/daemon/MCP/evaluation
через общий Go harness. Shell не используется как второй assertion framework.

Отдельно остаётся один внешний shell smoke test:

```bash
make smoke
```

Он проверяет TypeScript host integration через реальную TypeScript toolchain.
Process/package/host/deep-workflow boundaries находятся в bounded Go E2E.
Allowlist закреплён в `internal/architecture`; новый shell test требует
отдельного архитектурного обоснования. Быстрый gate — `make check`, полный
release gate — `make check-full` или `./scripts/verify.sh`.

Реальные live Pi/OpenCode/credentials smoke и production quality benchmark
выполняются отдельно и не подменяются deterministic release fixtures.

## Структура репозитория

- `cmd/takt` — тонкий launcher;
- `internal/application` — стабильные Run/Catalog/Authoring use cases;
- `internal/runtime` — единый scheduler и execution semantics;
- `internal/store` — durable state, events, locks и artifacts;
- `internal/bootstrap` — единственный production composition root;
- `internal/extensions` — adapters, packages, blocks и notifications;
- `internal/experimental` — Dynamic Flow, host control и learning;
- `internal/tooling` — evaluation и compatibility;
- `tests/e2e` — black-box CLI/daemon/MCP/evaluation contracts.

## Структура задачи

Хорошая задача содержит изменяемый контракт, ожидаемые переходы Run и Node,
события, классы ошибок, критерии приёмки, unit/contract tests, сквозной пример
и обновление спецификации, схем и status.

## Контрактные изменения

- YAML/JSON contract: обновите `docs/03-specification.md` и соответствующие
  `schemas/*.json`;
- runtime/protocol semantics: добавьте Go contract test и обновите
  `docs/09-runtime-semantics.md` или `docs/10-assistant-adapter-spec.md`;
- архитектурная граница: добавьте ADR и architecture gate;
- фактический статус: обновите `docs/05-implementation-status.md` и
  `CHANGELOG.md`;
- совместимость: не добавляйте второй scheduler, transport-specific Run
  semantics или второй composition root.

## OpenCode adapter

```bash
make opencode-contracts
```

Реальный smoke test является opt-in:

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

`.github/workflows/ci.yml` запускает `make check` на `ubuntu-latest` и
`macos-latest`. Worktree/path changes должны проходить обе ОС; локальный
Linux-прогон не заменяет macOS job.
