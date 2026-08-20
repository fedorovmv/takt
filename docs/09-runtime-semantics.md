# Спецификация семантики runtime

Статус документа: реализованный контракт `v0.1.64-alpha`. Он фиксирует единый
Archon-first Workflow language, durable loop/matrix evidence, immutable
assessments, exact session resume и recovery/retry поверх существующего
scheduler. Hard budgets и mutating fan-out остаются отдельными deferred срезами.

## 1. Основные сущности

### Workflow

Версионированное описание DAG, циклов, hooks и approval.

### Run

Один запуск workflow с неизменяемыми входами, fingerprints определений и собственной историей событий.

### Governed child Run

Отдельный Run, созданный узлом `workflow`. Он имеет собственные state/events/artifacts/usage и связан с родителем через `parent_run_id` и `parent_node_id`. Это lifecycle boundary, а не внутренний DAG `loop_group` и не security sandbox.

### Node

Единица выполнения с одним типом действия, зависимостями, условием, попытками и hooks.

### Attempt

Один фактический запуск действия узла. Approval, переводящий Run в `waiting`, попытку не расходует.

### Execution record

Сохраняемая запись одного фактического вызова действия. Она содержит номер попытки, assistant, его версию, requested/resolved model, Session ID, usage и результат выполнения. Агрегированные поля Node описывают итог узла, а execution records сохраняют различия между retry.

Для локального evaluation evidence execution record также сохраняет typed
`adapter` и опциональный `session_path`. Анализ evaluation запускается тем же
обычным Flow scheduler через read-only workflow; модельный вывод является
advisory и не участвует в deterministic outcome или benchmark metrics. Перед
persistence JSON/text evidence redacts configured secrets, а известный секрет в
binary evidence останавливает запись fail-closed.

### Control и execution workspace

Control workspace хранит определения, state/events, locks и artifacts. При worktree policy execution workspace указывает на отдельный Git worktree, где выполняются node actions. Абсолютный входной путь, указывающий на файл внутри control workspace, включая profile-generated `Source file` header, перед запуском node remap-ится на соответствующий путь execution workspace; это не позволяет assistant использовать служебный control-путь как рабочий.

### Iteration

Один проход `loop_group` с отдельными состояниями дочерних узлов. Завершённая итерация сохраняется в `NodeState.loop_iterations[]`; `loop_previous` остаётся compatibility view последней итерации. `max_iterations` ограничен 64, поэтому полная история остаётся bounded частью durable state.

### Matrix branch

Один последовательный проход вложенного DAG для канонического JSON item внутри
`matrix`. Это structural state того же Run, а не отдельный case Run.

### Assessment

Immutable typed artifact assessor Run, который связывает строгий validation
result и evidence с terminal `result_revision` target Run. Assessment качества
не меняет технический статус target.

## 2. Состояния Run

```text
running ──approval──> waiting ──answer──> running
   │                     │
   ├──pause request──> pausing ──safe boundary──> paused ──resume──> running
   ├──success────────> completed
   ├──failure────────> failed ──operator retry──> running
   ├──cancel─────────> cancelled
   └──abandon────────> abandoned
```

`completed`, `failed`, `cancelled` и `abandoned` — terminal-состояния. `paused` является durable non-terminal состоянием. Pause не прерывает attempt посередине provider/tool call: scheduler перестаёт запускать новые узлы и fan-out batches, активная попытка доходит до безопасной границы.

При terminal transition Run фиксирует `result_revision` равной revision события
`run.completed|failed|cancelled|abandoned`. Administrative commits после
результата увеличивают обычную `revision`, но не меняют этот pin. Operator retry
очищает его до нового terminal result.

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

Корневой workflow, тело `loop_group` и ветки `matrix` используют один scheduler
и одинаковую семантику:

- `depends_on`;
- `when`;
- `trigger_rule`;
- hooks;
- attempts;
- timeout;
- классификация ошибок;
- `allow_failure`.

Различается только область состояния: после итерации child states добавляются в `LoopIterations`, последняя итерация также копируется в совместимое `LoopPrevious`, после чего активные child states удаляются из карты. Вложенные `loop_group` запрещены в `v1alpha1`: path namespace уже поддерживает другие виды композиции, а рекурсивную loop-семантику не замораживаем без production evidence.

После matrix branch active child states копируются в `MatrixBranches` и также
удаляются. Вложенные `matrix` и `loop_group` внутри matrix запрещены;
`subworkflow`, `foreach`, governed `workflow`, approval, attempts и hooks
разрешены. Branches выполняются последовательно; parallel matrix не входит в
текущий контракт.

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

- сохраняет feedback и diagnostic fingerprint;
- сохраняет отдельную `ExecutionState` и событие `node.retry`;
- `attempts.retry_session: fresh` очищает Session ID, `reuse` сохраняет его;
- hook `on_failure.session: resume` также обязан вернуть тот же Session ID;
- следующая попытка увеличивает счётчик и durable `not_before` не пересчитывается
  после restart;
- остановка на approval уменьшает счётчик обратно и сохраняется отдельным
  transition.

При исчерпании `attempts.max` узел становится `failed` с кодом `attempts_exhausted`. Агрегированные output, Session ID и `resumed` остаются результатом последней фактической попытки и не обнуляются синтетическим terminal transition.

### Provider availability retry

Только assistant result с kind `provider_unavailable` получает отдельный
durable retry scope. Это строго доказанная transient provider evidence: HTTP
`429|502|503|504`; явно rate-limited, overloaded или temporarily unavailable
provider error; либо connection reset/refused, явное `connection error`,
temporary DNS или эквивалентный transport failure, когда request-side effect не
наблюдаем. Другие `4xx`,
malformed protocol, tool failure, context overflow, agent decision и unknown
external side effect не классифицируются так; cancellation и timeout parent
context сохраняют приоритет.

Одна workflow-попытка делает ровно три Takt `SessionAdapter.Run` calls максимум:
initial и до двух provider resume. Pi/OpenCode internal retries не считаются.
`ProviderAttempt` (1..3) и `ProviderAttempts` сохраняются отдельно от workflow
`Attempt`/`attempts.max`; session ID остаётся тем же. Default backoff — `2s`,
`4s`; adapter `Retry-After` заменяет delay, но Takt ограничивает его `60s`.
Перед ожиданием scheduler durable commits `Retry{scope: provider, not_before,
delay, provider_attempt, attempt_deadline}` и `provider.retry.scheduled`; перед повторным вызовом
— `provider.retry.ready`; третья provider failure — `provider.retry.exhausted`
и terminal `provider_unavailable`. `allow_failure` и workflow `retry_on` не
превращают это в product success.

`attempt_deadline` является исходным абсолютным deadline workflow-попытки.
Provider backoff и resume используют его без пересчёта, поэтому `node.timeout`
ограничивает суммарное время initial call, ожиданий, resume и hooks. Истечение
deadline во время backoff завершает узел как `timed_out` без нового adapter call.

Recovery сохраняет marker, deadline, provider ordinal и session. После restart
ожидающий узел не вызывается до persisted `not_before`; crash в in-flight
provider resume нормализуется обратно в pending с тем же marker/session, без
`worker_lost` и без decrement workflow attempt. Provider retry не применяется
к domain adapter actions или другим external side effects.

## 7. Ошибки и `allow_failure`

Классы execution error:

- `exit`;
- `start`;
- `timed_out`;
- `cancelled`;
- `protocol`;
- `configuration`;
- `internal`.

`allow_failure: true` разрешает только `exit`. Он не скрывает `start`, timeout,
cancellation, protocol, configuration или internal error. `configuration`
обозначает доказанную некорректную настройку adapter и не является допустимым
значением `attempts.retry_on`.

Output, exit code, session ID и признак truncation сохраняются даже при неуспешном результате, если они доступны. Для `bash` stdout и stderr сохраняются раздельно; совместимое поле `output` остаётся объединённым представлением для шаблонов, feedback и диагностики. Структурные протоколы поверх bash, включая `takt-validation/v1alpha1`, декодируются только из stdout. Для агентного узла также сохраняются assistant, версия assistant, requested model и resolved model. Pi adapter использует `responseModel` последнего assistant message и только при его отсутствии берёт модель из `get_state`.

Приоритет terminal classification строгий: context `timed_out`/`cancelled` сохраняет этот статус даже при output overflow; `allow_failure` применяется только к штатному `exit`; protocol, internal, start и unknown side effect не превращаются в допустимый failure.

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

При отмене родительского context Node и Run становятся `cancelled`. Причина context имеет приоритет над одновременно обнаруженным output overflow: Node сохраняет `timed_out` или `cancelled`, а `output_truncated` остаётся дополнительным полем результата. Для fan-out раннее решение join помечает ненужных siblings `cancel_reason: fanout_result_decided`, что не является пользовательской отменой.

`takt cancel` создаёт durable marker в каталоге Run. Ожидающий Run переводится в `cancelled` сразу; активная попытка проверяет marker и отменяет context вместе с process group. Запрос каскадируется по известным `child_run_ids`. Terminal children не изменяются.

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
4. вычисляет `until` для завершённой итерации;
5. сохраняет immutable snapshot child states в `LoopIterations`;
6. обновляет совместимый `LoopPrevious` последней итерацией;
7. сохраняет `loop.iteration.completed` с результатом `satisfied`.

`until` вычисляется только для child node со статусом `completed`. `skipped`, `failed`, `errored`, `timed_out`, `cancelled` и `blocked` не удовлетворяют условию независимо от значения `exit_code`.

`until.signal` требует ровно одно валидное occurrence ожидаемого uppercase
сигнала в `<promise>NAME</promise>` либо в последней непустой строке. Fenced
Markdown не учитывается. Отсутствие сигнала сохраняется как
`signal_diagnostic: signal_missing`, повторное/неоднозначное occurrence — как
`signal_ambiguous`; измеренный сигнал и отсутствие сигнала сериализуются как
`null`/строка, а не как пустой текст. Если source output truncated, signal
predicate завершается `protocol` до проверки содержимого.

`until.requires` проверяет дополнительные evidence только узлов со статусом
`completed`; failed/errored/timed_out/cancelled terminal states не участвуют в
acceptance.
Обычное несовпадение даёт следующую bounded iteration; отсутствующий или
не-terminal required evidence — `required_evidence_missing` protocol error.
`until_bash` выполняется после child DAG, не получает скрытого JSON validator
контракта и сохраняет `PredicateEvidence` (stdout, stderr, exit code, duration,
terminal status, truncation, error code) внутри immutable iteration snapshot.

Любая ошибка вычисления predicate сохраняет текущую итерацию до возврата
ошибки. Failure-like child body node делает safe-stop и не передаёт его exit
code/output в acceptance.

При выполнении `until` parent node становится `completed`. При исчерпании лимита parent node получает `failed/exit`.

Если attempt context родительского цикла завершён во время child execution, его причина имеет приоритет над производной ошибкой контейнера:

- deadline → parent Node `timed_out`;
- внешняя cancellation → parent Node и Run `cancelled`.

`loop_group exhausted` не может заменить эти состояния на `failed/exit`.

Approval внутри `loop_group` сохраняет `loop_iteration` и дочерние состояния. После `answer` scheduler продолжает ту же итерацию; после её завершения approval-state очищается перед следующей итерацией. `fresh_context: true` очищает Session ID для следующей итерации, а `false` сохраняет каждый совместимый assistant Session ID и требует exact resume. `context: shared` ищет транзитивного единственного upstream assistant ancestor с тем же provider/model и не имеет fresh fallback. Вложенный `loop_group` остаётся запрещён в `v1alpha1`.

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

Если корневой Run ждёт `kind: child_run`, `takt answer` проходит по waiting-ссылкам до фактического approval. После ответа сначала продолжается ребёнок, затем каждый родитель до корня. Отдельные locks берутся последовательно на конкретные Run; state разных Run не сливается.

## 12. Структурированный вывод и JSON-пути

Если AI-узел объявляет `output_format`, runtime добавляет его точный `takt-schema-subset/v1` contract к отрендеренному prompt, а успешный сырой output затем декодируется как ровно одно JSON-значение, проверяется по тому же contract и канонизируется. Workflow JSON `input.schema` использует тот же validator; полный JSON Schema не заявляется. Ошибка декодирования, лишнее значение или нарушение схемы классифицируются как `protocol`; такой output не становится успешным результатом узла.

`when` и renderer разрешают путь `$<id>.output.<field>` только после
декодирования output как JSON. Поля объектов и индексы массивов читаются без
преобразования всего workflow в отдельный task AST. Один `internal/flowref`
parser также обслуживает `$INPUTS.*`, `$LOOP_PREV.*`, `$FANOUT.*`, approval
output и shell/script escaping; `${...}` legacy forms fail closed.

Workflow с `input.format: json` читает root поля через
`$INPUTS.<field>[.<path>]`. В matrix body доступны `$MATRIX.item`,
`$MATRIX.index`, `$MATRIX.total` и alias `$<as>` из `matrix.as`. `script.stdin`
рендерится через NonShell surface и передаётся дочернему процессу byte-for-byte.

### Matrix resume и immutable branches

Перед branch 0 runtime разрешает и канонизирует весь `items_from`, отклоняет
более 1024 элементов и canonical duplicates, проверяет все динамические child
workflow identities. Container сохраняет `matrix_fingerprint`, active index и
`matrix_branches[]`; каждый completed branch содержит item fingerprint,
структурный snapshot nodes, output, child workflow identities и при наличии
primary assessment ID.

Активные nodes существуют в общем state только во время текущей ветки. После
completion они копируются в snapshot и удаляются; их канонический NodePath имеет
вид `/cases[0]/validate`. Crash/resume продолжает active branch, не повторяет
completed branches и fail-closed возвращает `matrix_items_changed` при drift.
Approval сохраняет active index и продолжает ту же ветку.

### Assessment capture и stale semantics

`assessment` action загружает terminal target из того же Store, pin-ит его
`result_revision`, строго декодирует `takt-validation/v1alpha1`, проверяет
primary provenance и evidence checksums, атомарно сохраняет
`takt-assessment/v1alpha1` artifact в assessor Run и только затем завершает
Node. Assessment ID включает node/attempt, matrix scope и immutable target
Run ID/result revision, поэтому operator retry не может переиспользовать
старую оценку после сброса attempt namespace. Primary result принимается от `bash|script|adapter` или от выбранного
детерминированного producer governed workflow; assistant result допустим только
как advisory.

`valid:false` является успешно измеренным результатом. Malformed result,
nonterminal target, invalid provenance, missing/corrupt evidence и persistence
failure являются execution error. Target state не изменяется. Query вычисляет
`stale` сравнением сохранённого pin с текущим target `result_revision`; corrupt
artifact возвращает `assessment_corrupt`, а не пропускается.

## 13. Persistence

Каждое изменение runtime проходит через `Store.Commit`:

- новому state и event присваивается одна revision;
- оба файла сначала записываются во временные файлы и синхронизируются;
- event log заменяется до state;
- `Load` сравнивает последние revision;
- рассогласование возвращает `store_inconsistent`.

Terminal Run commit одновременно устанавливает `result_revision`.
Последующие cleanup/worktree/notification commits её не меняют. Для legacy Run
assessment query восстанавливает pin по последнему terminal Run event и
fail-closed возвращает `target_revision_unavailable`, если такого event нет.

Это не полная транзакционная БД, но повреждение не скрывается.

## 14. Fingerprints

Run хранит SHA-256:

- workflow;
- config;
- содержимого разрешённых Markdown-команд;
- статически подключённых governed child definitions в fingerprint родителя.

Каждый ребёнок дополнительно хранит собственные fingerprints. `answer` и `resume` блокируются при изменении любого определения. Рекурсивные `workflow`-ссылки отклоняются, глубина ограничена 16.

## 15. YAML

Loader принимает target root `name`/`nodes` и target node `provider`/`context`.
Legacy Workflow root (`apiVersion`, `kind`, `metadata`, `defaults`, `assistant`)
не имеет compatibility parse path и отклоняется до Run. YAML syntax принадлежит
`go.yaml.in/yaml/v3`; Takt сохраняет только strict JSON-tag contract diagnostics.

## 16. JSON CLI

Машинный режим возвращает один документ:

- успех: `{"ok":true,"result":...}`;
- ошибка: `{"ok":false,"error":...}`.

Flag parser не печатает дополнительный текст в stderr.

## 17. Оставшаяся семантика v0.2

- расширение `run inspect` отдельными iteration/evidence projections;
- hard token/tool budgets после live capability proof Pi/OpenCode;
- mutating merge fan-out после отдельной merge action и threat-model.
- финальная `v1alpha1 → v1beta1` migration после production evidence;
- live compatibility evidence для guarded host integrations и внешних wrappers;
- новые schema keywords только как явное совместимое расширение `takt-schema-subset/v1`, если их потребует evidence.


## Композиция workflow

`subworkflow` и `foreach` разворачиваются до запуска в обычные DAG-узлы. Тот же компилятор работает внутри `loop_group`. Runtime сохраняет один scheduler, один Run и одну модель persistence; дочерние действия используют общую семантику hooks, retry, approval и ошибок.

Контейнер сохраняет публичный ID. Namespaced узлы с `__` остаются во внутреннем сохранённом состоянии, а CLI строит публичную проекцию без них. `waiting.node_id`, `current_node` и approvals отображаются через ID ближайшего контейнера. `takt answer` принимает этот публичный ID и сопоставляет его с фактическим approval-узлом.

Fingerprint workflow включает исходный родительский файл, каноническое скомпилированное определение, встроенные локальные Markdown-команды и исходные байты `foreach.items_from`. Изменение дочернего workflow, команды, входов, inline `items` или внешнего списка блокирует resume старого Run.

`provider`, `model` и `context` контейнера становятся defaults подключённого
вызова. Политики, требующие управления всей группой — `attempts`, `timeout`,
hooks, `native_hooks` и `allow_failure` — остаются внутри дочернего workflow.

`foreach` при `parallel: false` связывает итерации последовательно, а при `parallel: true` создаёт независимые ветви от общего gate. Aggregator ждёт terminal-состояния всех итераций и собирает output в порядке исходного массива, а не в порядке завершения.

Рекурсивные ссылки отклоняются по стеку абсолютных путей. Глубина одновременно активной композиции ограничена 16 workflow.


## Governed child Run lifecycle

Узел `workflow` не компилируется в DAG родителя. При первой попытке родитель создаёт Child Run ID, сохраняет link event и запускает отдельный Runner поверх того же file store.

Состояние дерева:

- ребёнок: `parent_run_id`, `parent_node_id`;
- родитель: direct `child_run_ids`;
- узел: current `child_run_id` и история child attempts;
- ожидание: `kind: child_run`, `child_run_id` и публичный parent node ID.

Результат terminal child Run становится execution result родительского узла. Usage ребёнка агрегируется как usage этой попытки. Failure и cancellation не маскируются. Если parent attempts повторяют узел, terminal ребёнок не переиспользуется: создаётся новый child Run, а предыдущий сохраняется.

Изоляция ребёнка определяется `workflow.isolation`: собственная policy, `inherit`, `worktree` или `none`. `inherit` разделяет execution workspace с родителем, но state/events/artifacts остаются раздельными.

Одиночные governed nodes не входят в обычную параллельную DAG-волну. Для управляемой конкурентности нескольких детей используется `workflow.fan_out`; он задаёт явную границу группы и join policy.

### Dynamic governed child fan-out

`workflow.fan_out` разрешает массив из `$<id>.output...` после успешного
upstream-узла. До запуска родитель фиксирует fingerprint списка, индекс,
канонический item и Run ID каждого ребёнка. При resume terminal-дети
переиспользуются, waiting-дети продолжаются, а изменение списка блокирует
запуск.

`max_parallel` ограничивает число одновременно исполняемых детей. Join формируется после terminal-состояния группы и поддерживает `all_success`, `all_done`, `one_success`. Output агрегируется в исходном порядке; usage детей входит в usage родительского узла. `WaitingState.ChildRunIDs` хранит множественное ожидание. Ответ через родителя разрешён только при одном waiting-ребёнке; при нескольких CLI требует выбрать child Run.

Retry родительского узла создаёт новую группу с новыми Run ID и сохраняет предыдущие попытки. Отмена отдельного ребёнка отражается в агрегате, отмена родителя каскадируется всей группе.

## Script execution и typed artifact lifecycle

`script` является обычным детерминированным действием scheduler. Execution context, timeout, cancellation, worktree mapping и сохранение stdout/stderr совпадают с `bash`, но процесс запускается без `bash -lc`: `command` — напрямую, `python` — через `python3`, `node` — через `node`. Structured output проверяется до фиксации terminal-состояния.

После успешного действия и hooks runtime обрабатывает `output_type`. Источником является нормализованный Output либо файл `output_path`. Файл копируется в `.takt/runs/<run>/artifacts/nodes/<node>/<attempt>/`, после чего вычисляются SHA-256 и размер. Только сохранённый снимок считается артефактом; временный producer path не является контрактом downstream-узлов. Ошибка capture завершает узел как `artifact`, поэтому completed-узел всегда содержит валидные ссылки.

`node.completed` включает metadata артефактов. `RunState.Artifacts` агрегирует ссылки без потери hidden structural nodes. Governed child Run возвращает свои ссылки как часть execution result родительского узла, а fan-out сохраняет их в каждом item state. Сам файл остаётся в store фактического producer Run.

Renderer разрешает `$<id>.artifacts.<type>.<field>` с именованным artifact type;
числовой index и positional artifact forms запрещены. Resume использует
сохранённый path/checksum, а изменение script source или dependency блокируется
definition fingerprint до исполнения.

## Managed Git worktree

Workflow-level `worktree.enabled` creates a branch and worktree before actions start. For structural subworkflow, a hidden gate can activate its policy before child nodes. The smart router now starts the selected process as a governed child Run; by default that child applies its own workflow policy. CLI overrides are persisted in Run state.

Only a clean successful `on_success` worktree is removed automatically. An unchanged branch whose head still equals the recorded base commit is deleted; a branch with commits is preserved. Dirty, failed, cancelled, waiting, manual, or cleanup-error worktrees remain. The runtime never deletes uncommitted changes automatically. This boundary isolates local code changes but is not a sandbox.

## Политики AI-узлов

Эффективная политика вычисляется до вызова adapter. Локальные ограничения объединяются с inherited policy: deny-списки складываются, allowlist и список skills пересекаются как верхние границы, `read_only` и `network: deny` наследуются как наиболее строгие значения, а inherited MCP нельзя незаметно заменить. Явные пустые `allowed_tools: []` и `skills: []` сохраняются как запрет, а не трактуются как отсутствие настройки.

Adapter публикует capabilities. Если хотя бы одна необходимая capability отсутствует, узел завершается до запуска процесса. Эффективная политика и список capabilities сохраняются в `NodeState.policy`; inherited policy сохраняется в child Run. Policy resources входят в definition fingerprint.


## Authoring, always_run и idle_timeout

Workflow загружается fail-closed. Неизвестные поля получают path-aware
подсказку; обязательная `$path` должна разрешиться, `$path?` допускает
отсутствие, `$path:-default` подставляет fallback. До Run статически
проверяются upstream output/artifact references и capabilities локальных
adapters. Shell surface quote-ит подставленное значение одним аргументом,
передаёт reserved env refs через environment и сохраняет native `$?`, `$1`,
`$$`, `$((...))`, `$(...)`.

`always_run` ждёт terminal-состояния всех зависимостей и затем становится runnable независимо от их успеха. Cleanup не меняет итог failed Run на completed. `idle_timeout` измеряет отсутствие нормализованной assistant activity, а не полное wall-clock время попытки; для Pi activity включает terminal tool/message events и transient `message_update`/`tool_execution_update`, которые не становятся durable events. Общий `timeout` остаётся верхней границей. Blocking tool approval не считается зависанием внешнего worker.

## Local daemon semantics

Daemon является дополнительным владельцем времени жизни `control.Service`. Unix socket, metadata и lock лежат в `.takt/`; файловый Store остаётся источником истины. Concurrent clients не пишут state напрямую и используют bounded ожидание per-Run lock. Event subscription передаёт события после revision как NDJSON. Shutdown прекращает daemon и ожидает служебные monitor goroutines. При следующем старте daemon выполняет PID-based recovery локальных `running|pausing` Run: текущая attempt получает diagnostic `worker_lost`, её счётчик возвращается, node снова становится `pending`, а child Runs восстанавливаются раньше родителей. Внешний side effect может повториться, поэтому adapter/workflow обязан обеспечивать идемпотентность критичных операций.

## MCP control-plane semantics

MCP adapter не создаёт альтернативную модель состояния. Каждый tool вызывает общий local control service и использует тот же `Runner`, `FS`, lock, fingerprint, child lifecycle и worktree policy, что CLI.

Detached start генерирует Run ID до запуска goroutine и возвращает его после появления durable state либо ранней ошибки запуска. При прямом `takt mcp` жизненный цикл ограничен процессом MCP. При `takt daemon` Run принадлежит отдельному локальному процессу и продолжается после закрытия клиента; daemon restart не продолжает тот же OS-процесс, но восстанавливает его durable Run как новую attempt через PID-based recovery.

`takt.run.events` использует монотонный `Event.Revision` как cursor: ответ содержит только события с revision больше `after_revision`, сохраняет порядок журнала и ограничивает число элементов. `wait_ms` реализует bounded polling и не меняет event store.

JSON-RPC cancellation отменяет контекст текущего MCP-запроса. Отмена самого Run выполняется только явным `takt.run.cancel`, сохраняется в store и каскадируется governed children по общей runtime-семантике.


## Runtime reliability and local security (v0.1.44)

`attempts.backoff` is scheduler state, not a sleep hidden inside an adapter. After a retryable failure Takt records `NodeState.retry.next_attempt/not_before/delay/kind/fingerprint`, commits `node.retry.scheduled`, and waits while continuing to honor pause/cancel. When the deadline is reached the marker is cleared by `node.retry.ready`. A restart therefore consumes the stored deadline instead of choosing a new jitter value.

Execution failures also receive a normalized `DiagnosticState`. The human message is preserved, while the fingerprint is computed from `code/kind/op` and a normalized message with control/execution workspace paths and volatile process numbers removed. `ExecutionState.diagnostic` keeps per-attempt history; `NodeState.diagnostic` describes the current terminal/retry failure and is cleared after success.

Governed fan-out short-circuits inside the existing scheduler. `one_success` cancels remaining work after the first success; `all_success` cancels remaining work after the first failure-like result. Children not yet started are marked cancelled without execution. Running siblings receive context cancellation. Parent records use `cancel_reason=fanout_result_decided`, so operator cancellation remains distinguishable.

`NodeState.path` is a canonical structural namespace generated from the expanded node ID. It does not replace the compatible ID/key in v1alpha1; state/events can use the path for diagnostics/evidence while existing templates continue to address node IDs.

Secret protection is applied at the persistence boundary. The live executor may temporarily hold the resolved value of `secret://ENV_NAME`; `Runner.commit` clones state, redacts known values, then writes only the redacted clone and event data. Text artifacts are redacted before hashing/persistence; a non-text artifact containing a known secret fails closed. Public control/CLI responses reload the Run from Store after foreground execution instead of exposing the transient live state. SecretRef itself, rather than a resolved value, is the resumable source of truth.

OS sandbox enforcement is intentionally narrower than assistant policy. `command/prompt` sandbox remains adapter-enforced. For local `bash/script` nodes, `sandbox.enforcement=required|optional` wraps every deterministic process (including validation runtime and hooks) with `bubblewrap` on Linux or `sandbox-exec` on macOS when available. `required` fails before payload execution when no supported backend is available; `optional` records a degraded decision and continues. This does not create an untrusted multi-user runtime.

## Dynamic plan revisions

`WorkflowPlan` не исполняется напрямую. Проверенная редакция компилируется в обычные governed workflow-сегменты. Checkpoint отделяет immutable завершённую историю от ещё не начатого хвоста. Steering применяется только планировщиком checkpoint. Новая редакция может заменить оставшиеся фазы, но не статус, output, events, usage или artifacts завершённых Run.

`when` поддерживает сравнения `==`/`!=` и ограниченные логические связки `&&`/`||`; `&&` имеет более высокий приоритет. Выражения не являются общим языком программирования.

## Autonomous Run operations

Реестр `run.list` строится из файлового Store и отличает фактический статус от вычисленного `pausing|abandoning`. `run.attention` выделяет approval, question, tool approval, failed и paused Run. `run.summary` агрегирует descendant Runs, usage и artifacts без изменения исходных state.

`run.retry` сбрасывает failed node и его зависимый хвост, сохраняя `operator_retries`. `run.fork` создаёт новый Run или новый Dynamic Plan; `run.abandon` формирует отдельное terminal-состояние. Notification dispatcher сравнивает durable snapshots, записывает дедуплицированные items в inbox и затем пытается доставить их sinks; inbox остаётся источником истины независимо от успеха desktop/process delivery.


## Domain adapter execution

`adapter` является ещё одним детерминированным Node action внутри существующего scheduler. Перед `Invoke` runtime разрешает adapter из config, получает declaration и проверяет требуемую нейтральную capability. Поэтому неизвестная или неподдерживаемая операция завершается до внешнего side effect.

Для `side_effect.mode: reconcile` adapter обязан объявить reconcile для этой операции до первого mutating-вызова. Неопределённый transport/result не переводится в обычный retry: runtime сначала вызывает `Reconcile` с тем же durable idempotency key и receipt. `applied` принимается как завершённый эффект, `not_applied` допускает один повтор, `unknown` классифицируется как `external_state_unknown` и требует внешнего разрешения.

Process и MCP являются transport-реализациями одной границы. Workflow и scheduler не знают имён провайдеров; `NodeState.domain_operation` сохраняет фактически обнаруженные capabilities и reconciliation provenance. Эта модель продолжает ADR-063 и не вводит отдельный integration runtime.

## Multi-repo repository child lifecycle

`v0.1.43-alpha` разрешает governed child Run выбрать `workflow.repository`. Runtime разрешает repository path только внутри общего control workspace, повторяет проверку после `EvalSymlinks` и использует выбранный Git repository как child control workspace. `isolation: worktree` создаёт отдельный repository worktree; он удерживается до завершения Dynamic Plan, чтобы downstream repositories, integration verification и evidence могли читать фактический candidate. Parent `NodeState` сохраняет child control/execution workspace, branch и base commit.

Repository-aware Dynamic Plan не меняет retry/replan semantics: completed repository phase остаётся completed, а `PendingPhases` и operator retry работают с незавершённым dependency tail. Если retry родительского workflow-узла вызван post-child hook/check, уже completed governed child переиспользуется; новый child создаётся только для неуспешной предыдущей попытки. Per-repository Git diffs агрегируются с prefix repository ID и проходят тот же post-action declared-change gate.
