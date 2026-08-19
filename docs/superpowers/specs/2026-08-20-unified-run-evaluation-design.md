# Unified Run Evaluation: обычный Run, произвольный DAG и assessments

Статус: **READY FOR REVIEW**
Дата: 2026-08-20

## 1. Решение

Evaluation перестаёт быть внешним циклом вокруг нескольких Run. Новая
evaluation является обычным durable Run на существующих scheduler и Store:

```text
evaluation Run
  └─ matrix (case/repeat branches одного Run)
       ├─ произвольные preparation/assistant/workflow/check/evidence nodes
       ├─ optional candidate Run(s) через обычный workflow node
       └─ ровно один primary assessment на ветку
```

Фиксированной последовательности `candidate → validator` нет. Автор может
расположить несколько моделей, детерминированных проверок, review, evidence и
advisory assessments в любом DAG. Runtime знает только две новые декларативные
семантики:

1. `matrix` повторяет вложенный DAG для JSON items внутри того же Run;
2. `assessment` неизменяемо связывает результат проверки и evidence с точной
   terminal revision оцениваемого Run.

Статус и качество разделены:

```text
Run.status = удалось ли выполнить измерение
Assessment = что измерено
CLI exit   = прошли ли явно выбранные gates
```

`valid: false` не превращает технически успешное измерение в failed Run. Crash
валидатора, malformed result, отсутствие primary assessment/evidence или ошибка
persistence делают evaluation Run `failed`. Gate failure возвращает non-zero
после durable записи Run и assessments, но не меняет `Run.status=completed`.

## 2. Проектный шлюз design-unknowns

**Статус:** READY
**Уверенность:** высокая для модели данных и runtime; средняя для длительности
compatibility-периода.

Открытых P0 нет. Compatibility старых report directories остаётся ограниченным
P1: они читаются, но не становятся новым источником истины и не переписываются
в Run Store задним числом.

### 2.1. Реестр неизвестных

| ID | Неизвестное | Приоритет | Решение | Статус |
|---|---|---:|---|---|
| U-01 | Может ли quality failure делать Run failed | P0 | Нет: status измеряет исполнение, assessment — качество, CLI — gates | закрыт |
| U-02 | Что является target assessment | P0 | Terminal Run и его immutable result revision; не свободный workspace и не node text | закрыт |
| U-03 | Нужен ли отдельный case Run | P0 | Нет; case/repeat — matrix branch одного evaluation Run | закрыт |
| U-04 | Обязательны ли candidate и validator stages | P0 | Нет; DAG произвольный, обязательна только запись primary assessment | закрыт |
| U-05 | Как не делать assessment stale после cleanup | P0 | Run получает `result_revision`, которая меняется только при новом terminal result, но не при administrative cleanup | закрыт |
| U-06 | Где хранить поздние assessments без изменения target | P0 | Typed artifact в assessor Run; query ищет его по target/assessor relation | закрыт |
| U-07 | Как доказать authoritative verdict | P0 | Primary result должен происходить из deterministic `bash/script/adapter`; assistant result только advisory | закрыт |
| U-08 | Как задаются cases/repeats без suite-языка | P1 | `takt eval flow` materializes versioned JSON input; `matrix` читает его как обычный workflow input | закрыт |
| U-09 | Как сохранить старые Make-команды и отчёты | P1 | Make переходит на Run ID; старые output directories остаются read-only compatibility input | ограничен |
| U-10 | Нужен ли assessment index/DB | P2 | Нет; local Store сканирует typed artifacts. Индекс добавляется только при измеренной проблеме | отложен |
| U-11 | Нужен ли parallel matrix | P2 | Нет в первом контракте; ветки последовательны, как текущий flow eval | отложен |

### 2.2. Условия возврата к проектированию

Реализацию нужно остановить и вернуть на design-unknowns, если код докажет хотя
бы одно из следующего:

- terminal result Run нельзя отделить от administrative revisions без второго
  источника истины;
- вложенный DAG нельзя исполнить существующим `executeGraph` без второй
  scheduler-семантики;
- target/evidence нельзя сохранить атомарно до `assessment` node completion;
- current mini-du corpus нельзя выразить произвольным DAG без скрытого
  фиксированного candidate/validator engine;
- migration требует читать или изменять внешний пользовательский workspace.

## 3. Цели и границы

### 3.1. Цели

- любой Run самодостаточен по status, nodes, attempts, usage, artifacts и
  внутренним проверкам;
- assessment можно добавить тем же workflow или отдельным поздним assessor Run;
- evaluation является одним родительским Run с case/repeat branches;
- suite authoring использует обычный `takt/v1alpha1` Workflow;
- validator может быть произвольным workflow, а не одним process callback;
- `status`, `stats`, `inspect` и `assessment` работают по Run ID одинаково;
- текущие true/false accept/reject, identity, metrics и gates сохраняют смысл;
- пользовательский путь не сложнее текущего `suite.yaml + cases + validator`.

### 3.2. Не входит в изменение

- server, DB, remote queue или multi-user permissions;
- LLM judge как authoritative validator;
- новый plugin framework;
- parallel matrix branches в первом срезе;
- автоматическая повторная оценка stale targets;
- изменение production workflow в
  `/Users/fedorov.m.v/work/sbt/micro-mono/micro-spec-coder/.worktrees/direct-route-agent-rebuilt/`;
- параллельный live `EVAL_PRESET=qwen38 EVAL_IDLE_TIMEOUT=15m make eval-feature`.

## 4. Почему выбран этот вариант

Рассматривались три варианта.

### A. Обычный Workflow + `matrix` + `assessment` — выбран

Плюсы:

- один scheduler, Store, lifecycle и CLI;
- DAG не имеет предопределённых стадий;
- case/repeat не становятся искусственными Run;
- поздняя оценка использует тот же artifact contract;
- текущий `loop_group` уже доказывает nested DAG snapshots/resume.

Цена — два небольших расширения языка и миграция eval tooling в application
queries.

### B. Скрытый parent Run поверх текущего Go evaluator loop — отклонён

Он дал бы Run ID, но preparation, validator, evidence и gates остались бы второй
машиной состояний в `internal/tooling/evaluation`. Такой parent не был бы
самодостаточным, а authored DAG оставался бы фикцией.

### C. Один child Run на каждый case/repeat — отклонён

Это проще механически, но создаёт lifecycle boundary там, где нужна только
структурная ветка. Case Run дублирует status/usage/artifacts candidate Run и
сохраняет прежнее расхождение `run inspect` с `eval inspect`.

## 5. Целевая модель данных

### 5.1. Result revision Run

В `RunState` добавляется:

```json
{
  "result_revision": 42
}
```

`result_revision` устанавливается revision terminal transition
`completed|failed|cancelled|abandoned`. Administrative commits после terminal
result — например `worktree.removed` — увеличивают обычную `revision`, но не
меняют `result_revision`.

Operator retry очищает старую result revision перед продолжением и записывает
новую при следующем terminal transition. Assessment предыдущей result revision
после этого становится stale.

Для старых Run значение восстанавливается по последнему terminal run event.
Если event недоступен, assessment такого legacy Run fail-closed возвращает
`target_revision_unavailable`.

### 5.2. Assessment artifact

Новый immutable envelope:

```json
{
  "protocol_version": "takt-assessment/v1alpha1",
  "type": "assessment",
  "id": "assessment-...",
  "role": "primary",
  "target": {
    "run_id": "run-candidate",
    "revision": 42,
    "status": "completed",
    "workflow_fingerprint": "...",
    "config_fingerprint": "..."
  },
  "assessor": {
    "run_id": "run-evaluation",
    "node_id": "assess",
    "revision": 81
  },
  "scope": {
    "case_id": "implement-basic",
    "repeat": 1
  },
  "result": {
    "protocol_version": "takt-validation/v1alpha1",
    "type": "validation_result",
    "valid": false,
    "diagnostics": []
  },
  "outcome": "false_accept",
  "evidence": [
    {
      "producer_run_id": "run-evaluation",
      "artifact_id": "evidence-...",
      "sha256": "..."
    }
  ],
  "created_at": "2026-08-20T00:00:00Z"
}
```

Правила:

- `role` — `primary|advisory`;
- target обязан быть terminal Run из того же local Store;
- `revision` берётся runtime из `target.result_revision`, автор её не вводит;
- `(assessor_run_id, node_id, branch, attempt)` создаёт не более одного
  assessment; resume возвращает существующий artifact;
- assessments не перезаписываются;
- primary обязан иметь `case_id`, положительный `repeat` и хотя бы один
  immutable evidence reference;
- advisory может использовать свободный scope и не влияет на gates/metrics;
- `stale` не сохраняется в artifact, а вычисляется сравнением сохранённой и
  текущей `result_revision` target;
- artifact type — `assessment`, MIME —
  `application/vnd.takt.assessment+json`;
- target Run не изменяется.

### 5.3. Outcome

Outcome вычисляется и сохраняется при создании artifact:

| Target Run | Validation | Outcome |
|---|---|---|
| `completed` | `valid:true` | `true_accept` |
| `completed` | `valid:false` | `false_accept` |
| не `completed` | `valid:false` | `true_reject` |
| не `completed` | `valid:true` | `false_reject` |

Infrastructure error не является пятым quality outcome. Он означает, что
primary assessment не был корректно создан, и поэтому evaluation Run failed.

## 6. Workflow authoring

### 6.1. `matrix`: общий structural node

`matrix` — не evaluation engine. Это bounded runtime-resolved structural
container, аналогичный `loop_group`, который исполняет вложенный DAG для JSON
items в одном Run.

```yaml
- id: cases
  matrix:
    items_from: $INPUTS.cases
    as: case
    nodes:
      - id: candidate
        workflow:
          path: $MATRIX.item.workflow_path
          repository: $MATRIX.item.repository
          input: $MATRIX.item.input_path
          isolation: worktree
          keep_worktree: true

      - id: validate
        depends_on: [candidate]
        trigger_rule: all_done
        script:
          runtime: command
          path: ../validator
          stdin: |
            {
              "protocol_version":"takt-validation-request/v1alpha1",
              "case_id":"$MATRIX.item.case_id",
              "repeat":$MATRIX.item.repeat,
              "target_run_id":"$candidate.child_run_id",
              "workspace":"$candidate.child_execution_workspace",
              "expected_path":"$MATRIX.item.expected_path"
            }
        allow_failure: true

      - id: evidence
        depends_on: [candidate, validate]
        trigger_rule: all_done
        script:
          runtime: command
          path: ../collect-evidence
          args:
            - $candidate.child_run_id
            - $candidate.child_execution_workspace
            - $ARTIFACTS_DIR/evaluation-evidence.tar
        output_type: evaluation-evidence
        output_mime: application/x-tar
        output_path: $ARTIFACTS_DIR/evaluation-evidence.tar

      - id: assess
        depends_on: [validate, evidence]
        trigger_rule: all_done
        assessment:
          role: primary
          target_run_id: $candidate.child_run_id
          result_from: $validate.output
          scope:
            case_id: $MATRIX.item.case_id
            repeat: $MATRIX.item.repeat
          evidence:
            - $evidence.artifacts.evaluation-evidence
    output_node: assess
```

Контракт первого среза:

- `items_from` — обязательная ссылка на JSON array из upstream output или
  workflow JSON input;
- `as` — identifier, default `item`;
- `nodes` — непустой вложенный DAG с обычными node semantics;
- `output_node` обязателен при нескольких terminal nodes;
- максимум 1024 items;
- exact duplicate canonical JSON items отклоняются;
- branches исполняются последовательно в исходном порядке;
- вложенный `matrix` и `loop_group` внутри `matrix` в первом срезе запрещены;
- `subworkflow`, existing structural `foreach`, governed `workflow`, approvals,
  attempts и hooks внутри разрешены;
- если body содержит primary `assessment`, runtime требует ровно один созданный
  primary artifact для каждого item;
- item и его SHA-256 фиксируются до первой branch action; изменение source при
  resume является `matrix_items_changed`;
- `$MATRIX.index`, `$MATRIX.total`, `$MATRIX.item` и alias из `as` доступны во
  всех обычных template surfaces;
- public `matrix` output — ordered JSON array branch results.

Для workflow с `input.format: json` root-ссылки `$INPUTS.<field>[.<path>]`
читают проверенный JSON input. Для текстового input прежний контракт
`$INPUTS.input|message` не меняется. Это позволяет `matrix.items_from` читать
cases без отдельного suite parser и использует тот же JSON path resolver, что
structured node output.

Cases/repeats не имеют отдельной runtime-семантики. `takt eval flow` передаёт
один item на каждую пару case/repeat:

```json
{
  "case_id": "implement-basic",
  "repeat": 1,
  "input_path": "...",
  "expected_path": "...",
  "repository": ".takt/evals/.../workspaces/implement-basic/repeat-001/control",
  "workflow_path": "...",
  "case_fingerprint": "..."
}
```

Это оставляет `matrix` общим языковым механизмом и не зашивает в scheduler
понятия corpus, candidate или validator.

### 6.2. Matrix state

`NodeState` matrix container хранит bounded immutable history:

```json
{
  "matrix_fingerprint": "...",
  "matrix_active_index": 0,
  "matrix_branches": [
    {
      "index": 0,
      "item": {},
      "item_fingerprint": "...",
      "status": "completed",
      "nodes": {},
      "output": "...",
      "primary_assessment_id": "assessment-...",
      "completed_at": "..."
    }
  ]
}
```

Внутренние body node states существуют в root `state.Nodes` только для активной
ветки. После завершения они структурно копируются в branch snapshot и удаляются,
как states завершённой loop iteration. NodePath имеет форму
`/cases[0]/candidate`; совместимый публичный ID контейнера остаётся `cases`.

Crash/resume продолжает активную ветку и не повторяет completed branch. Approval
сохраняет active index и продолжает ту же ветку. Usage/artifacts дочерних nodes
агрегируются в matrix node и root Run без потери producer provenance.

### 6.3. Dynamic governed workflow paths

Чтобы prepared case мог запускать точный production workflow, `workflow.path`
и `workflow.repository` разрешают обычные template references. До первого child
side effect runtime:

- render-ит оба значения;
- проверяет containment repository внутри evaluation control workspace после
  symlink resolution;
- требует regular workflow file;
- загружает/валидирует child workflow и config references;
- сохраняет resolved path/repository и fingerprint в parent NodeState.

При resume изменение rendered path, repository или child fingerprint
fail-closed. Статические существующие значения сохраняют прежнее поведение.

`workflow.keep_worktree: true` передаёт обычный child StartOption и удерживает
workspace до assessment/evidence. После evaluation Run terminal cleanup может
удалить worktree; это administrative revision child Run и не меняет его
`result_revision`.

### 6.4. `script.stdin`

`script` получает необязательный `stdin`. Он render-ится через NonShell surface
и передаётся процессу byte-for-byte. Это общий process boundary, а не validator
feature. Одновременно с `inline`/`path` он не конфликтует.

### 6.5. `assessment`: отдельный node action

`assessment` ничего не запускает. Он атомарно:

1. render-ит target/result/scope/evidence references;
2. загружает terminal target и pin-ит `result_revision`;
3. декодирует `takt-validation/v1alpha1`;
4. проверяет provenance primary result;
5. проверяет evidence refs и checksums;
6. сохраняет target state/event snapshot и assessment envelope в artifact
   directory assessor Run;
7. регистрирует typed artifact в NodeState/RunState;
8. только затем завершает node.

В первом срезе multi-file evidence упаковывается deterministic collector-ом в
один tar artifact. Архив обязан содержать `manifest.json` с относительными
путями, SHA-256 и MIME каждого entry; absolute paths, `..`, symlink, device и
другие non-regular entries запрещены. Runtime не получает новый multi-artifact
API: assessment pin-ит обычный immutable artifact ref и его checksum. `inspect`
показывает manifest и извлекает только явно запрошенный bounded regular entry.

```yaml
- id: assess
  assessment:
    role: primary
    target_run_id: $candidate.child_run_id
    result_from: $validate.output
    scope:
      case_id: $MATRIX.item.case_id
      repeat: $MATRIX.item.repeat
    evidence:
      - $evidence.artifacts.evaluation-evidence
```

Primary `result_from` должен вести к output детерминированного
`bash|script|adapter` producer. Если source — governed validator workflow,
проверяется его selected output producer. `command|prompt` допускаются только
для `role: advisory`.

`result.valid=false` является успешным выполнением assessment node. Malformed
result, nonterminal/missing target, stale target во время capture, invalid
provenance, missing evidence или persistence failure являются execution error.

## 7. Evaluation input и launcher

### 7.1. Новый canonical input

`takt eval flow <evaluation-workflow>` выполняет только preflight/materialization
до Run:

```json
{
  "protocol_version": "takt-evaluation-input/v1alpha1",
  "type": "evaluation_input",
  "cases": [],
  "gates": [],
  "identity": {}
}
```

Preflight переиспользует текущие безопасные функции:

- discovery/normalization case IDs;
- independent workspace preparation;
- config/model materialization;
- profile initialization;
- Git/SCM fixture preparation;
- validator/config/path/capability checks;
- dataset/workspace/strategy fingerprints.

После успешного preflight создаётся один evaluation Run. Никаких candidate,
validator, evidence или assessment действий launcher после старта не выполняет:
они принадлежат authored DAG.

Preflight error до принятия Run возвращается как authoring/configuration error.
После принятия все измерительные ошибки отражаются в durable nodes/events.

### 7.2. Команда

Целевая форма:

```text
takt eval flow <evaluation-workflow> \
  --target code:feature-development \
  --config config.yaml \
  --cases cases \
  --repeat 3 \
  --gate validation_error_rate.max=0 \
  --model-preset qwen38 \
  --assistant-idle-timeout 15m \
  --trace
```

Обычный `takt run` может запустить тот же workflow с заранее materialized JSON
input. Разница `takt eval flow` — удобный corpus preflight и exit по gates, а не
другая Run semantics.

Gate definitions сохраняются в evaluation input и входят в identity
fingerprint. Без `--gate` quality gate отсутствует: CLI exit определяется
только техническим завершением. `eval flow init` генерирует безопасный default
`validation_error_rate.max=0` в Make example.

## 8. Gates и метрики

Metrics строятся только из non-stale primary assessments, emitted evaluation
Run:

```text
total = materialized case/repeat items
evaluated = items с ровно одним non-stale primary assessment

valid_rate = true_accept / evaluated
false_accept_rate = false_accept / evaluated
false_reject_rate = false_reject / evaluated
flow_completion_rate = target status completed / total
validation_error_rate = (total - evaluated) / total
```

Отсутствующий primary обычно уже делает Run failed; метрика сохраняется для
partial/legacy reports. `evaluated == 0` даёт `null`, а измеренный ноль — `0`.

Gates применяются application use case после durable reload Run и artifacts.
Gate result возвращается как DTO и не коммитится в target/evaluation Run.
Повторный `run stats --check-gates` детерминированно даёт тот же результат.

## 9. Общие Run queries и CLI

Новые canonical application queries:

```text
takt run status <run-id>
takt run stats <run-id> [--check-gates]
takt run inspect <run-id> [--case ID] [--repeat N] [--node ID]
takt run assessment <run-id> [--role primary|advisory] [--include-stale]
```

- `status` показывает technical state, progress matrix branches, usage и краткий
  assessment summary;
- `stats` агрегирует attempts, executions, timings, usage, outcomes и gates;
- `inspect` читает только state/events/artifacts/assessments и строит
  deterministic cause/evidence view;
- `assessment` возвращает assessments, где Run является target или assessor, с
  явным relation;
- ни одна query не запускает workflow/model/validator.

Существующие top-level `takt status`, `takt artifacts`, `takt children` и
`takt run summary` сохраняются как compatibility aliases/projections.

`takt eval status|stats|inspect` принимают новый Run ID и делегируют тем же
application queries. Старый directory argument остаётся read-only compatibility
path.

## 10. Persistence и поиск assessments

Assessment artifact принадлежит assessor Run. Для запроса по target локальный
application service сканирует Run IDs и только metadata artifacts type
`assessment`, затем строго декодирует найденные envelopes. Это O(number of
Runs), но не добавляет index/store abstraction без измеренной необходимости.

Повреждённый artifact не игнорируется: query возвращает `assessment_corrupt` с
producer Run/Artifact ID. Секреты проходят общий redactor; binary evidence с
известным секретом остаётся fail-closed.

## 11. Failure semantics

| Событие | Node/Run | Assessment/Gate |
|---|---|---|
| target completed, validator valid | completed | true_accept |
| target completed, validator invalid | completed | false_accept |
| target failed, validator invalid, candidate failure allowed | completed | true_reject |
| target failed, validator valid, candidate failure allowed | completed | false_reject |
| validator exit 1 с корректным result и `allow_failure` | completed | result сохраняется |
| validator crash/timeout | failed/timed_out | primary отсутствует |
| malformed result | assessment errored, Run failed | protocol error |
| evidence missing/corrupt | assessment errored, Run failed | evidence_missing |
| primary duplicate | matrix/Run failed | assessment_ambiguous |
| quality gate failed | Run остаётся completed | CLI non-zero |
| worktree cleanup failed после result | Run result не меняется | CLI cleanup error, assessment не stale |

`allow_failure` по-прежнему разрешает только ordinary exit. Child workflow
должен передавать исходный infrastructure kind, чтобы configuration/protocol/
timeout не маскировались под допустимый candidate reject.

## 12. Migration

### 12.1. Новые Run

- canonical source — `.takt/runs/<run-id>/state.json`, events и artifacts;
- новый eval не пишет отдельные `progress.json`/`report.json` как source of
  truth;
- optional export может строить совместимый report из Run queries, но export не
  используется status/stats/inspect.

### 12.2. Старые suites и reports

На compatibility-период:

- `takt-flow-evaluation/v1alpha1` продолжает запускаться старой командой и
  получает явное deprecation warning;
- `eval status|stats|inspect <directory>` продолжают читать сохранённые
  progress/report/evidence;
- legacy reports immutable и не импортируются автоматически в Run Store;
- `eval flow init` создаёт только новый ordinary workflow form;
- удаление старого runner разрешено после миграции feature/review/architect
  examples и одного release cycle чтения старых reports.

### 12.3. Make

Новые цели печатают полный evaluation Run ID в начале запуска. Read-only цели
используют его:

```text
make eval-status RUN=run-...
make eval-stats RUN=run-...
make eval-inspect RUN=run-... CASE=... REPEAT=...
```

Путь старого output directory остаётся допустим в compatibility-период.

## 13. Проверки

Минимальный product contour:

1. unit: assessment envelope, provenance, result revision и stale detection;
2. unit: matrix items, ordered branches, crash/resume, approval, duplicate and
   primary cardinality;
3. unit: dynamic workflow path/repository containment and fingerprint drift;
4. component: valid/invalid/validator crash/evidence missing status matrix;
5. component: late advisory assessment не меняет completed target;
6. application: status/stats/inspect/assessment используют один Store query;
7. E2E: один evaluation Run, две cases, два repeats, четыре branches и четыре
   primary assessments без case Runs;
8. E2E: gate failure даёт non-zero при `Run.status=completed`;
9. compatibility: старый report directory остаётся читаемым;
10. migrated mini-du smoke использует authored validator/evidence DAG и не
    вызывает скрытый Go post-run validator.

Live Pi/OpenCode benchmark не входит в release gate. Он запускается отдельно
после Go verification и не параллельно пользовательскому eval.

## 14. Реализационные срезы

### Slice A — assessment foundation

- result revision;
- assessment schema/action/artifact;
- artifact scan/query;
- status/stats/assessment DTO;
- stale/provenance contracts.

### Slice B — matrix в одном Run

- language/schema/reference grammar;
- durable branch snapshots/resume;
- assessment cardinality;
- public projection/inspect.

### Slice C — ordinary evaluation launcher

- materialized evaluation input;
- dynamic governed workflow references and keep-worktree;
- script stdin;
- gates/application exit;
- Make and mini-du feature migration.

### Slice D — convergence

- review/architect migration;
- unified inspect/stats aliases;
- docs/skill/changelog/version;
- old suite deprecation, без преждевременного удаления reader.

## 15. Контракт передачи в реализацию

Соблюдать принятые решения, допущения и ограничители. Не создавать второй
scheduler, case Run, assessment index/DB или fixed candidate/validator callback.

Если обнаружится факт, который меняет Run result revision, assessment
immutability, target/evidence contract, application boundary или возможность
matrix использовать общий `executeGraph`, остановить реализацию, зафиксировать
отклонение и вернуть задачу на повторный design-unknowns.

Внешний пользовательский workspace и текущий live eval не трогать. Все product
изменения сначала покрывать Go regression test, затем обновлять schema/docs/
skill/changelog и проходить полный release contour из `AGENTS.md`.
