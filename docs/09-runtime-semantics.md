# Спецификация семантики runtime

Статус документа: целевой контракт v0.2. Семантика отказов, параллельных DAG-волн, `loop_group`, approval, fingerprints, persistence и per-attempt execution identity реализована к `v0.1.24-alpha`. Оставшиеся отличия перечислены в `05-implementation-status.md`.

## 1. Основные сущности

### Workflow

Версионированное описание DAG, циклов, hooks и approval.

### Run

Один запуск workflow с неизменяемыми входами, fingerprints определений и собственной историей событий.

### Node

Единица выполнения с одним типом действия, зависимостями, условием, попытками и hooks.

### Attempt

Один фактический запуск действия узла. Approval, переводящий Run в `waiting`, попытку не расходует.

### Execution record

Сохраняемая запись одного фактического вызова действия. Она содержит номер попытки, assistant, его версию, requested/resolved model, Session ID, usage и результат выполнения. Агрегированные поля Node описывают итог узла, а execution records сохраняют различия между retry.

### Iteration

Один проход `loop_group` с отдельными состояниями дочерних узлов.

## 2. Состояния Run

```text
running ──approval──> waiting ──answer──> running
   │                     │
   ├──success──────────> completed
   ├──failure──────────> failed
   └──cancel───────────> cancelled
```

`completed`, `failed` и `cancelled` — terminal-состояния.

Run не переходит в `failed` сразу после первого неуспешного узла. Scheduler сначала выполняет доступные ветви, включая `all_done`, затем вычисляет итоговый статус графа.

## 3. Состояния Node

```text
pending → running → completed
    │        │
    │        ├→ waiting → pending
    │        ├→ pending       retry
    │        ├→ failed        штатный отрицательный результат
    │        ├→ errored       ошибка запуска/runtime
    │        ├→ timed_out
    │        └→ cancelled
    ├────────→ skipped
    └────────→ blocked
```

Terminal-состояния:

- `completed`;
- `failed`;
- `errored`;
- `timed_out`;
- `cancelled`;
- `skipped`;
- `blocked`.

### Различие failure и error

- `failed` — действие выполнилось и вернуло отрицательный результат: ненулевой exit code, исчерпание цикла или явный `fail` hook;
- `errored` — действие не удалось корректно выполнить: отсутствующий бинарник, ошибка разрешения адаптера, внутренняя ошибка runtime;
- `timed_out` — истёк timeout узла;
- `cancelled` — внешний context был отменён.

## 4. Планирование DAG

Узел готов к запуску, когда:

1. все зависимости terminal;
2. `trigger_rule` разрешает запуск;
3. `when` вычисляется в `true`.

При `when == false` узел становится `skipped`.

### Trigger rules

#### `all_success`

Запуск, когда все зависимости `completed`. Иначе узел `skipped`.

#### `all_done`

Запуск после любых terminal-результатов зависимостей.

#### `none_failed_min_one_success`

Запуск, когда среди зависимостей нет failure-like состояний и хотя бы одна зависимость `completed`.

#### `one_success`

После terminal-состояния всех зависимостей узел запускается, если хотя бы одна зависимость `completed`. Правило предназначено в том числе для соединения взаимоисключающих условных ветвей.

Если после завершения доступных ветвей узел не может стать runnable, он получает `blocked` с кодом `unresolved_dependencies`.

### Параллельные волны

Scheduler собирает все готовые узлы текущего топологического слоя. Независимые `command`, `prompt` и `bash` без portable hooks и без повторных попыток переводятся в `running`, затем выполняются конкурентно. Результаты применяются в стабильном порядке определения узлов; persistence до и после волны остаётся сериализованным. Ошибка одного участника не отменяет остальные уже запущенные действия. Узлы с hooks или `attempts.max > 1` выполняются последовательным путём до расширения семантики параллельных retries.

## 5. Корневой и дочерний DAG

Корневой workflow и тело `loop_group` используют один scheduler и одинаковую семантику:

- `depends_on`;
- `when`;
- `trigger_rule`;
- hooks;
- attempts;
- timeout;
- классификация ошибок;
- `allow_failure`.

Различается только область состояния: после итерации child states копируются в `LoopPrevious` родительского узла и удаляются из активной карты. В `v1alpha1` вложенные `loop_group` запрещены; namespace состояния для произвольной вложенности отложен до отдельной версии контракта.

## 6. Попытки узла

Порядок попытки:

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
- сохраняет событие `node.retry`;
- при `session: fresh` очищает session ID;
- следующая попытка увеличивает счётчик;
- остановка на approval уменьшает счётчик обратно и сохраняется отдельным transition.

При исчерпании `attempts.max` узел становится `failed` с кодом `attempts_exhausted`.

## 7. Ошибки и `allow_failure`

Классы execution error:

- `exit`;
- `start`;
- `timed_out`;
- `cancelled`;
- `protocol`;
- `internal`.

`allow_failure: true` разрешает только `exit`. Он не скрывает `start`, timeout, cancellation, protocol или internal error.

Output, exit code, session ID и признак truncation сохраняются даже при неуспешном результате, если они доступны. Для `bash` stdout и stderr сохраняются раздельно; совместимое поле `output` остаётся объединённым представлением для шаблонов, feedback и диагностики. Структурные протоколы поверх bash, включая `takt-validation/v1alpha1`, декодируются только из stdout. Для агентного узла также сохраняются assistant, версия assistant, requested model и resolved model. Pi adapter использует `responseModel` последнего assistant message и только при его отсутствии берёт модель из `get_state`.

Usage каждой агентной попытки добавляется к aggregate `NodeState.usage`, а сама попытка записывается в `NodeState.executions`. Поэтому retry после внешней проверки не теряет стоимость и не приписывает usage предыдущих попыток последней модели. Различающиеся assistant/version/requested/resolved model образуют mixed execution identity.

Для process assistant с `takt-assistant/v1alpha1` OS exit code и envelope `exit_code` обязаны совпадать всегда. Расхождение классифицируется как `protocol` до применения `allow_failure`. Runtime также отклоняет дополнительный JSON, неизвестные поля, несовместимые status/exit, отрицательный usage и неподтверждённый resume.

## 8. Timeout и cancellation

`node.timeout` задаётся Go duration и ограничивает всю попытку: `before_node`, действие, `on_failure`, `after_node` и `before_complete`.

При timeout:

- активный process получает cancellation;
- на Unix завершается process group;
- Node становится `timed_out`;
- downstream `all_done` может выполниться;
- итоговый Run становится `failed`.

При отмене родительского context Node и Run становятся `cancelled`. Причина context имеет приоритет над одновременно обнаруженным output overflow: Node сохраняет `timed_out` или `cancelled`, а `output_truncated` остаётся дополнительным полем результата. Команда `takt cancel` остаётся задачей v0.2.

## 9. Hooks

Hooks выполняются последовательно:

1. глобальные workflow hooks;
2. локальные node hooks.

Результат hook:

- exit code 0 — продолжение;
- ненулевой exit code или transport error — применяется `on_failure`;
- timeout или cancellation hook немедленно завершают попытку как `timed_out` или `cancelled` и не превращаются в `hook_failed`;
- ошибка persistence при записи события/состояния немедленно возвращается вызывающему коду.

Решения:

- `continue`;
- `retry`;
- `fail`.

## 10. Loop group

Каждая итерация:

1. создаёт свежие child states;
2. сохраняет `loop.iteration.started`;
3. выполняет дочерний DAG общим scheduler;
4. копирует child states в `LoopPrevious`;
5. сохраняет `loop.iteration.completed`;
6. вычисляет `until`.

`until` вычисляется только для child node со статусом `completed`. `skipped`, `failed`, `errored`, `timed_out`, `cancelled` и `blocked` не удовлетворяют условию независимо от значения `exit_code`.

При выполнении `until` parent node становится `completed`. При исчерпании лимита parent node получает `failed/exit`.

Если attempt context родительского цикла завершён во время child execution, его причина имеет приоритет над производной ошибкой контейнера:

- deadline → parent Node `timed_out`;
- внешняя cancellation → parent Node и Run `cancelled`.

`loop_group exhausted` не может заменить эти состояния на `failed/exit`.

Approval внутри `loop_group` сохраняет `loop_iteration` и дочерние состояния. После `answer` scheduler продолжает ту же итерацию; после её завершения approval-state очищается перед следующей итерацией. Вложенный `loop_group` остаётся запрещён в `v1alpha1`.

## 11. Approval и безопасное продолжение

При первом выполнении approval:

1. Node и Run становятся `waiting`;
2. записывается `approval.requested`;
3. rollback счётчика попытки сохраняется как `node.suspended`;
4. CLI возвращает успешный JSON с состоянием waiting.

`takt answer`:

1. получает lock Run;
2. загружает state;
3. загружает и валидирует workflow/config/commands;
4. сравнивает fingerprints;
5. проверяет ожидаемый approval node;
6. атомарно сохраняет ответ и `approval.answered`;
7. продолжает Run.

При изменении определений ответ не потребляется. `takt resume` позволяет повторить продолжение после временной ошибки.

## 12. Структурированный вывод и JSON-пути

Если AI-узел объявляет `output_format`, успешный сырой output сначала декодируется как ровно одно JSON-значение, затем проверяется по schema subset и канонизируется. Ошибка декодирования, лишнее значение или нарушение схемы классифицируются как `protocol`; такой output не становится успешным результатом узла.

`when` и renderer разрешают путь `nodes.<id>.output.<field>` только после декодирования output как JSON. Поля объектов и индексы массивов читаются без преобразования всего workflow в отдельный task AST.

## 13. Persistence

Каждое изменение runtime проходит через `Store.Commit`:

- новому state и event присваивается одна revision;
- оба файла сначала записываются во временные файлы и синхронизируются;
- event log заменяется до state;
- `Load` сравнивает последние revision;
- рассогласование возвращает `store_inconsistent`.

Это не полная транзакционная БД, но повреждение не скрывается.

## 14. Fingerprints

Run хранит SHA-256:

- workflow;
- config;
- содержимого разрешённых Markdown-команд.

`answer` и `resume` блокируются при изменении любого определения.

## 15. YAML

Текущий loader поддерживает документированный subset, а не полную YAML 1.2. Block scalar сохраняет пустые строки и поддерживает chomp modes. Полная замена на внешнюю YAML-библиотеку не является обязательной для v0.2, пока subset явно ограничен и покрыт тестами.

## 16. JSON CLI

Машинный режим возвращает один документ:

- успех: `{"ok":true,"result":...}`;
- ошибка: `{"ok":false,"error":...}`.

Flag parser не печатает дополнительный текст в stderr.

## 17. Оставшаяся семантика v0.2

- строгие неизвестные template variables;
- `takt cancel`;
- normalized assistant protocol;
- session resume без тихого fallback;
- capabilities и per-node ограничения инструментов;
- расширение `output_format` до более полного JSON Schema;
- schema version, attempt и correlation ID как отдельные поля event.


## Композиция workflow

`subworkflow` и `foreach` разворачиваются до запуска в обычные DAG-узлы. Тот же компилятор работает внутри `loop_group`. Runtime сохраняет один scheduler, один Run и одну модель persistence; дочерние действия используют общую семантику hooks, retry, approval и ошибок.

Контейнер сохраняет публичный ID. Namespaced узлы с `__` остаются во внутреннем сохранённом состоянии, а CLI строит публичную проекцию без них. `waiting.node_id`, `current_node` и approvals отображаются через ID ближайшего контейнера. `takt answer` принимает этот публичный ID и сопоставляет его с фактическим approval-узлом.

Fingerprint workflow включает исходный родительский файл, каноническое скомпилированное определение, встроенные локальные Markdown-команды и исходные байты `foreach.items_from`. Изменение дочернего workflow, команды, входов, inline `items` или внешнего списка блокирует resume старого Run.

`assistant`, `model` и `session` контейнера становятся defaults подключённого вызова. Политики, требующие управления всей группой — `attempts`, `timeout`, hooks, `native_hooks` и `allow_failure` — остаются внутри дочернего workflow.

`foreach` при `parallel: false` связывает итерации последовательно, а при `parallel: true` создаёт независимые ветви от общего gate. Aggregator ждёт terminal-состояния всех итераций и собирает output в порядке исходного массива, а не в порядке завершения.

Рекурсивные ссылки отклоняются по стеку абсолютных путей. Глубина одновременно активной композиции ограничена 16 workflow.
