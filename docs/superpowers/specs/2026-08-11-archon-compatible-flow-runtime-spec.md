# Спецификация Archon-first Takt YAML и durable Flow Runtime

**Дата:** 2026-08-11
**Статус:** normative target design; `READY` для A0/A1, `CONDITIONAL` для live budgets и parallel mutation
**Область:** локальный trusted runtime для произвольных пользовательских flow
**Основание:** `docs/proposals/002-archon-compatible-flow-runtime.md`

Этот документ превращает замечания аудита proposal 002 в целевой контракт. Он
не утверждает, что описанные расширения уже реализованы. Текущий внешний
контракт `takt/v1alpha1` остаётся источником фактической семантики до выхода
версии, в которой этот документ будет принят.

Целевой внешний контракт — один Takt YAML. Takt нативно принимает first-class
flow-конструкции Archon как основу своей схемы и расширяет эту же схему своими
durable/governance-полями. Отдельного Archon dialect, importer, transpiler,
compile-команды или второго authoring-формата нет. Преобразование загруженного
YAML в Go runtime definition является обычной внутренней работой parser/loader,
а не публичной архитектурной границей.

## 1. Цель и границы

Пользователь должен описывать процесс обычным YAML и Markdown-командами:

```text
workflow DAG
  → command/prompt/bash/script nodes
  → loop_group с bounded repair iterations
  → deterministic validation
  → review signal
  → downstream action
```

Takt отвечает за порядок, повтор, остановку, durable state, session/worktree
provenance, artifacts, budgets и observability. Агент отвечает за reasoning и
решения внутри своей сессии. Пользовательские tools отвечают за факты,
проверки и domain semantics.

### 1.1. Единый language surface

Целевой Takt YAML использует Archon-first root и node shape:

- root `name`, `description`, `labels`, `provider`, `model`, `nodes`;
- node `id`, `depends_on`, `when`, `provider`, `model`, `context` и ровно один
  action;
- actions `command`, `prompt`, `bash`, `loop`, `loop_group`, `cancel`;
- `until`, `until_bash`, `max_iterations`, `fresh_context`;
- единая reference grammar из §5 во всех renderable surfaces и `when`.

Takt расширяет тот же node/workflow contract прямыми полями для `script`,
`approval`, workflow composition, retries, typed artifacts, worktrees,
tool/skill/MCP/sandbox policies, budgets, `until.requires`, durable side-effect
reconcile, `output_format` и provenance. Существующие root `input`, `hooks` и
`worktree` также остаются прямыми Takt extensions. Namespace или вложенный
«Takt workflow» не вводится: конфликт имён обязан решаться при проектировании
общей схемы.

`provider` разрешается как имя существующего `assistants` binding в Takt Config,
а `model` — как логическое имя из `models`. Root задаёт default, node может его
переопределить. Неизвестное имя является preflight/authoring error до создания
Run; поле никогда не игнорируется и не требует пользовательского adapter code.

Node `context` принимает `fresh|shared`. Default Takt — `fresh`; root context не
нужен. `shared` явно продолжает сессию уникального ближайшего upstream
assistant при совпадении provider binding и logical model. Source обязан быть
транзитивным ancestor по явным `depends_on` в том же root/body DAG scope;
`context: shared` не добавляет ordering edge. Ноль подходящих ancestors,
несколько несравнимых sources, mismatch или возможность конкурентного resume
одной source session несколькими nodes являются authoring error, а не fresh
fallback. Session не протекает через loop/subworkflow/child-Run boundary.
Межитерационное продолжение того же logical node по-прежнему задаёт
`fresh_context`, а retry одной попытки —
`attempts.retry_session`; эти три механизма не подменяют друг друга.

### 1.1.1. Миграция root/node/command fields

| Текущий alpha field | Target field | Правило A0 |
|---|---|---|
| `apiVersion`, `kind` | — | удалить только у Workflow YAML |
| `metadata.name` | `name` | перенести без изменения |
| `metadata.description` | `description` | перенести без изменения |
| `metadata.labels` | `labels` | перенести без изменения |
| `defaults.assistant` | root `provider` | переименовать; binding остаётся в `Config.assistants` |
| `defaults.model` | root `model` | перенести без изменения logical name |
| `defaults.session: fresh` | — | удалить: target default уже `fresh` |
| node `assistant` | node `provider` | переименовать |
| node `model` | node `model` | без изменения |
| node `session: fresh` | node `context: fresh` | прямое соответствие |
| node `output_format` | node `output_format` | сохранить без изменения; schema subset и fail-closed validation остаются нормативными |
| root `input|hooks|worktree` | те же root fields | сохранить как Takt extensions |
| node `executor` и остальные governance fields | те же node fields | сохранить как Takt extensions |

Старый `session: resume` не имеет механического эквивалента в новой модели:
он не превращается в `context: shared`. A0 сохраняет фактический fresh first
entry, удаляя этот default/node override; exact retry переносится в
`attempts.retry_session: reuse` или уже существующий hook `on_failure.session`.
Только подтверждённое намерение разделять сессию между разными
последовательными nodes получает `context: shared` при A1. Межитерационное
намерение отдельно переводится в `fresh_context: false` при A1; до A1
сохраняется текущее fresh-between-iterations поведение.

Совместимость относится к flow language, а не к поведенческой идентичности всей
платформы Archon. Repository layout, UI, auth, container backend и встроенные
provider names Archon не входят в контракт Takt. Pinned fixtures из §10
проверяют только parse, validate и normalization реального Archon YAML; runtime
семантика проверяется собственными Takt contract/E2E tests.

Обязательные `apiVersion`, `kind`, `metadata` и старый `${nodes...}` authoring
из текущего alpha-контракта не переносятся автоматически. Если они не нужны
новой схеме, они удаляются; compatibility layer для alpha не строится.

### 1.2. Атомарный language switch

Переход не допускает dual-parse и красной промежуточной точки фиксации. Один
mergeable slice A0 одновременно меняет schema/loader/spec, renderer/`when`,
definition generators/rewriters и весь актуальный in-repo workflow content:

- все актуальные in-repo YAML definitions с `kind: Workflow`, включая весь
  встроенный профиль `code` и hidden `.takt/workflows` в skill/example assets;
- все активные Markdown commands с runtime references, examples, Go
  unit/contract/E2E fixtures и текущие references `skills/takt`;
- production generators, включая Dynamic Takt, которые создают старые root или
  `${...}` references;
- `docs/03`, `docs/07`, `docs/09`, `docs/72`, `ARCHITECTURE_DECISIONS.md`,
  schema, changelog и `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md`.

Config, package, workspace и evaluation documents сохраняют собственные
`apiVersion`/`kind`: они не являются Workflow authoring language. Исторические
release-документы остаются историей и не переписываются.

A0 является atomic language-switch release boundary, а не маленькой
feature-правкой. Внутри рабочей ветки его можно выполнять несколькими
проверяемыми коммитами, но mergeable результат обязан одновременно содержать
новую схему и полную миграцию и оставлять `make check` зелёным.

После A0 старые persisted Run остаются доступными для read-only inspection, но
не могут `resume`, `retry` или `fork` под новым Workflow contract. Операция
fail-closed сообщает несовместимую definition version; продолжение требует
старого бинарника, а новый runtime создаёт новый Run. Мигратор state не строится.
В A0 read-only означает существующие `run.get|summary|events`; новый
node/iteration `run inspect` остаётся срезом B.

Новый Run сохраняет `workflow_contract: takt-flow/v1alpha1` в durable state.
Поле является внутренним discriminator persisted definition, а не возвращением
`apiVersion` в YAML. Отсутствующее или неизвестное значение означает legacy
Run: Store/inspector обязаны его прочитать, mutating lifecycle operations —
отклонить до загрузки/сопоставления новой Workflow definition.

В документ не входят:

- второй agent tool loop;
- отдельный Gate/transition DSL или произвольные `next` из ответа модели;
- обязательный MCP или Go-adapter для каждого пользовательского tool;
- domain-specific JSON interpretation в ядре;
- автоматический merge параллельных mutating worktrees без явной merge action;
- server, multi-user или untrusted execution.

## 2. Решения P0 после аудита

### 2.1. Authority completion — bounded cross-node predicate

Новый generic validation runtime не вводится. `until` получает bounded
`requires`-список из простых предикатов над completed nodes. Loop завершается
только когда одновременно выполнены:

1. все условия primary `until` predicate;
2. optional `until_bash` вернул exit `0`;
3. все `requires` predicates.

Это не expression language. Primary `until` predicate разрешает только `node`,
`signal`, `exit_code` и `output_contains`; несколько его условий соединяются
через AND. Каждый `requires` predicate обязан содержать `node` и минимум одно
из `exit_code|output_contains`; `signal` внутри `requires` запрещён. LLM
completion authority остаётся одним primary signal; если нужны несколько
модельных мнений, их синтезирует объявленный review node. `requires` содержит
не более 64 entries с уникальными body node IDs, не ссылается за пределы loop
body, не повторяет primary node и не использует hidden compiled IDs. Условия
primary node записываются в самом `until`. Функции, regex, арифметика и
произвольный routing запрещены.

Целевая форма:

```yaml
until:
  node: review
  signal: BUILD-CLEAN
  requires:
    - node: validate
      exit_code: 0
```

Строка `until: BUILD-CLEAN` — нативная базовая форма Takt YAML. Loader
разрешает её только если в body есть ровно один terminal node и нормализует в
семантический predicate `{node: <terminal>, signal: BUILD-CLEAN}`. Это
внутренняя parser-модель того же языка, а не компиляция внешнего dialect. При
нескольких terminal nodes автор обязан указать расширенную форму с `node` явно.

Terminal node — прямой public child loop body, на который не ссылается
`depends_on` ни одного другого прямого public child. Определение применяется к
validated body DAG до компиляции hidden nodes. Ноль terminal nodes означает
невалидный/циклический body; при двух и более scalar form запрещена. Единственный
terminal `bash`/`script` node допустим: signal matcher не зависит от action type.

Signal contract закрытый и case-sensitive:

- значение обязано соответствовать `^[A-Z][A-Z0-9_-]{0,63}$`;
- matcher принимает либо ровно `<promise>SIGNAL</promise>` с optional whitespace
  внутри literal lowercase tags, либо SIGNAL как всю последнюю непустую строку;
- trailing punctuation, `итог: SIGNAL, но...` и negative prose не совпадают;
- fenced Markdown blocks с маркерами `` ``` `` или `~~~` исключаются до match;
  opener — строка с 0–3 leading spaces и минимум тремя одинаковыми marker
  characters, closer использует тот же character и не меньшую длину; signal
  внутри незакрытого или закрытого fence никогда не принимается;
- вне fences должен существовать ровно один ожидаемый signal и не должно быть
  другого валидного `<promise>OTHER_SIGNAL</promise>`; иначе результат
  `signal_ambiguous`, predicate false;
- signal matcher запускается при завершении primary `until` source над полным
  normalized `Result.Output` до user-facing projection. Для любого узла,
  участвующего в primary `until` или `requires`, `output_truncated: true`
  означает execution/protocol failure до записи `node.completed`; primary
  signal в этом случае не проверяется. Для остальных узлов сохраняется текущая
  семантика: они могут завершиться `completed` с `output_truncated: true`;
- найденное значение отдельно сохраняется как `matched_signal` в iteration
  state/event, поэтому inspector не перематчивает bounded output.

Для подсчёта occurrence учитываются только валидные `<promise>...</promise>` и
вся последняя непустая строка, полностью совпадающая с signal; совпадение слова
внутри обычной prose не является occurrence. Promise плюс такая же final-line
строка — два occurrence, поэтому результат считается ambiguous.

Iteration snapshot/event содержит результат единственного primary signal:
`matched_signal: string|null` и
`signal_diagnostic: signal_missing|signal_ambiguous|null`. Измеренное отсутствие
signal — `null`, а не пустая строка. Если primary predicate не содержит
`signal`, оба поля равны `null`. Для узла-источника predicate output truncation
остаётся execution/protocol failure и не кодируется как signal diagnostic; для
остальных узлов это только диагностический признак.

Legacy `output_contains` сохраняет текущую substring-семантику и не должен быть
использован для LLM completion authority.

Если `review` возвращает `<promise>BUILD-CLEAN</promise>`, а `validate`
завершён с ненулевым exit code, loop не завершается. Комбинация проверяется в
scheduler, но сила гарантии равна силе source node: scheduler не превращает
assistant claim в deterministic evidence.

### 2.2. Product RED — node data, не новый domain result

Ядро не декодирует `takt-validation/v1alpha1` и не знает domain-specific
`valid`, `score` или `code`. Product RED выражается уже существующим контрактом:

```yaml
- id: validate
  bash: ./project-check.sh
  allow_failure: true
```

Правила:

- exit `0` означает deterministic PASS;
- штатный ненулевой exit означает deterministic RED и сохраняется как
  `status: completed`, `exit_code: N`, потому что включён `allow_failure`;
- start/protocol/internal/timeout/cancel ошибки `allow_failure` не скрывает;
- report/stdout/stderr и artifacts сохраняются независимо от PASS/RED;
- если domain validator печатает JSON envelope, это evidence для domain/eval
  tooling; validator обязан также вернуть соответствующий exit code.

Таким образом, product RED не маскируется под execution failure: в state
различаются `completed + non-zero exit` и `failed/errored/timed_out/cancelled`.
Новая validation action не нужна.

Normative acceptance source — `bash`, `script` или domain adapter, который
проверяет внешний факт и возвращает exit code независимо от LLM. `command` и
`prompt` являются assistant nodes: их exit code формируется assistant envelope
и остаётся agent-controlled. Predicate над таким node технически разрешён, но
является advisory и не может обосновывать формулировку «deterministic
acceptance». Authoring diagnostics обязан отмечать любой
`exit_code|output_contains` predicate над assistant node как advisory; primary
LLM signal по определению остаётся decision, а builtin acceptance flows
подкрепляют его только deterministic `requires`/`until_bash` sources.

### 2.3. Session continuity — явный `fresh_context`

`loop_group` получает:

```yaml
fresh_context: false # default
```

Семантика:

- первая итерация каждого assistant node следует `context`; default `fresh`,
  explicit `shared` может получить предыдущую последовательную сессию того же
  provider;
- на первой attempt итерации `N > 1` значение `fresh_context` имеет приоритет
  над root/node `context`;
- `fresh_context: false` принудительно выбирает effective resume и
  передаёт Session ID того же logical node из предыдущей итерации;
- `fresh_context: true` принудительно выбирает effective fresh и
  очищает межитерационный Session ID;
- retry внутри одной итерации управляется `attempts.retry_session`; default
  сохраняет текущую session, `fresh` очищает её;
- deterministic nodes Session ID не получают;
- resume должен вернуть тот же Session ID; mismatch или provider failure —
  execution failure;
- silent fresh fallback запрещён.

Session mapping не создаёт отдельную сущность: runtime использует durable
`loop_iterations[].nodes[logical_node].session_id`/`loop_previous` как источник
последнего Session ID и перед запуском следующей body iteration восстанавливает
его в новом transient `NodeState`. Resolver обязан применить inter-iteration
mode после обычного `node.context → root context → fresh` resolution, иначе
default `fresh` снова обнулит seeded ID.

`fresh_context: false` сохраняет причинный контекст, но может линейно увеличить
стоимость provider context. Пользователь выбирает `true`, когда важнее bounded
fresh context, чем продолжение разговора.

Фактическая база для перехода: `internal/runtime/attempt.go` и
`internal/runtime/runner_test.go` подтверждают `allow_failure` только для exit;
`internal/runtime/runner.go` и `internal/store/store.go` подтверждают durable
loop snapshots и текущую потерю transient child state между итерациями;
`internal/runtime/template.go` подтверждает strict `${...}` rendering;
`internal/validation/result.go` и `internal/tooling/evaluation/evaluation.go`
разделяют validation decoding от scheduler.

### 2.4. `loop`, `until_bash` и `cancel`

`loop` — нативная короткая форма для повторения одного assistant
`prompt`/`command`. Loader нормализует её в одноузловой `loop_group`, поэтому
новый executor и отдельная scheduler-семантика не создаются. `loop` требует
ровно один `prompt|command`, `until`, `max_iterations`; принимает
`fresh_context` и optional `until_bash`. Output равен output единственного body
node последней итерации.

```yaml
- id: repair
  loop:
    prompt: |
      Исправь результат.
      Предыдущая попытка: $LOOP_PREV.repair.output
    until: REPAIR-CLEAN
    until_bash: ./scripts/validate.sh
    max_iterations: 3
    fresh_context: false
```

Внутри single-node `loop` ссылка `$LOOP_PREV.<loop-node-id>.output` адресует
output его предыдущей итерации; loader связывает её с единственным logical body
node. Другой node ID остаётся authoring error.

`until_bash` — deterministic required predicate, а не альтернативный способ
обойти primary `until`. Если он задан, итерация завершается только при
одновременном выполнении:

```text
primary until predicate satisfied
AND until_bash exit 0
AND all until.requires predicates
```

`until_bash` выполняется после завершения body даже при удовлетворённом primary
predicate.
Ненулевой numeric exit означает product RED и следующую итерацию; start,
protocol, internal, timeout или cancellation останавливают loop safe-stop. Его
stdout/stderr, exit code и duration сохраняются как iteration evidence. Это
намеренно строже OR/short-circuit semantics Archon.

Исполнение переиспользует существующий deterministic bash action path в
execution worktree владельца loop, с его env/redaction/sandbox и оставшимся
node timeout. Отдельных `allow_failure`, retry policy или executor у
`until_bash` нет; numeric exit уже нормализован семантикой выше, остальные
failure kinds сохраняют обычный приоритет runtime.

`cancel: <reason>` — deterministic terminal action. После успешного render
reason source node завершается, root Run получает `cancelled` с
`cancel_source: workflow`, node ID и reason; pending nodes пропускаются, running
и child Runs получают cancellation. Это правило одинаково для root DAG и
`loop_group` body: active iteration/path сохраняются, predicate evaluation и
следующая iteration не запускаются; `cancel` не создаёт обратного перехода.
Это не `completed` и не product RED.

## 3. Authoring contract

### 3.1. Workflow и commands

Workflow — YAML DAG. `command` разрешает Markdown-файл рядом с workflow,
profile или package. В одном node задаётся ровно один execution source:
`command`, `prompt`, `bash`, `script`, `approval`, `cancel`, `loop`,
`loop_group`, `subworkflow`, `workflow`, `foreach` или domain `adapter`.

`subworkflow` компилирует статический DAG в тот же Run; `workflow` создаёт
отдельный durable child Run с собственной lifecycle/isolation provenance. Это
разные существующие scheduler contracts, а не синонимы или второй executor.

Markdown command использует обычный body и optional frontmatter
`description`, `argument-hint`, `provider`, `model`. В A0 старый frontmatter
`assistant` переименовывается в `provider`. Provider разрешается в порядке node
→ command frontmatter → root → `Config.default_assistant`; model — node →
command frontmatter → root. Полученные имена обязаны существовать в
`Config.assistants`/`Config.models`, иначе preflight завершается ошибкой.
`argument-hint` является authoring/catalog metadata и не меняет runtime input
contract. Root `name|description|labels` и command
`description|argument-hint` не являются renderable surfaces: примеры `$...` в
них остаются literal text и не проходят reference resolution.

### 3.1.1. Structured output contract

`output_format` сохраняется как optional Takt extension для
`command|prompt|script|adapter` nodes без изменения семантики. Он использует тот
же `takt-schema-subset/v1`, что и `input.schema`; поддержка полного JSON Schema
не заявляется.

Authoring валидирует schema definition и доступность обязательных nested
`$node.output.<path>`/`items_from` путей до Run. Runtime передаёт точный contract
assistant/external executor, принимает ровно одно JSON value, проверяет его
fail-closed и сохраняет нормализованный output. Для `script|adapter` действует
та же post-execution validation. Текст агента вне contract, отсутствующее
required field и неверный type не могут стать completed result.

`output_format` не является обязательным для обычного текстового node и не
превращает agent output в deterministic evidence: schema доказывает форму, а
product correctness по-прежнему принадлежит validator/adapter predicate.

Пользовательский command не содержит runtime protocol, JSON event envelope,
provider argv или persistence instructions. Он получает обычный input,
workspace и ссылки на outputs/artifacts.

### 3.2. Feature flow

Целевой flow создания feature:

```yaml
name: feature-development
description: Implement a planned change and publish it

nodes:
  - id: plan
    command: feature-plan

  - id: build
    depends_on: [plan]
    loop_group:
      max_iterations: 5
      fresh_context: false
      until:
        node: review
        signal: BUILD-CLEAN
        requires:
          - node: validate
            exit_code: 0
      nodes:
        - id: implement
          command: feature-implement

        - id: validate
          bash: ./scripts/project-validate.sh
          allow_failure: true
          depends_on: [implement]

        - id: review
          command: feature-review
          depends_on: [validate]

  - id: create-pr
    command: scm-create-change
    depends_on: [build]

  - id: verify-pr
    bash: ./scripts/verify-pr.sh
    depends_on: [create-pr]
```

`feature-implement` получает предыдущий review feedback. `feature-review`
получает output deterministic `bash` validator и current worktree. Если
validation RED, review остаётся runnable и формирует feedback; `requires`
запрещает ложный success. Замена validator на assistant `command` понижает
гарантию до advisory и не является эквивалентной.

`create-pr` остаётся assistant command и может выполнить публикацию обычными
agent tools, но его текст не доказывает внешний эффект. Поэтому downstream
`verify-pr` — project-owned deterministic `bash` (либо verified SCM adapter),
который заново читает внешний state и fail-closed проверяет receipt/base/head.

### 3.3. Другие процессы

Тот же runtime описывает без изменения ядра:

- PR review: load → parallel read-only reviewers → join → synthesis → approval
  → publish;
- security analysis: collect → parallel scanners → join → remediation loop →
  deterministic acceptance;
- schema-constrained YAML generation: analyze → generate → validate → review
  loop → runtime acceptance;
- обычный линейный flow: load → action → verify.

Различие процессов принадлежит commands, skills, tools и validators, а не
новым node types.

## 4. Loop semantics

### 4.1. Iteration lifecycle

Каждая итерация:

1. создаёт transient body NodeState для logical node paths;
2. передаёт `loop_previous` последней завершённой итерации;
3. исполняет body единым DAG scheduler;
4. сохраняет immutable `loop_iterations[]` snapshot, включая outputs,
   statuses, exit codes, sessions, usage и artifacts;
5. проверяет основной `until` и все `requires`;
6. завершает parent при success или запускает следующую iteration.

Независимые body nodes выполняются параллельными waves. Обратных DAG-рёбер нет:
возврат к implement/analyze — это повтор всего заранее объявленного body.

### 4.2. Predicate rules

Predicate считается истинным только для `status: completed` node и всех
указанных условий. `requires` проверяются после body completion; output
reviewer не может изменить их значение. `signal` проверяется безопасным
matcher-ом, описанным в §2.1; произвольная substring в reviewer output не
является completion evidence.

Все predicates читают только NodeStates текущей завершившейся итерации.
`loop_previous`, root nodes и artifacts прошлых итераций не участвуют в
completion evaluation; ссылка за пределы current body является authoring
error. Предыдущие данные доступны agents только как feedback/templates.

Для product RED validator обязан иметь `allow_failure: true`. Для execution
failure зависимые nodes не становятся feedback-итерацией: parent loop получает
ошибку и Run останавливается.

Если source основного или `requires` predicate получает `skipped`, это
`required_evidence_missing` и safe-stop, а не бесконечный retry. Authoring
должен проверять, что predicate nodes достижимы из body DAG.

### 4.3. Exhaustion and stop priority

При исчерпании `max_iterations` Run получает failure с сохранёнными последними
report/output/artifacts. `timed_out` и `cancelled` имеют приоритет над
производной ошибкой exhaustion. Budget stop имеет код `budget_exceeded` и не
становится success.

`always_run` cleanup не скрывает failure. External side effect со статусом
unknown требует reconcile и не повторяется blind.

### 4.4. Пауза, восстановление и операторский retry

Три случая не смешиваются:

- `waiting`, `paused` или process-loss оставляют `loop_iteration = N` и durable
  body NodeStates; `resume/recover` продолжает ту же итерацию, completed nodes не
  повторяются, потерянная running attempt нормализуется существующим recovery;
- completed product RED закрывает snapshot итерации N и штатно начинает N+1;
- execution failure сохраняет failed iteration snapshot, сбрасывает active
  marker и немедленно завершает parent/Run failure; автоматического N+1 нет.

Operator `run retry` failed loop сохраняет `loop_iterations`, `loop_previous`,
artifacts и executions и запускает новую полную итерацию N+1. Это не
`resume` той же failed iteration. При `fresh_context: false` Session IDs сеются
из failed snapshot. Если `N == max_iterations`, retry отклоняется; для нового
лимита/definition нужен `fork`.

Текущий `RunService.Retry` заменяет весь container `NodeState` и теряет loop
history; A1 обязан изменить этот special case. Поскольку operator retry
повторяет весь body, внешние side effects внутри repair loop допустимы только с
`idempotent/reconcile` policy; безопаснее держать publication после loop.

## 5. Feedback, templates and artifacts

Публичный reference syntax целевого Takt YAML является единым для prompts,
commands, deterministic actions, hooks, composition/fan-out inputs и `when`.
Старые `$USER_MESSAGE`, Takt `${...}` и отдельный `nodes.*` syntax после A0 не
принимаются.

| Значение | Старый alpha authoring | Целевой Takt YAML |
|---|---|---|
| Вход Run | `$USER_MESSAGE`, `${input}` | `$ARGUMENTS` |
| Вход подключённого workflow | `${inputs.name}` | `$INPUTS.name` |
| Output/JSON path | `${nodes.build.output.x}` | `$build.output.x` |
| Node state | `${nodes.build.status}`, `.exit_code` | `$build.status`, `$build.exit_code` |
| Child provenance | `${nodes.build.child_branch}` и остальные child fields | `$build.child_branch` и тот же field |
| Typed artifact | `${nodes.build.artifacts.report.path}` | `$build.artifacts.report.path` |
| Предыдущая итерация | `${loop.previous.review.output}` | `$LOOP_PREV.review.output` |
| Retry/hook feedback | `${feedback}` | `$FEEDBACK` |
| Governed fan-out item | `${fanout.item.x}` | `$FANOUT.item.x` |
| Fan-out position | `${fanout.index}`, `${fanout.total}` | `$FANOUT.index`, `$FANOUT.total` |
| Captured approval | `${approvals.confirm}` | `$confirm.output` |
| Static foreach vars | `${check}`, `${check.index}`, `${index}` | `$INPUTS.check`, `$INPUTS.check.index`, `$INPUTS.index` |
| Execution base ref | не был template context | `$BASE_BRANCH` |
| Artifact directory | `$ARTIFACTS_DIR` | `$ARTIFACTS_DIR` |

Canonical Run input — `$ARGUMENTS`; `$USER_MESSAGE` не сохраняется как alias.
`$INPUTS.<name>` разрешается при composition/foreach expansion, а `$FANOUT.*` —
при запуске конкретного governed child. `as` не создаёт конкурирующий `$node`
namespace: named value доступно как `$FANOUT.<as>`; значения `item`, `index` и
`total` для `as` reserved и отклоняются authoring.

`$BASE_BRANCH` возвращает durable resolved `worktree.base_ref` владельца Run —
то же значение, относительно которого создан candidate. Если Run не имеет
resolved base ref, обязательная ссылка fail-closed при render; author может
использовать optional suffix только когда отсутствие base допустимо. Takt не
угадывает branch по текущему состоянию Git во время каждого node.

Reference lexer распознаёт только следующие полные формы:

```text
NODE   := [A-Za-z_][A-Za-z0-9_-]*
SEG    := [A-Za-z_][A-Za-z0-9_-]* | [0-9]+
NAME   := [A-Za-z_][A-Za-z0-9_-]*
NODE_STATE := exit_code | status | child_run_id |
              child_control_workspace | child_execution_workspace |
              child_branch | child_base_commit
ATYPE  := [A-Za-z0-9][A-Za-z0-9._-]{0,127}
META   := id | type | mime | path | sha256 | size |
          producer_run_id | producer_node_id | attempt
OUT    := "$" NODE ".output" ("." SEG)*
STATE_REF := "$" NODE "." NODE_STATE
ART    := "$" NODE ".artifacts." ATYPE "." META
PREV   := "$LOOP_PREV." NODE ".output" ("." SEG)*
INPUT  := "$INPUTS." NAME ("." SEG)*
FANOUT := "$FANOUT.item" ("." SEG)* |
          "$FANOUT.index" | "$FANOUT.total" |
          "$FANOUT." NAME ("." SEG)*
BARE   := "$ARGUMENTS" | "$ARTIFACTS_DIR" | "$BASE_BRANCH" | "$FEEDBACK"
```

Lexer использует maximal munch. Reference заканчивается на первом символе,
который не может продолжать соответствующую форму; suffix `?`/`:-TOKEN`
потребляется сразу после неё. Точка после `output` означает JSON path, поэтому
literal `.suffix` нельзя приклеить к whole-output reference без промежуточной
переменной/аргумента. Неразрешённое продолжение известного `$NODE.` и bare
context без identifier boundary является authoring error, а не частичной
подстановкой.

Artifact type может содержать точки. Parser отделяет правый `.META`, а всё
между `.artifacts.` и ним считает `ATYPE`; поэтому
`$validate.artifacts.report.json.path` означает type `report.json`, metadata
`path`. Полностью numeric `ATYPE` запрещён: нестабильные positional artifact
indexes текущего alpha не входят в target language, автор ссылается на
объявленный artifact type.

Optional suffix `?` возвращает пустую строку только для отсутствующего runtime
value. Default suffix `:-TOKEN` принимает один непустой token без whitespace и
используется для missing/empty value. Неизвестный node, недоступный upstream,
необъявленный structured field или неизвестный context остаётся fail-closed.
`$LOOP_PREV...` на первой итерации является единственным implicit empty case.

Node IDs `ARGUMENTS`, `ARTIFACTS_DIR`, `BASE_BRANCH`, `INPUTS`, `LOOP_PREV`,
`FEEDBACK` и `FANOUT` являются reserved и отклоняются authoring. В non-shell
surfaces `$$` экранирует literal `$`. Lexer выполняет один проход и не рендерит
подставленное значение повторно. В non-shell renderable surface любая
последовательность `$NAME` является началом Takt reference: неизвестный
context/node/field — authoring error, для literal используется `$$`. В shell
surfaces действуют более узкие правила §5.1, сохраняющие обычные shell
variables. Лексически корректная node reference с необъявленным `NODE` всегда
остаётся authoring error.

`when` использует те же `$node.output...`, `$node.status`, `$node.exit_code` и
`$ARGUMENTS` references:

```yaml
when: "$validate.status == 'completed' && $validate.exit_code == '0'"
```

Операторы остаются ограничены `==`, `!=`, `&&`, `||`; скобки, функции, regex и
арифметика не добавляются. `when` сравнивает строки без numeric/boolean
coercion: quotes у RHS только ограничивают literal, поэтому `0` и `'0'`
сравниваются одинаково. `exit_code` сериализуется десятичной строкой, status и
string output остаются text, JSON number/bool — text из runtime JSON
serialization, `null` — пустая строка; object/array сравниваются как compact
JSON только при явном равенстве. `$INPUTS` разворачивается до проверки `when`,
а runtime-only
`$FEEDBACK`/`$FANOUT` в `when` запрещены.

### 5.1. Контекстное экранирование

- prompt, command, approval, cancel и другие non-shell text surfaces получают
  raw textual value;
- `when` разрешает reference напрямую и не выполняет string substitution;
- в `bash` и `until_bash` node/artifact/previous/input/fan-out values
  подставляются как один shell-quoted argument; bare `$ARGUMENTS`, `$FEEDBACK`
  `$ARTIFACTS_DIR` и `$BASE_BRANCH` передаются через одноимённые environment
  variables, а не встраиваются сырым текстом;
- authoring отклоняет двойное shell quoting вида `"$node.output"`, потому что
  значение уже quoted; нормативная форма — `value=$node.output`;
- обычные shell expansions `$PATH`, `${PATH}`, `$?`, `$$`,
  `$((...))` и `$(...)` в `bash`/`until_bash` не являются Takt references и
  остаются byte-for-byte; старые Takt `$USER_MESSAGE`,
  `${nodes...}`/`${input}` при этом отклоняются;
- `script.args`, `script.env`, paths и working directory получают значения как
  argv/env/path без shell; inline Python/Node/Go source не интерполирует
  user-controlled references — данные передаются через `args`/`env`;
- start/protocol/render/escaping failure является execution failure, а не
  пустой подстановкой.

Outputs bounded. Полный stdout/stderr, reports, traces и diffs сохраняются как
artifacts с digest/provenance. Artifact path не считается частью Git scope,
если файл лежит вне execution worktree.

Новый node не получает все artifacts автоматически в prompt: command явно
ссылается на нужный output/artifact. Профили могут добавлять bounded manifest
через одну reusable command; runtime не изобретает второй artifact DSL.

## 6. Sessions, worktrees and fan-out

### 6.1. Worktree

Последовательные mutating nodes одного Run используют общий execution worktree.
Read-only parallel workers могут использовать parent workspace. Каждый
mutating child Run получает managed worktree и сохраняет собственные
provenance/events/artifacts.

Takt не делает implicit last-writer-wins и не обещает merge mutating fan-out.
Текущий Workflow contract не содержит достоверного `mutating/read_only`
маркера, поэтому authoring не заявляет, что способен классифицировать worker по
его реальному поведению. До отдельной merge action `isolation: worktree`
объединяет только child outputs/artifacts/statuses: файлы child worktree не
появляются в parent candidate. Автор обязан ограничить workers read-only либо
добавить явную user-owned merge command. Merge conflict — failure; implicit
merge отсутствует.

### 6.2. Dynamic work items

Агент возвращает данные, а не workflow transitions:

```text
analysis output → declared foreach/fan-out → child Run(s) → join
```

`items_from` принимает только JSON array из объявленного output path. Worker
workflow/subworkflow задаётся flow заранее. Тип элемента не выбирает node,
command, toolset или модель; для разных worker roles автор объявляет разные
fan-out nodes либо один явно определённый worker, который валидирует item.

Fingerprint списка фиксируется до создания child Runs; изменение списка на
resume/retry не переиспользует старые children молча.

## 7. Tools, skills and policies

### 7.1. Регистрация и запуск из кодового агента

Пакет или профиль регистрирует flow декларативными метаданными каталога:
workflow YAML, соседними Markdown-командами, skills, политикой tools и входным
контрактом. Регистрация не создаёт runtime plugin или второй scheduler.

Пользователь или кодовый агент запрашивает именованный flow через host command
или каноническую Run operation:

```text
agent/host → request named flow + input
Takt       → resolve package/profile and create durable Run
Takt       → return Run ID, state, artifacts and attention requirements
```

Перехват `/takt` является capability интеграции Pi/OpenCode. Без подтверждённой
host-control capability вызов через skill/MCP остаётся advisory: агент может
запросить `run.start`, но scheduler принадлежит Takt, и текст агента не может
завершить, пропустить или перенаправить Run.

Каждый assistant node может задавать `allowed_tools`, `denied_tools`, `skills`,
`mcp`, sandbox и model. Effective child policy пересекает allowlists и
наследует более строгие deny/requirements/sandbox.

Пользовательский tool подключается одним из путей:

- executable/script с stdin/stdout и exit code;
- process tool с JSON request/result;
- MCP server;
- существующий neutral domain adapter, если нужна reconcile/idempotency.

Go-код для каждого project validator не требуется. Tool не получает право
менять DAG; порядок обязательных действий задают соседние nodes и `depends_on`.

Lifecycle hooks (`before_node`, `after_node`, `before_complete`, `on_failure`)
являются bounded instrumentation/repair points. Они могут писать метрики и
diagnostics, но не получают произвольный `next` и не заменяют deterministic
predicate. Per-tool pre/post interception остаётся capability конкретного host;
Takt не требует от domain developer писать такой adapter.

## 8. Budgets and observability

### 8.1. Loop budget

Целевой `loop_group.budget`:

```yaml
budget:
  max_tokens: 200000
  max_tool_calls: 1000
  enforcement: required # required | optional
```

Budget охватывает все assistant attempts и child Runs body одной loop group,
суммируется по итерациям и сохраняется durable. `max_iterations`, node
`timeout` и `idle_timeout` остаются отдельными hard limits.

- `max_tokens` — input + output usage delta;
- `max_tool_calls` — unique normalized tool call IDs;
- повтор одного durable call ID не считается дважды;
- при превышении scheduler не запускает следующий node/iteration и сохраняет
  `budget_exceeded`;
- `enforcement: required` требует capability proof live usage/tool events до
  assistant execution;
- `optional` разрешает degraded decision, если host сообщает usage только
  после попытки; degraded flag обязан попасть в state/events;
- текущий process/output limit не подменяет token/tool budget.

До live capability proof token/tool budget не является гарантией конкретного
Pi/OpenCode host. Wall-time, cancellation, `max_iterations` и output bounds
остаются enforceable независимо от live tool stream.

### 8.2. Durable metrics

Каждый Run/iteration сохраняет:

- flow/command/profile/skill/tool fingerprints;
- requested/resolved assistant/model и provider version;
- session ID, fresh/resume, attempts и usage;
- tool calls, denied calls и approval decisions, когда host их сообщает;
- loop iteration, validation exit code, review signal и diagnostic fingerprint;
- worktree revision/digest и artifact manifest;
- terminal reason, budget decision и human intervention.

Недоступная метрика — `null`, измеренный ноль — `0`. Provider wall time не
используется как quality metric.

### 8.3. Inspector

Добавляется read-only use case и CLI:

```text
takt run inspect <run-id>
takt run inspect <run-id> --node build
takt run inspect <run-id> --node build --iteration 2
takt run inspect <run-id> --json
```

Проекция читает Store после исполнения и показывает:

```text
iteration 1: validate RED (exit=1), review feedback, session S1, digest A
iteration 2: validate PASS (exit=0), review BUILD-CLEAN, resumed S1, digest B
loop exit: accepted
```

Это projection существующего durable state, не второй runtime/store.

## 9. Failure matrix

| Ситуация | Результат |
|---|---|
| validator exit 0 | predicate PASS; iteration может завершиться при signal |
| validator non-zero + `allow_failure` | completed product RED; review/next iteration runnable |
| validator non-zero без `allow_failure` | node failed; safe-stop |
| assistant `command` exit 0 в `requires` | predicate может совпасть, но evidence advisory |
| start/protocol/internal/timeout/cancel | execution failure; `allow_failure` не скрывает |
| reviewer без signal | feedback iteration; следующий body pass |
| reviewer signal при required RED | loop не завершается |
| `until_bash` exit 0 | required deterministic predicate PASS; только вместе с primary `until`/`requires` |
| `until_bash` numeric non-zero | product RED; iteration evidence; next iteration |
| `until_bash` start/protocol/timeout/cancel | execution failure; safe-stop |
| ambiguous/fenced/prose signal | predicate false + diagnostic; не success |
| output truncated у узла-источника predicate | execution/protocol failure до `node.completed`; matcher не запускается |
| output truncated у другого узла | текущая семантика: `completed` с `output_truncated: true`; это не signal diagnostic |
| required node skipped | `required_evidence_missing`; safe-stop |
| exact resume mismatch | execution failure; no fresh fallback |
| operator retry failed iteration | history preserved; next full iteration N+1 |
| max iterations | failed/exhausted with evidence |
| budget exceeded | failed/budget_exceeded with evidence |
| `cancel` action | Run cancelled с workflow source/reason; child cancellation cascade |
| persisted Run старого Workflow contract | read-only inspect; resume/retry/fork fail-closed |
| external side effect unknown | reconcile/needs_input; no blind retry |

## 10. Контракт приёмки

Реализация спеки не принимается без модульных и контрактных тестов Go, а также
E2E-тестов чёрного ящика, которые доказывают:

### 10.1. A0 — language switch

1. Workflow loader принимает только новый root/node contract, а завершённый A0
   мигрирует весь актуальный in-repo Workflow/Markdown-command content,
   включая hidden `.takt/workflows` assets, generators, examples, E2E и skill
   references; dual-parse отсутствует и `make check` зелёный.
   A1-only fields/actions до A1 отклоняются схемой, а не падают при execution.
2. Config/package/workspace/evaluation documents продолжают использовать свои
   отдельные схемы и не попадают под Workflow language switch.
3. Каждый старый context из таблицы §5 имеет ровно одну target-форму;
   optional/default suffix, numeric JSON segments, reserved IDs, `$$` и все
   NodeState/artifact metadata проверяются contract tests; positional artifact
   index отклоняется в пользу typed reference. Optional `output_format`
   сохраняет `takt-schema-subset/v1`, fail-closed runtime validation,
   authoring-проверку nested output paths и `items_from`; invalid/missing/type
   mismatch покрыты negative tests.
4. `when` использует ту же `$...` grammar и только `==`, `!=`, `&&`, `||`;
   старые `nodes.*`/`inputs.*` и renderable `$USER_MESSAGE`/Takt `${...}`
   отклоняются. Contract tests фиксируют string-only comparison для quoted и
   unquoted `0`, JSON string/number/bool/null и отсутствие type coercion.
5. Prompt/raw, `when`, bash/until_bash и script args/env interpolation имеют
   разные зафиксированные escaping contracts; в A0 это проверяется для `bash`,
   а A1 применяет тот же contract к `until_bash`. Shell injection fixture
   остаётся одним argument, обычные shell expansions не меняются, double
   quoting Takt node reference получает authoring error, inline script source не
   получает raw user-controlled substitution.
6. `provider`/`model` разрешаются через существующий Config registry;
   `context:fresh` является default и неизвестные bindings fail-before-Run;
   `context:shared` остаётся schema-invalid до A1. Legacy root/node `session`
   отсутствует, а каждый прежний `resume` use либо перенесён в retry policy,
   либо явно сохранён fresh до A1 по §1.1.1.
7. Реальный Archon fixture
   `.archon/workflows/defaults/archon-feature-development.yaml` vendored
   byte-for-byte из commit `41765d6a1448da73f398a30e161f3b4eaba0b768`
   вместе с MIT License и repository/path/commit/SHA-256 metadata. A0-тест
   доказывает только parse, schema validation и normalization базовых
   command/bash/provider/context constructs без source rewrite. Full authoring
   preflight не является свойством одного YAML fixture; если он запускается в
   этом тесте, Archon command names и `claude`/`large` предоставляет внешний
   test resolver/Config, а fixture не меняется. Execution compatibility не
   заявляется.
8. Persisted Run старого Workflow contract остаётся inspectable, но
   resume/retry/fork отклоняются понятной incompatible-definition ошибкой;
   новый Run всегда сохраняет `workflow_contract`, legacy state без поля
   остаётся loadable.

### 10.2. A1 — loop acceptance и sessions

9. Скалярный `until` разрешает единственный terminal-узел body, принимает
   deterministic terminal node и отклоняет нулевое или неоднозначное
   определение terminal node.
10. Signal grammar принимает один точный ожидаемый signal и отклоняет negative
    prose, signal внутри предложения, code fence, несколько/чужие signals и
    недопустимый charset.
11. Truncation у узла-источника predicate создаёт execution/protocol failure до
    `node.completed`; другой узел сохраняет текущую completed-семантику;
    `matched_signal` переживает Store reload.
12. `requires` читает только public body nodes текущей итерации; missing/skipped
    evidence даёт `required_evidence_missing` safe-stop.
13. `requires` над assistant `command`/`prompt` помечается advisory; builtin
    acceptance использует `bash`/`script`/verified adapter.
14. Review signal не закрывает итерацию с RED validation; разрешённый ненулевой
    exit остаётся completed product evidence, а execution errors останавливают
    loop.
15. `until_bash` всегда исполняется при наличии и соединяется с primary
    `until`/`requires` через AND; exit 0 завершает predicate, numeric
    non-zero повторяет iteration, start/protocol/timeout/cancel останавливают
    Run; его Takt references и обычные shell expansions соблюдают contract §5.1.
16. `loop` нормализуется в одноузловой `loop_group` и использует тот же
    scheduler/state/recovery contract без второго executor. Второй pinned
    fixture `.archon/workflows/rasmus-tests/t1-fix-issue.yaml` из того же commit
    принимается без rewrite после A1; при full preflight test Config задаёт
    default assistant и связывает `@mini`, а external `gh`/project commands не
    запускаются fixture-тестом.
17. `cancel` переводит Run и children в cancelled с durable workflow source,
    node path, active iteration и reason; одинаково работает внутри
    `loop_group`, predicate/next iteration не запускаются, downstream не
    запускается.
18. `context:shared` продолжает только однозначную последовательную
    upstream-сессию того же provider/model от explicit `depends_on` ancestor в
    одном DAG scope, не создаёт implicit edge и отклоняет
    missing/ambiguous/concurrent reuse. На итерации N>1 `fresh_context` имеет
    приоритет над node `context`; false продолжает Session ID того же logical
    node, true начинает fresh, а mismatch не получает fallback.
19. Recovery после wait/pause/process-loss продолжает активную итерацию без
    повтора completed nodes; execution failure сохраняет snapshot, operator
    retry запускает N+1 и сохраняет history.
20. Iteration snapshots, artifacts, usage, predicate diagnostics и sessions
    переживают reload; max iterations и timeout/cancel priority сохраняются.

### 10.3. Последующие срезы

21. Required budget fail-before-execution без capability proof, optional
    записывает degraded state, budget stop сохраняется durable.
22. Inspector читает persisted state и фильтрует по node/iteration.
23. Isolated worktree fan-out объединяет только outputs/artifacts и не переносит
    child files без explicit merge command.
24. Feature, PR-review и schema-constrained generation flows используют только
    public commands, policies и существующие tool seams.
25. Coding-agent host запускает именованный flow и получает durable Run ID;
    scheduler/completion остаются под управлением Takt.

Infrastructure contract suites остаются отдельными от model-quality benchmark.
Eval сравнивает плоскость продукта (acceptance/evidence/safe stop) и плоскость
процесса (iterations/tokens/tools/resumes/cost/failure fingerprints).

## 11. Порядок реализации и обязательный контрактный след

### A0 — language switch и полная in-repo migration

1. Сначала добавить failing contract tests §10.1, включая pinned Archon fixture
   без source rewrite и negative test старого Workflow root/reference syntax.
2. Заменить Workflow root/node schema и Go spec shape; Config/package/evaluation
   schemas не менять; `output_format` и его schema-subset contract перенести без
   изменения. A0 schema принимает только target forms, которые A0 уже
   умеет исполнить: A1-only `loop`, `cancel`, scalar `until` и `until.signal`,
   `until.requires`, `until_bash`, `fresh_context` и `context: shared` остаются
   schema-invalid до A1. Существующий structured `loop_group.until` с
   `exit_code|output_contains` сохраняется и мигрируется без второго dialect.
3. Реализовать один reference lexer/resolver для templates и `when`, включая
   context-aware escaping, provider/model preflight, safe `context:fresh` и
   reserved IDs.
4. В том же mergeable slice мигрировать все актуальные in-repo YAML definitions
   с `kind: Workflow`, включая hidden `.takt/workflows` skill/example assets,
   production generators/rewriters, все активные Markdown commands с runtime
   references, examples, unit/E2E fixtures, `skills/takt` и current docs. Старый
   in-repo Workflow/command reference content не остаётся рабочим параллельно —
   он перестаёт существовать.
5. Добавить fail-closed definition-version guard для persisted old Run; inspect
   остаётся read-only; новый state пишет `workflow_contract: takt-flow/v1alpha1`,
   отсутствие поля означает legacy.
6. Обновить `schemas/workflow.schema.json`, `schemas/run-state.schema.json`,
   `docs/03-specification.md`,
   `docs/07-archon-compatibility.md`, `docs/09-runtime-semantics.md`,
   `docs/archive/releases/72-architecture-contracts-v0.1.57.md`, `ARCHITECTURE_DECISIONS.md`,
   `docs/05-implementation-status.md`, `CHANGELOG.md` и `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md`.
   `docs/03` показывает отдельно non-shell `$$` escape и shell `$$` PID/native
   expansion, чтобы одинаковый token не выглядел единым поведением.

### A1 — loop acceptance, actions и sessions

1. Сначала добавить failing unit/contract/E2E tests §10.2.
2. Расширить `UntilSpec` safe `signal`/`requires`, scalar `until` и durable
   predicate diagnostics.
3. Добавить native `loop` normalization, required-AND `until_bash` и `cancel`
   action на едином scheduler/cancellation contract.
4. Добавить node `context:shared`, `LoopGroupSpec.fresh_context`, exact Session
   ID seeding, interrupted/operator retry semantics и fake-host vertical slice
   от validation RED до BUILD-CLEAN.
5. Обновить `schemas/workflow.schema.json`, `schemas/run-state.schema.json`
   (`matched_signal`, `required_evidence_missing`, workflow cancel source),
   `docs/03`, `docs/09`, `docs/10`, ADR, `skills/takt`, `docs/05`,
   `CHANGELOG.md` и `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md`.

### Срез B — инспектор

1. Добавить один canonical `internal/appapi.OperationDescriptor` для
   `run.inspect`, generated operation docs и CLI projection.
2. Добавить application/CLI/MCP contract tests; не создавать transport-specific
   state или второй executor.
3. Обновить `docs/03-specification.md`, карту operations, `CHANGELOG.md` и
   `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md`.

### Срез C — guarded budgets

1. Добавить loop budget fields и capability proof только после live host
   evidence.
2. Проверить required/optional enforcement и durable budget stop.
3. Обновить workflow schema и run-state schema (`budget_exceeded`, enforcement
   decision/counters), `docs/03-specification.md`,
   `docs/09-runtime-semantics.md`, `docs/10-assistant-adapter-spec.md`, ADR,
   `skills/takt`, `docs/05`, `CHANGELOG.md` и `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md`.

Parallel mutating merge остаётся отложенным до реального flow-потребителя. До
этого mutation выполняется последовательно либо через явную user-owned merge
command.

## 12. Проектный шлюз

Решения P0 по acceptance, session continuity, migration, `output_format`,
references, interpolation, provider binding, actions, fixture provenance и old
Run state закрыты этим документом. Для P1 заданы ограниченные срезы и safe
defaults:

- нет implicit merge или недоказуемой authoring-классификации mutation;
- core не интерпретирует domain validation JSON;
- fan-out item не выбирает worker type;
- hard token/tool guarantee запрещена без capability proof;
- resume failure не получает automatic fresh fallback.

Шлюз: `READY для A0/A1; CONDITIONAL для C и parallel mutation`.

| Пункт | Владелец | Ограничитель до реализации | Доказательство для снятия ограничения |
|---|---|---|---|
| U-04/U-09 native references | authoring runtime | один Archon-first Takt YAML; dual-parse запрещён | A0 loader/template/when + full migration tests |
| U-10 migration | владелец Workflow contract | A0 атомарно меняет schema и весь актуальный Workflow/command content по field map §1.1.1; `output_format` сохраняется | `make check` после A0, old syntax/output-format negative tests |
| U-11 interpolation | владелец runtime | context-aware raw/when/shell/script rules §5.1; native shell expansions не являются Takt refs | injection/quoting/native-shell contract tests A0 |
| U-12 Archon actions | владелец runtime | `loop` normalizes; `until_bash` required AND; `cancel` durable | action/failure/recovery tests A1 |
| U-13 provider/context | владелец config/runtime | A0 bindings fail-before-Run/default fresh; A1 shared source однозначен в DAG | preflight A0 + session/parallel-rejection tests A1 |
| U-14 fixture/state | владелец tests/runtime | pinned MIT source; `workflow_contract` различает new/legacy state; old Run inspect-only | provenance + incompatible-definition tests |
| U-05 merge | владелец runtime при появлении use case | worktree fan-out объединяет только outputs/artifacts; mutation классифицирует автор | explicit merge + E2E конфликта/provenance двух worktrees |
| U-06 budgets | владелец assistant integration | hard claim запрещён без capability proof | live event/control conformance + budget tests |
| U-07 work items | владелец workflow authoring | item не выбирает worker/node/toolset | schema/authoring rejection tests |

Если реализация обнаружит факт, меняющий эти контракты, нужно остановиться и
вернуться в `design-unknowns`; нельзя скрывать изменение в local adapter или
prompt.

### 12.1. Контракт передачи в реализацию

Соблюдать решения и ограничители этой спеки. A0 нельзя делить на mergeable
часть со schema switch и отложенную миграцию content; dual-parse, второй
renderer/`when` dialect и silent provider fallback запрещены.

Если обнаружится существующий reference context, `output_format` consumer,
execution surface, persisted state или Archon construct, для которого таблица
§5 либо A0/A1 failure contract не задаёт однозначного поведения, остановить
реализацию и вернуть задачу в `design-unknowns`. Зафиксировать найденный
источник, опровергнутое решение, затронутые контракты, уже выполненный diff и
безопасное состояние ветки.
