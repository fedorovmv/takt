# Go-native Test Architecture — v0.1.53-alpha

`v0.1.53-alpha` — отдельный рефакторинг тестового контура после application-boundary refactor `v0.1.52`. Продуктовые контракты Takt не менялись.

## Проблема

К `v0.1.52` release gate содержал 38 `scripts/test-*.sh` суммарно примерно на 3250 строк. Shell использовался не только как внешний smoke layer, но и как второй тестовый framework:

- повторно собирал одни и те же бинарники;
- вручную создавал temp workspace и fixtures;
- разбирал JSON через встроенный Python;
- проверял структуры через `grep`;
- дублировал уже существующие Go unit/component suites;
- содержал отдельную Python-реализацию schema registry validation;
- требовал вспомогательные assertion binaries только для связи shell и Go.

Это усложняло локальный запуск, diagnostics, переиспользование fixtures и review изменений тестовой семантики.

## Решение

Тесты разделены на три уровня.

### 1. Unit/component contracts

Обычные `*_test.go` рядом с production packages остаются основным способом проверки runtime, application, adapters, schemas, storage и protocol semantics.

Канонический запуск:

```bash
go test ./... -count=1
```

### 2. Black-box Go E2E

`tests/e2e` проверяет собранный `takt` как пользовательский процесс, но orchestration/assertions реализованы на Go.

Общий harness:

- собирает требуемый binary один раз на test process;
- создаёт изолированный `t.TempDir()`;
- запускает CLI с явным environment;
- декодирует JSON/JSONL/JSON-RPC без Python;
- содержит reusable file/tree/git/eventually helpers;
- выводит stdout/stderr через `testing.T` при сбое.

В Go перенесены, в частности:

- compatibility;
- workflow composition;
- worktree lifecycle;
- BlockPackage catalog;
- Takt skill и code profile;
- human-reviewed learning loop;
- authoring diagnostics;
- MCP 2025/2026 surfaces;
- daemon lifecycle;
- Structured Task Sources;
- Adapter Platform;
- Route DSL E2E/evaluation/benchmark;
- Dynamic Takt;
- Simple Reliable Router.

Schema registry contract перенесён в `internal/schemacontract`; Python-validator удалён.

### 3. Внешние smoke tests

Shell оставлен только там, где предмет проверки действительно находится на process/language/package boundary:

- `test-deep-code-workflows.sh` — один representative Git/fake-gh/process-agent путь; полный каталог и runtime semantics проверяются Go-тестами;
- `test-host-control.sh`;
- `test-host-integrations-typescript.sh`;
- `test-package-distribution.sh`;
- `test-reference-adapters.sh`.

Архитектурный тест содержит явный allowlist этих пяти файлов. Новый `scripts/test-*.sh` без изменения architecture contract приводит к падению `go test ./internal/architecture`.

## Makefile

Исторические contract target names сохранены как удобные aliases, но теперь большинство из них запускают Go packages или конкретные Go E2E tests.

Основные команды:

```bash
make test        # go test ./...
make race        # go test -race ./...
make e2e         # black-box Go E2E
make smoke       # пять внешних shell smoke tests
make check       # fmt + vet + test + race + build + smoke + docs + manifest
```

`go test ./...` является источником истины для product correctness. Smoke слой не должен повторно реализовывать business semantics, которые доступны через Go API/test harness.

`make test`, `make race` и `scripts/verify.sh` ограничивают package parallelism значением `8` по умолчанию (`GO_TEST_P` можно переопределить), потому что process-heavy E2E и adapter suites при неограниченной конкуренции создавали нестабильное время release gate. Это ограничение orchestration, а не отдельная тестовая семантика.


## Удалённый тестовый код

Удалены shell wrappers над существующими Go suites и shell/Python contracts, перенесённые в Go. Также удалены `internal/testsupport/routee2eassert` и `internal/testsupport/evalassert`: их assertions теперь находятся непосредственно в `tests/e2e`.

## Ограничения

Рефакторинг намеренно не вводит BDD framework, generic scenario DSL или стороннюю test library. Fixtures и assertions остаются обычным Go-кодом. Это соответствует KISS/YAGNI и сохраняет возможность использовать стандартные `go test`, `-run`, `-race`, coverage и IDE tooling.
