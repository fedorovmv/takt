# Динамический fan-out управляемых дочерних Run в v0.1.28-alpha

## Назначение

Узел `workflow` теперь может запускать один управляемый дочерний Run на каждый элемент массива, полученного из структурированного output предыдущего узла. Это закрывает сценарии, в которых состав работы определяется во время выполнения: набор проверяющих агентов, истории PRD, найденные модули или список независимых задач.

Fan-out остаётся частью одного родительского узла, но каждый элемент получает отдельные state, events, artifacts, usage, cancellation marker и при необходимости Git worktree.

## Контракт YAML

```yaml
- id: discover
  command: select-reviewers
  output_format:
    type: object
    properties:
      reviewers:
        type: array
        items:
          type: string
    required: [reviewers]

- id: reviews
  depends_on: [discover]
  workflow:
    path: workflows/review-perspective.yaml
    input: |
      Perspective: ${reviewer}
      Position: ${fanout.index} of ${fanout.total}
    output_node: review
    isolation: inherit
    fan_out:
      items_from: nodes.discover.output.reviewers
      as: reviewer
      max_parallel: 5
      join: all_success
```

Поля `fan_out`:

- `items_from` — обязательный путь `nodes.<id>.output` или путь к вложенному полю JSON;
- `as` — имя переменной элемента, по умолчанию `item`;
- `max_parallel` — число одновременно выполняемых детей, по умолчанию `1`, максимум `64`;
- `join` — `all_success`, `all_done` или `one_success`, по умолчанию `all_success`;
- `allow_empty` — разрешает пустой массив; по умолчанию пустой список считается ошибкой маршрутизации.

Источник обязан быть upstream-зависимостью fan-out-узла. Runtime декодирует output через `json.Decoder.UseNumber` и требует массив в выбранной точке.

## Переменные элемента

В `workflow.input` доступны:

- `${fanout.item}` — текущий элемент: строка без кавычек, остальные типы как JSON;
- `${fanout.item.<path>}` — поле объекта или индекс массива;
- `${fanout.index}` — индекс с нуля;
- `${fanout.total}` — число элементов;
- `${<as>}` и `${<as>.<path>}` — алиас текущего элемента.

Обычные `${input}`, `${nodes.<id>.output}`, `${feedback}` и `$ARTIFACTS_DIR` продолжают работать.

## Устойчивое состояние и resume

До запуска детей родитель:

1. канонически кодирует список;
2. вычисляет fingerprint всего массива и каждого элемента;
3. создаёт Run ID для каждого индекса;
4. сохраняет все связи в состоянии и событии `child_run.fan_out.linked`.

При resume завершённые дети не запускаются повторно. Waiting- и незавершённые дети продолжаются под прежними ID. Изменение списка в рамках той же попытки отклоняется как небезопасное продолжение.

Retry родительского узла создаёт новую группу дочерних Run. Предыдущая группа остаётся в `child_runs` с номером попытки и доступна для аудита.

## Параллельность

Дети выполняются пакетами не больше `max_parallel`. Порядок завершения не влияет на порядок результата. Waiting-ребёнок не удерживает вычислительный слот после перехода в состояние ожидания.

`isolation: worktree` создаёт отдельный managed worktree для каждого ребёнка. `inherit` использует execution workspace родителя; параллельные изменяющие процессы в общем workspace должны быть запрещены самим workflow через изоляцию или read-only policy.

## Join и результат

Родитель ждёт terminal-состояния всех элементов:

- `all_success` — каждый ребёнок должен завершиться `completed`;
- `all_done` — родительский узел завершается успешно независимо от статусов детей;
- `one_success` — достаточно хотя бы одного `completed`, но итог формируется после завершения всей группы.

Output — JSON-массив в исходном порядке:

```json
[
  {
    "index": 0,
    "item": "code",
    "run_id": "...",
    "status": "completed",
    "output": "...",
    "usage": {
      "input_tokens": 120,
      "output_tokens": 40,
      "cost": 0.01
    }
  }
]
```

JSON-output ребёнка сохраняет тип. Остальной output становится строкой. Usage всех детей текущей попытки агрегируется в usage родительского узла.

## Approval и отмена

Если ждёт один ребёнок, прежний вызов через корневой Run остаётся доступен. Если ждут несколько детей, `takt answer` по родительскому узлу возвращает список Run ID и требует ответить конкретному ребёнку:

```bash
takt children <parent-run-id>
takt answer <child-run-id> <approval-node-id> --value approved
```

После ответа CLI автоматически продолжает parent chain. `takt cancel <child-run-id>` отменяет один элемент; результат определяется `join`. Отмена родительского Run распространяется на всю группу.

`takt children` показывает для fan-out-ребёнка индекс, исходный элемент, номер попытки и родительский узел.

## Использование в профиле code 0.7.0

`smart-review-block` теперь получает массив перспектив из структурированного классификатора и создаёт только нужные governed review Runs. `review-block` запускает пять стандартных перспектив тем же механизмом с `max_parallel: 5`.

Это устраняет прежнюю комбинацию условных статических ветвей и compile-time `foreach`: состав smart review теперь действительно определяется во время выполнения, а каждый проверяющий имеет отдельный lifecycle.

## Исправления предыдущего ревью

В этот же срез вошли:

- portable `scripts/test-worktree.sh` без `readarray`, совместимый со штатным Bash 3.2 macOS;
- OpenCode `read_only` всегда запрещает `write`, включая явный allowlist;
- рекурсивный merge вложенных секций `OPENCODE_CONFIG_CONTENT`;
- явная документация: Pi принимает только существующий path skill, OpenCode поддерживает path skills и именованные skills.

## Проверки

- unit-тесты ordered aggregation, bounded parallelism, multi-approval resume, `all_done`, changed-list protection и retry groups;
- CLI-контракт `scripts/test-child-fanout.sh` с неоднозначным approval, выборочной отменой и metadata `takt children`;
- профиль `code` использует runtime fan-out в smart и comprehensive review;
- схемы workflow и Run state включают `fan_out`, `child_runs`, fingerprint и множественное ожидание детей.

## Оставшиеся ограничения

- `one_success` пока не завершает группу досрочно и не отменяет остальные элементы автоматически;
- нет отдельной команды повторного запуска одного элемента внутри завершённой группы;
- script nodes и типизированные артефакты остаются следующим крупным срезом;
- OS sandbox и безопасное параллельное изменение одного общего workspace не реализованы.
