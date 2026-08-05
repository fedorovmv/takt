# Результаты проверки Takt v0.1.24-alpha

Проверено 5 августа 2026 года.

## Среда

```text
Go: go1.23.2 linux/amd64
OS: Linux 6.12.13 x86_64
Исходный архив: takt-v0.1.23-alpha.zip
SHA-256 исходного архива: 50685e14575d6867ebee0978be17592eb05c56cd652baa4f364013487cba1ea8
```

Контрольная сумма исходного архива совпала с приложенным файлом `takt-v0.1.23-alpha.sha256`.

## Проверенный контракт среза

- профиль `code` 0.3.0 содержит ровно 19 пользовательских workflow и два переиспользуемых review-блока;
- default workflow профиля является умным роутером и выбирает одну из 19 ветвей внутри того же Run;
- любой процесс запускается напрямую селектором `code:<workflow>`;
- `takt workflow list` и `takt workflow describe` показывают каталог;
- `output_format` проверяет один JSON-результат агентного узла, обязательные поля, типы, `enum`, массивы и `additionalProperties`;
- вложенные JSON-пути доступны в `when` и шаблонах;
- независимые простые `command`, `prompt` и `bash` выполняются параллельной scheduler-волной;
- `foreach.parallel` выполняет итерации конкурентно и собирает результат в порядке входного массива;
- approval внутри `loop_group` останавливает Run, после ответа продолжает текущую итерацию и повторно запрашивается на следующей;
- `trigger_rule: one_success` соединяет условные ветви после их terminal-состояния;
- authoring skill обновлён до 0.6.0 и описывает новый контракт.

## Полные проверки рабочего дерева

Фактически завершились успешно:

```text
gofmt -w cmd internal                       PASS
go vet ./...                                PASS
./scripts/verify.sh                         PASS
```

`verify.sh` последовательно выполнил полный `go test ./...`, полный `go test -race ./...`, сборку бинарников, adapter contract suites, Route DSL E2E/evaluation, composition, authoring skill, каталог `code`, документацию и штатные `takt validate`.

Дополнительно отдельно выполнены с `-count=1`:

```text
go test ./internal/profile ./internal/workflow ./internal/runtime ./cmd/takt
                                               PASS
go test -race ./internal/runtime               PASS
go test -race ./internal/workflow ./internal/profile ./cmd/takt
                                               PASS
go test -race ./internal/assistant              PASS
go test -race ./internal/command ./internal/config ./internal/definition \
  ./internal/evaluation ./internal/execution   PASS
go test -race ./internal/store ./internal/validation ./internal/yamlmini
                                               PASS
```

Контрактные наборы:

```text
fake-assistant contract suite: PASS
Pi adapter contract suite: PASS
OpenCode adapter contract suite: PASS
Route DSL end-to-end: PASS
Route DSL evaluation: PASS
Route DSL evaluation isolation: PASS
workflow composition: PASS
Takt authoring skill: PASS
code profile catalog contract: PASS
documentation check: PASS
verification: PASS
```

## Специальные регрессии v0.1.24

Проверены отдельные сценарии:

- два независимых узла проходят взаимный файловый барьер, который невозможно пройти при последовательном запуске;
- параллельный `foreach` проходит общий барьер двух итераций и возвращает `["one","two"]`;
- два последовательных approval внутри разных итераций одного `loop_group` корректно возобновляют Run;
- schema-valid JSON нормализуется, недопустимое значение `enum` становится `protocol`-ошибкой;
- поле `nodes.route.output.workflow` работает в условии и шаблоне;
- mock-запуск роутера вызывает только `route` и выбранный `assist`, остальные 18 ветвей пропускаются;
- каждый из 19 явных селекторов проходит `takt validate` после `takt init code`;
- профиль после установки содержит 21 YAML-файл workflow: 19 пользовательских и два reusable review-блока.

## Прерванные запуски

Первые два foreground-запуска `make check` были остановлены внешним лимитом одного вызова инструмента во время полного `go test -race ./...`. Следующий полный запуск выявил слишком жёсткий временной порог теста параллельности: две секундные ветви завершились за 1,82 с при пороге 1,8 с. Проверка заменена на взаимный файловый барьер, который доказывает конкурентный запуск без зависимости от скорости среды; новый тест прошёл 20 повторов. После исправления полный `make check` и `verify.sh` выполнены повторно.

## Внешние интеграции

Реальные Pi/OpenCode smoke и фактические операции с GitHub/Remotion не запускались: в среде сборки нет пользовательских credentials, provider-конфигурации и целевых репозиториев. Проверены fake adapter contracts, структура всех workflow, маршрутизация с mock assistant и детерминированные runtime-механизмы.

## Оставшиеся функциональные пробелы относительно Archon

Все 19 процессов и умный роутер присутствуют. Оставшиеся различия относятся к инфраструктуре исполнения:

- нет автоматической git worktree isolation для Run;
- `subworkflow` остаётся частью родительского Run, а не отдельным governed child Run;
- нет per-node `allowed_tools`, `denied_tools`, skills, MCP и sandbox policy;
- нет script nodes Bun/uv, `output_type`, CLI `cancel`, server/Web UI, БД, message adapters и notifications;
- параллельная scheduler-волна пока не включает узлы с portable hooks или `attempts.max > 1`;
- `items_from` читает статический compile-time файл, динамический fan-out из output узла не реализован;
- полный служебный `state.json` сохраняет namespaced ID, внешним контрактом остаётся публичная проекция CLI.
