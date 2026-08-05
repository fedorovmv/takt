# Результаты проверки Takt v0.1.26-alpha

Проверено 5 августа 2026 года.

## Среда

```text
Go: go1.23.2 linux/amd64
OS: Linux 6.18.35 x86_64
Исходный архив: takt-v0.1.25-alpha.zip
SHA-256 исходного архива: 8cf114daba62a31e225591ab45f8b87062d2bc852f08291cf561d395b66475dc
```

Контрольная сумма исходного архива совпала с приложенным файлом `takt-v0.1.25-alpha.sha256`.

## Проверенный контракт среза

- узел `workflow` запускает подключённое определение как отдельный governed child Run;
- ребёнок получает собственные Run ID, state, events, artifacts, fingerprints, output и usage;
- родитель и ребёнок связаны через `parent_run_id`, `parent_node_id`, `child_run_ids` и waiting link;
- `takt children` показывает прямых детей и их execution state;
- approval внутри ребёнка принимается через корневой Run, после чего CLI продолжает ребёнка и всю parent chain;
- `takt cancel` использует durable marker, каскадирует отмену по дереву и останавливает активный process context;
- `workflow.isolation` поддерживает собственную policy ребёнка, `inherit`, `worktree` и `none`;
- retry родительского узла создаёт новый child Run, а не перезаписывает terminal child attempt;
- parent fingerprint рекурсивно включает статически подключённые child definitions; рекурсия и глубина более 16 отклоняются;
- root router профиля `code` запускает выбранный из 19 процессов как child Run;
- reusable review-блоки запускаются как child Runs с `isolation: inherit`;
- профиль `code` 0.5.0 и authoring skill 0.8.0 описывают structural и governed composition как разные контракты.

## Фактически завершившиеся проверки

```text
gofmt -w cmd internal                       PASS
go vet ./...                                PASS
go test ./... -count=1                      PASS
go test -race ./... -count=1                PASS
make check                                  PASS
./scripts/verify.sh                         PASS
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
governed child run contract: PASS
documentation check: PASS
verification: PASS
```

## Специальные регрессии v0.1.26

- child approval переводит родителя в `waiting` с `kind: child_run`, отдельным Child Run ID и отдельным каталогом артефактов;
- ответ по публичному ID родительского workflow-узла доходит до фактического approval ребёнка и завершает корень;
- failure ребёнка переводит родительский узел и Run в failure без потери child state;
- отмена ожидающего дерева переводит и root, и child в `cancelled`;
- cancellation marker завершает реально выполняющийся `sleep 30`, не дожидаясь естественного exit;
- child `isolation: inherit` выполняется в worktree родителя, но сохраняет отдельные state/events/artifacts;
- выбранный mutating workflow smart router получает собственный managed worktree, а router остаётся в control checkout;
- retry узла `workflow` создаёт два дочерних Run: первый сохраняется как `failed`, второй как `completed`;
- изменение child definition меняет fingerprint родителя и блокирует resume;
- multiple terminal branches корневого router возвращают output единственной фактически завершившейся terminal-ветви;
- `workflow.isolation: shared` отклоняется валидатором;
- schema files декодируются как корректный JSON.

## Запуски, остановленные внешним лимитом

Первая попытка `make check` была остановлена внешним лимитом tool call во время `scripts/test-pi-adapter.sh`. До остановки завершились formatting, vet, unit, race и fake-assistant contracts; ошибок проекта не было. `scripts/test-pi-adapter.sh` затем прошёл отдельно, а повторный полный `make check` завершился с кодом `0`. Итоговый `./scripts/verify.sh` также завершился с кодом `0`.

## Внешние интеграции

Реальные Pi/OpenCode smoke, GitHub writes и Remotion rendering не запускались: в среде сборки нет пользовательских credentials, provider-конфигурации и целевого репозитория. Проверены fake adapter contracts, Git worktree lifecycle, governed child lifecycle, все workflow definitions и runtime semantics.

## Оставшиеся крупные пробелы

- per-node `allowed_tools`, `denied_tools`, skills, MCP, sandbox и capability negotiation;
- динамический fan-out governed children из output предыдущего узла;
- параллельная scheduler-волна для `workflow`-узлов, portable hooks и повторных попыток;
- script nodes и типизированные артефакты с `output_type`;
- строгие неизвестные template variables, более полный JSON Schema и расширенная история loop iterations;
- server, Web UI, БД, remote workers и message adapters остаются proposal-направлением для возможного выхода за локальный trusted runtime.

## Проверка поставляемого архива

Черновой `takt-v0.1.26-alpha.zip` был распакован в отдельный чистый каталог. Непосредственно из содержимого архива завершились:

```text
sha256sum -c MANIFEST.sha256              PASS
go test ./... -count=1                    PASS
go test -race ./... -count=1              PASS
fake-assistant contract suite             PASS
Pi adapter contract suite                 PASS
OpenCode adapter contract suite           PASS
Route DSL end-to-end/evaluation/isolation PASS
workflow composition                      PASS
Takt authoring skill                       PASS
code profile catalog                      PASS
git worktree lifecycle                    PASS
governed child run lifecycle              PASS
documentation check                       PASS
example validation set                    PASS
```

Один монолитный вызов `./scripts/verify.sh` был остановлен внешним лимитом после Pi contracts без ошибки проекта. Тот же скрипт затем выполнен по неизменённым командам отдельными группами в той же чистой распаковке; все этапы завершились успешно. Это ограничение относится к длительности вызова инструмента проверки, а не к Takt.
