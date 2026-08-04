# Результаты проверки Takt v0.1.22-alpha

Проверено 5 августа 2026 года.

## Новый контракт композиции

- `subworkflow` подключает обычный `takt/v1alpha1 Workflow` и компилирует его в родительский DAG до запуска;
- публичный ID контейнера сохраняется для `depends_on` и ссылок `${nodes.<id>.*}`;
- `output_node` обязателен только при нескольких конечных узлах;
- локальные Markdown-команды дочернего workflow разрешаются относительно его каталога;
- approval внутри subworkflow приостанавливает и возобновляет родительский Run;
- изменение подключённого workflow или его локальной команды меняет fingerprint и блокирует небезопасный resume;
- последовательный `foreach` работает только с явно заданным списком и не разбирает Markdown;
- поддерживаются scalar и JSON-object элементы, `${item}`, `${item.field}`, `${index}` и пользовательское имя через `as`;
- профиль `code` 0.2.0 использует переиспользуемые implementation/review subworkflows, сохраняя Markdown-план авторитетным документом;
- authoring skill 0.4.0 описывает композицию и запрещает выдумывать обязательный task AST.

## Пройденные проверки рабочего дерева

```text
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
make check
./scripts/verify.sh

fake-assistant contract suite: PASS
Pi adapter contract suite: PASS
OpenCode adapter contract suite: PASS
Route DSL end-to-end: PASS
Route DSL evaluation: PASS
Route DSL evaluation isolation: PASS
workflow composition contract: PASS
code profile contract: PASS
Takt authoring skill: PASS
documentation check: PASS
JSON Schema Draft 2020-12 composition checks: PASS
```

## Проверка чистого архива

Из предварительно упакованного и заново распакованного дерева прошли:

```text
sha256sum -c MANIFEST.sha256
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
./scripts/verify.sh
takt v0.1.22-alpha
schemas JSON parse: PASS
```

Совмещённая команда unit/race/vet/verify достигла внешнего лимита времени во время повторного контрактного прогона. `verify.sh` был сразу запущен отдельно на том же чистом дереве и прошёл полностью.

## Внешние smoke-тесты

Реальные Pi и OpenCode smoke для этой сборки в среде сборки не запускались: пользовательские бинарники, credentials и provider-конфигурация отсутствуют. Контрактные fake adapters прошли.

## Примечание

В одном предварительном полном unit-прогоне существующий тайминговый тест hook timeout завершился нестабильно. Изолированный повтор 10 раз и последующие полные `make check` и `verify.sh` прошли.
