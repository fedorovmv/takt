# Governed child Runs in v0.1.26-alpha

## Назначение

`subworkflow` остаётся механизмом повторного использования структуры внутри одного Run: файл компилируется в родительский DAG, использует общее состояние и не имеет собственного жизненного цикла.

Новый узел `workflow` решает другую задачу. Он запускает подключённый workflow как **отдельный управляемый Run** со своим идентификатором, состоянием, журналом событий, каталогом артефактов, usage и worktree-политикой. Родитель ждёт завершения ребёнка и получает его публичный результат через обычный output узла.

Этот механизм нужен для крупных процессов профиля `code`: умный роутер теперь остаётся наблюдаемым корневым Run, а выбранный процесс выполняется дочерним Run. Те же правила применяются к переиспользуемым review-блокам.

## Контракт YAML

```yaml
- id: implementation
  workflow:
    path: workflows/feature-development.yaml
    input: ${input}
    output_node: summary
    isolation: inherit
```

Поля:

- `path` — обязательный путь к `takt/v1alpha1 Workflow`, относительно содержащего файла;
- `input` — вход дочернего Run после подстановки шаблонов;
- `output_node` — публичный узел, результат которого становится output родительского узла;
- `isolation` — `inherit`, `worktree`, `none` или пустое значение.

Если `output_node` не задан, у дочернего workflow должен быть ровно один структурный terminal-узел. Для workflow с несколькими terminal-ветвями поле задаётся явно.

## Отдельное состояние и связь дерева

Дочерний Run получает:

- собственный `id`;
- `parent_run_id` и `parent_node_id`;
- отдельные `.takt/runs/<child-id>/state.json` и `events.jsonl`;
- отдельный `.takt/runs/<child-id>/artifacts/`;
- собственные fingerprints, usage, output и terminal status.

Родитель хранит:

- `child_run_ids` на уровне Run;
- текущий `child_run_id` и историю `child_run_ids` на узле `workflow`;
- waiting-ссылку с `kind: child_run`, когда ребёнок остановлен на approval;
- aggregate usage дочернего Run в execution record родительского узла.

Повтор родительского узла создаёт новый дочерний Run. Неуспешный и успешный дочерние запуски остаются отдельными аудируемыми попытками, а `child_run_id` указывает на текущую попытку.

## Lifecycle

1. Родитель готовит узел `workflow` и заранее создаёт Child Run ID.
2. Child Run получает свой definition fingerprint и начинает выполнение.
3. При завершении ребёнка его output, stdout/stderr, exit code и usage становятся результатом родительского узла.
4. При failure или cancellation ребёнка родительский узел получает соответствующую failure-классификацию.
5. При approval ребёнка родитель становится `waiting`, но не дублирует approval в собственном состоянии.
6. `takt answer` может быть вызван по корневому Run ID и публичному ID родительского `workflow`-узла. CLI спускается к фактическому approval, продолжает ребёнка и затем поднимает resume до корня.
7. `takt cancel` ставит durable cancellation marker и распространяет отмену по дереву детей. Ожидающие Run отменяются сразу, активные наблюдают marker во время выполнения узла.

## Worktree isolation

`workflow.isolation` управляет рабочей директорией ребёнка:

| Значение | Поведение |
|---|---|
| пусто | применяется собственная `worktree`-политика дочернего workflow |
| `inherit` | ребёнок выполняется в execution workspace родителя и не создаёт свой worktree |
| `worktree` | ребёнок принудительно создаёт отдельный managed worktree |
| `none` | ребёнок выполняется в control workspace без worktree |

Состояние и артефакты всех детей всегда сохраняются в control workspace. `inherit` разделяет файловую транзакцию, но не state/events/artifacts. `worktree` создаёт независимую файловую транзакцию и ветку.

## CLI

```bash
# Прямые дети Run
takt children <run-id> --workspace .

# Статус любого ребёнка читается как статус обычного Run
takt status <child-run-id> --workspace .

# Ответ на approval внутри глубоко вложенного ребёнка через корневой Run
takt answer <root-run-id> <public-workflow-node-id> \
  --value approved --workspace .

# Каскадная отмена дерева
takt cancel <run-id> --reason "остановлено пользователем" --workspace .
```

`children` возвращает прямых детей, их статус, workflow path, parent node, execution workspace и usage. Полное дерево восстанавливается повторным вызовом для нужного ребёнка.

## Fingerprints и resume

Fingerprint родителя включает определения всех статически подключённых `workflow.path` рекурсивно. Изменение дочернего workflow блокирует `answer` и `resume` уже начатого дерева до исполнения изменённого определения.

Каждый ребёнок дополнительно хранит собственные fingerprints workflow, config и Markdown-команд. Это сохраняет самостоятельный resume-контракт дочернего Run.

Глубина статических ссылок ограничена 16. Рекурсивная цепочка `workflow` отклоняется до запуска.

## Профиль code 0.5.0

Умный роутер и reusable review-блоки переведены с compile-time `subworkflow` на governed `workflow`:

- корневой router Run хранит решение маршрутизации;
- выбранный из 19 процессов получает отдельный Run ID;
- изменяющий процесс применяет собственную worktree-политику;
- review-блоки используют `isolation: inherit`, чтобы продолжать работу в той же ветке, но сохранять отдельный lifecycle и usage.

`subworkflow` сохраняется для внутренней структурной композиции и compile-time fan-out, где отдельный жизненный цикл не нужен.

## Проверки

Новый contract suite `scripts/test-child-runs.sh` проверяет через CLI:

- создание родительского и дочернего Run;
- `parent_run_id`, `parent_node_id` и `child_run_ids`;
- просмотр через `takt children`;
- approval через корневой Run;
- автоматическое продолжение ребёнка и родителя;
- передачу output;
- каскадную отмену ожидающего дерева.

Unit-тесты дополнительно проверяют failure propagation, cancellation активного процесса, worktree inheritance, fingerprints и создание нового ребёнка при retry.

## Оставшиеся ограничения

- несколько `workflow`-узлов пока не образуют параллельную scheduler-волну;
- динамический fan-out дочерних Run из output предыдущего узла ещё не реализован;
- нет отдельной политики лимита числа детей и конкурентности;
- нет per-node tool/MCP/skills/sandbox contract;
- server, Web UI и БД остаются proposal для возможного нелокального режима и не входят в локальный runtime.
