# Результаты проверки Takt v0.1.25-alpha

Проверено 5 августа 2026 года.

## Среда

```text
Go: go1.23.2 linux/amd64
OS: Linux 6.12.13 x86_64
Исходный архив: takt-v0.1.24-alpha.zip
SHA-256 исходного архива: b4d189bff44fa55fa5444edd6b8ae4b72e56a1c6688311286c13faca1e533966
```

Контрольная сумма исходного архива совпала с приложенным файлом `takt-v0.1.24-alpha.sha256`.

## Проверенный контракт среза

- workflow-level Git worktree policy создаёт отдельную ветку и execution workspace, сохраняя state/events/artifacts в control workspace;
- direct selector применяет policy при старте Run, а smart router активирует policy выбранного дочернего workflow на его gate;
- CLI поддерживает `--worktree`, `--no-worktree`, `--keep-worktree`, `--allow-dirty-worktree`, `--worktree-base` и `takt worktree list/remove/prune`;
- чистый успешный worktree с `cleanup: on_success` удаляется, branch сохраняется; dirty/failed/cancelled/manual worktree удерживается;
- CLI overrides и оба workspace сохраняются в Run state и используются при resume;
- `output_format` нормализует только `output`, сохраняя raw provider stdout/stderr;
- native attempts policy повторяет `protocol`-ошибки и передаёт точный validation error через `${feedback}`;
- router использует один retry mechanism без дублирующего failure hook;
- утверждённый `interactive-prd` не запускает revise на итерации `ready`;
- malformed reproduction output в `create-issue` запускает reporting и summary branches;
- integer output validation сохраняет точность для значений больше `2^53`;
- `current_nodes` публикуется на время параллельной волны и очищается отдельным persisted transition;
- comprehensive review использует `foreach.parallel` по пяти перспективам;
- профиль `code` 0.4.0 содержит 19 пользовательских workflow и три reusable workflow-блока;
- authoring skill 0.7.0 описывает managed worktree contract.

## Фактически завершившиеся проверки

```text
gofmt -w cmd internal                       PASS
go vet ./...                                PASS
go test ./... -count=1                      PASS
go test -race ./... -count=1                PASS
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
git worktree contract: PASS
documentation check: PASS
```

## Специальные регрессии v0.1.25

- top-level workflow изменяет файл только в managed worktree, а control checkout остаётся неизменным;
- clean successful worktree удаляется автоматически, но созданная branch остаётся;
- child workflow включает worktree isolation на compiled gate до первого дочернего действия;
- `--no-worktree` подавляет child policy и сохраняется в Run state;
- code router работает в control checkout, выбирает `feature-development`, после чего все дочерние agent nodes получают execution workspace worktree;
- assist route не создаёт worktree;
- waiting Run возобновляется в том же worktree, а active worktree нельзя удалить через CLI;
- dirty retained worktree требует `--force` для ручного удаления;
- structured output сохраняет raw NDJSON stdout после JSON normalization;
- второй protocol attempt получает enum/schema error в `${feedback}` и использует fresh session;
- `create-issue` сохраняет protocol failure узла reproduce, но выполняет `reproduction-error` и итоговый summary;
- большие целые и целые в exponent form принимаются без `Float64`, дробные значения отклоняются;
- full review fan-out возвращает результаты `foreach.parallel` в исходном порядке.

## Запуски, остановленные внешним лимитом

Один foreground-запуск `make check` был остановлен внешним лимитом инструмента во время `go test ./...`; на момент остановки ошибок проекта не было. Полные unit и race suites после этого выполнены отдельными фоновыми командами и завершились с кодом `0`. Все составные contract scripts и `verify.sh` также запущены отдельно, чтобы внешний лимит одного tool call не подменял результат проекта.

## Внешние интеграции

Реальные Pi/OpenCode smoke, GitHub writes и Remotion rendering не запускались: в среде сборки нет пользовательских credentials, provider-конфигурации и целевого репозитория. Проверены fake adapter contracts, Git worktree lifecycle, все workflow definitions и runtime semantics.

## Оставшиеся крупные пробелы

- `subworkflow` остаётся частью родительского Run; governed child Run с отдельными ID, state/events, artifacts, cost и cancellation пока не реализован;
- отсутствуют per-node `allowed_tools`, `denied_tools`, skills, MCP и sandbox policy;
- отсутствуют script nodes Bun/uv, `output_type`, CLI `cancel` и runtime fan-out из output предыдущего узла;
- `foreach` с child workflow, который сам требует отдельный worktree, отклоняется до появления governed child Runs;
- параллельная scheduler-волна пока не включает узлы с portable hooks или `attempts.max > 1`;
- server, Web UI, БД, remote workers и message adapters остаются proposal-направлением для возможного выхода за локальный trusted runtime и не входят в текущий приоритет.
