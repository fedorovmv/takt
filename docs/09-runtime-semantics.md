# Спецификация семантики runtime

Статус документа: целевой контракт v0.2. Текущие отличия перечислены в `05-implementation-status.md`.

## 1. Основные сущности

### Workflow

Версионированное описание DAG, циклов, hooks и approval.

### Run

Один запуск workflow с неизменяемыми исходными входами и собственной историей событий.

### Node

Единица выполнения. Узел имеет один тип действия, зависимости, условие, попытки и hooks.

### Attempt

Одна попытка выполнения узла. Approval, который переводит Run в `waiting`, не считается завершённой попыткой.

### Iteration

Один проход `loop_group`. Итерация содержит отдельные состояния дочерних узлов.

## 2. Состояния Run

```text
running ──approval──> waiting ──answer──> running
   │                     │
   ├──success──────────> completed
   ├──error────────────> failed
   └──cancel───────────> cancelled
```

Допустимые переходы:

| Из | В | Причина |
|---|---|---|
| отсутствует | running | `run` |
| running | waiting | approval требует ввода |
| waiting | running | корректный `answer` |
| running | completed | все верхнеуровневые узлы terminal и нет failure |
| running/waiting | cancelled | `cancel` |
| running | failed | необработанная ошибка или исчерпан лимит |

`completed`, `failed` и `cancelled` — конечные состояния.

## 3. Состояния Node

```text
pending → running → completed
    │        │
    │        ├→ waiting → pending
    │        ├→ pending     retry
    │        └→ failed
    └────────→ skipped
```

Допустимые статусы:

- `pending`;
- `running`;
- `waiting`;
- `completed`;
- `failed`;
- `skipped`;
- `cancelled`.

Текущий прототип хранит `waiting` на уровне Run и может не записывать его в NodeState. v0.2 должен хранить оба состояния согласованно.

## 4. Планирование DAG

Узел готов к запуску, когда:

1. все зависимости находятся в terminal-состоянии;
2. `trigger_rule` разрешает запуск;
3. `when` вычисляется в `true`.

При `when == false` узел переходит в `skipped`.

### Trigger rules

#### `all_success`

Запуск, когда все зависимости `completed`. Иначе узел `skipped`.

#### `all_done`

Запуск после любого terminal-результата зависимостей.

#### `none_failed_min_one_success`

Запуск, когда среди зависимостей нет `failed` или `cancelled` и хотя бы одна завершилась `completed`.

## 5. Приоритет настроек агентного узла

Для assistant и model:

```text
node
→ Markdown command frontmatter
→ workflow defaults
→ ошибка разрешения
```

Для session policy:

```text
node.session
→ workflow.defaults.session
→ fresh
```

Разрешённые значения v0.2:

- `fresh` — новая агентная сессия;
- `resume` — продолжение сохранённой сессии;
- `inherit` — использовать session policy родительского loop/subworkflow.

В текущем прототипе строка session передаётся без полной проверки.

## 6. Попытки узла

`attempts.max` включает только фактические запуски действия узла.

Порядок одной попытки:

```text
node.started
→ before_node hooks
→ action
→ on_failure hooks при ошибке action
→ after_node hooks
→ before_complete hooks
→ node.completed
```

Решение `retry`:

- сохраняет feedback;
- возвращает узел в `pending`;
- при `session: fresh` очищает session ID;
- увеличивает счётчик только после следующего фактического запуска.

При исчерпании `attempts.max` узел становится `failed`, а Run — `failed`, если ошибка не обработана родительской конструкцией.

## 7. Ошибки и allow_failure

Результат действия должен разделять:

- transport/runtime error;
- process exit code;
- stdout;
- stderr;
- structured output, если доступен.

`allow_failure: true` разрешает ненулевой exit code как данные. Он не скрывает transport error, например невозможность запустить бинарник или отмену контекста.

## 8. Hooks

Hooks исполняются последовательно в порядке объявления:

1. глобальные workflow hooks;
2. локальные node hooks.

Результат hook:

- exit code 0 — продолжение;
- ненулевой exit code — применяется `on_failure`;
- transport error — считается ошибкой hook.

### Решения

#### `continue`

Ошибка hook записывается в события, выполнение продолжается.

#### `retry`

Текущая попытка не завершается успешно. Stdout и stderr hook нормализуются и добавляются в feedback.

#### `fail`

Узел завершается ошибкой.

## 9. Loop group

`loop_group` является контейнером дочернего DAG.

Каждая итерация:

1. создаёт новые состояния дочерних узлов;
2. предоставляет результаты предыдущей итерации через `loop.previous`;
3. выполняет дочерний DAG;
4. вычисляет `until`;
5. сохраняет итог итерации в журнале.

При `until == true` loop node завершается `completed`.

При исчерпании `max_iterations` loop node становится `failed` с кодом `loop_exhausted`. Однако timeout или cancellation родительской попытки имеют приоритет над `loop_exhausted`: родительский loop node получает `timed_out` или `cancelled`, а исходная классификация сохраняется на уровне Run.

Область имён дочерних узлов локальна loop group. Во внешнем DAG доступен агрегированный output loop node. Текущий прототип частично хранит дочерние состояния в общей карте; это должно быть исправлено до стабилизации схемы.

Approval внутри `loop_group` не входит в v0.2 и должен отклоняться валидатором.

## 10. Approval

При первом выполнении approval:

1. узел формирует message;
2. Run и Node переходят в `waiting`;
3. `state.json` сохраняется;
4. записывается `approval.requested`;
5. CLI возвращает JSON и код успеха.

При `answer`:

1. проверяются Run ID и Node ID;
2. ответ сохраняется;
3. записывается `approval.answered`;
4. узел возвращается в `pending`;
5. Run продолжается с текущего workflow.

Approval не должен повторно спрашивать ввод после сохранённого ответа.

## 11. Отмена и тайм-ауты

Целевое поведение v0.2:

- Run принимает cancellation через CLI/API;
- активный дочерний процесс получает отмену context;
- после grace period процесс принудительно завершается;
- Run и активный Node переходят в `cancelled`;
- отмена записывается в event log;
- повторный cancel конечного Run идемпотентен.

## 12. Шаблоны и данные

Шаблон не должен молча заменять неизвестную переменную пустой строкой. Целевое поведение — ошибка рендеринга, кроме явно необязательных значений.

Обязательные пространства имён:

- `input`;
- `feedback`;
- `nodes.<id>`;
- `loop.previous.<id>`;
- `approvals.<id>`;
- `artifacts`.

## 13. События

Минимальный набор v0.2:

```text
run.started
run.waiting
run.completed
run.failed
run.cancelled
node.ready
node.started
node.retry
node.completed
node.failed
node.skipped
hook.started
hook.completed
hook.failed
loop.iteration.started
loop.iteration.completed
approval.requested
approval.answered
assistant.started
assistant.completed
assistant.failed
artifact.created
```

Каждое событие содержит:

- timestamp;
- schema version;
- Run ID;
- Node ID при наличии;
- attempt и iteration при наличии;
- типизированные data;
- correlation ID для внешнего вызова.

## 14. Совместимость

- workflow и config сохраняют `apiVersion: takt/v1alpha1` до готовности мигратора;
- новые необязательные поля разрешены в рамках alpha;
- изменение существующей семантики требует записи в `ARCHITECTURE_DECISIONS.md`;
- Run хранит fingerprint workflow и config, чтобы resume не использовал незаметно изменённые файлы.
