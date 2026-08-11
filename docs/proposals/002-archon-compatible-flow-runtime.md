# Proposal 002. Archon-first Takt YAML и durable Flow Runtime

**Дата:** 2026-08-11
**Статус:** draft; решение о едином Archon-first Takt YAML зафиксировано, обновлённый language contract требует ревью; proposal не является реализацией
**Ветка:** `stabilize/live-host-conformance`
**Изменения кода:** отсутствуют
**Область:** универсальные локальные trusted-workflows для coding-agent, deterministic actions, approvals и domain tools

## 1. Зачем нужен этот proposal

Takt уже реализует почти все нужные orchestration primitives: DAG,
`loop_group`, Markdown `command`, child Runs, typed fan-out, worktrees,
artifacts, assistant sessions и deterministic actions. `command` и inline
`prompt` уже взаимоисключающие; `output_format` уже optional; loop history,
`${loop.previous.<node>.output}`, fan-out fingerprint и exact resume внутри
retry уже durable.

Поэтому проблема не требует нового flow runtime или второго языка. Тяжёлый
authoring в основном сосредоточен в контенте встроенного профиля `code`, где
workflow вручную повторяют status/code envelopes, `output_format`, artifact
paths и acceptance nodes. Runtime уже достаточно богат, но его публичный YAML
выбран неудачно.

Целевой контракт — один нативный Takt YAML. Его базой становятся first-class
workflow-конструкции Archon; Takt расширяет ту же схему своими durable и
governance-возможностями. Отдельного Archon dialect, importer, transpiler,
пользовательского compile-step или второго «внутреннего Takt YAML» нет.

Pinned базовые Archon flow должны проходить parse/validate/normalization Takt
без предварительного source rewrite. Это не обещание execution compatibility
для Archon-specific command layout, provider names, UI или инфраструктуры.

Основой публичного Takt YAML становится модель:

```text
workflow YAML
  → command/prompt/bash nodes
  → loop/loop_group
  → until signal
  → $node.output и $LOOP_PREV.<node>.output
```

Takt должен сохранить эту простоту и свои уже реализованные свойства:

- durable Run state и events;
- exact session resume внутри retry;
- worktree/artifact provenance;
- node timeout, retry bounds и loop `max_iterations`;
- deterministic actions и durable validation evidence;
- run inspection и сравнение workflow revisions;
- governed child Runs и fan-out.

Работа разделяется на authoring switch A0 и runtime semantics A1. A0 включает
замену публичной Workflow schema, единую reference/`when` grammar,
context-aware escaping, provider/model mapping, safe `context:fresh` и
атомарную миграцию всего
актуального in-repo Workflow content. A1 закрывает runtime gaps:

1. переносом assistant Session ID между итерациями одного logical loop node;
2. разделением product validation RED и execution failure;
3. deterministic authority, не позволяющей reviewer signal перекрыть RED;
4. native `loop`/`cancel` и required-AND `until_bash`;
5. node `context:shared` с однозначной последовательной session source.

Inspector projection по node и iteration остаётся отдельным следующим срезом:
он использует появившиеся durable данные, но не входит в A1.

A0 не публикует schema-only обещания: `loop`, `cancel`, scalar `until` и
`until.signal`,
`until.requires`, `until_bash`, `fresh_context` и `context:shared` остаются
невалидными до A1.
Для миграции текущего content A0 сохраняет исполнимый structured
`loop_group.until` с `exit_code|output_contains` уже внутри нового root/reference
language.

Дополнительные удобства — budgets внутри loop, автоматический artifact manifest
и allowlist типов dynamic work items — рассматриваются отдельно и не являются
основанием для rewrite.

Целевая формула:

```text
Takt YAML
= Archon first-class flow constructs
+ Takt durable/governance extensions
```

`$node.output`, `$LOOP_PREV...`, scalar `until`, `until_bash`, `loop` и
`loop_group` являются нативными конструкциями целевого Takt YAML, а не aliases
внешнего языка. Loader всё равно преобразует YAML в runtime definition, как
любой parser, но это внутренняя реализация одного языка, а не граница между
двумя пользовательскими форматами.

Текущий `takt/v1alpha1` остаётся описанием фактически реализованного контракта
до выхода новой версии. Обратная совместимость с его Workflow authoring не
является целью. Dual-parse запрещён: A0 атомарно меняет schema/loader,
generators/rewriters, все актуальные in-repo YAML definitions с `kind: Workflow`,
включая hidden `.takt/workflows` skill/example assets, активные Markdown
commands с runtime references, examples, tests, skills и current docs.
Config/package/evaluation документы сохраняют собственные схемы.
Старые Run после switch inspectable, но не resumable/retryable/forkable.
Новый durable state помечается внутренним
`workflow_contract: takt-flow/v1alpha1`; отсутствие поля однозначно обозначает
legacy Run и проверяется до попытки сопоставить новую definition. Это не поле
публичного YAML и не state migrator.

Takt не становится domain runtime и не копирует внутренний tool loop
OpenCode/Pi.

## 2. Основные решения

### 2.1. Workflow остаётся DAG

Обычный workflow и тело `loop_group` — ациклические DAG с `depends_on`,
`when`, parallel waves и join semantics.

Обратных рёбер в DAG нет. Возврат к предыдущей фазе моделируется повтором
заранее объявленного DAG-body:

```text
loop_group
  iteration 1: analyze → work → validate → review
  iteration 2: analyze → work → validate → review
  iteration N: ...
```

`loop_group` завершается по `until` signal или получает terminal failure при
исчерпании `max_iterations`.

### 2.2. Gate не является отдельным routing DSL

Публичный `Gate` node, `repair_target`, список разрешённых переходов и
arbitrary `next` из ответа модели не вводятся.

Проверочная граница обычно состоит из обычных nodes внутри `loop_group`:

```text
deterministic check
  → review command/prompt
  → until signal
```

Deterministic check создаёт факты и report. Review-agent анализирует их,
сопоставляет с критериями и выдаёт completion signal, например `BUILD-CLEAN`.
Если проверка не прошла, reviewer не выдаёт signal, и body запускается снова.

Это authoring-модель, а не утверждение, что текущий scheduler уже умеет
связать signal с результатом другого node. В текущем v1alpha1 `until` проверяет
только terminal status/exit code/output одного указанного node. В target spec
выбран bounded `requires`-список простых predicates; normalized validation
action не вводится. Primary `until` владеет единственным completion signal;
`requires` принимает только `exit_code|output_contains`. Несколько модельных
мнений при необходимости сводит объявленный review node, а не второй signal
DSL.

### 2.3. `until` — простой completion contract

В целевом Takt YAML пользователь пишет нативную короткую форму:

```yaml
until: BUILD-CLEAN
```

или:

```yaml
until: ROUTE-CLEAN
```

Signal обычно выдаёт последний `review` node. В более детерминированном
варианте completion подтверждается выбранным validation contract и безопасным
signal matcher-ом, а не произвольной substring в тексте reviewer. Если задан
`until_bash`, Takt выполняет его даже при удовлетворённом primary `until` и
требует exit 0 вместе со всеми primary/`requires` conditions;
OR/short-circuit semantics Archon намеренно не наследуется. Normalized
validation action не вводится.

Текущий v1alpha1 пока принимает структурированную форму
`until: {node: review, output_contains: BUILD-CLEAN}`. Это факт старой
реализации, а не целевая authoring-модель. В новом Takt YAML scalar `until`
является first-class формой; расширенная форма с `node` и `requires` нужна,
когда completion зависит от нескольких узлов.

В текущем runtime signal сам по себе ещё может завершить loop при успешном
`review`, даже если другой validator вернул RED. Это implementation gap,
закрываемый target `requires` contract.

### 2.4. Переменные переносят feedback, а не скрытый runtime state

Публичная форма целевого Takt YAML:

```text
$<node>.output
$<node>.output.<field>
$LOOP_PREV.<node>.output
$ARTIFACTS_DIR
```

Она является частью единой grammar, а не только shorthand для node output.
Target spec задаёт полный migration map: `$ARGUMENTS`, `$INPUTS`, node
output/state/artifacts, `$LOOP_PREV`, `$FEEDBACK`, `$FANOUT` и captured approval
через `$<approval-node>.output`, а также Archon `$BASE_BRANCH` из Run/worktree
context. `when` использует те же `$...` references и сохраняет только `==`,
`!=`, `&&`, `||`; сравнение остаётся строковым без numeric/boolean coercion,
`exit_code` — decimal text, JSON scalar — runtime JSON text.

На первой итерации `$LOOP_PREV.<node>.output` даёт пустую строку, как в базовой
модели Archon. На следующих итерациях он содержит bounded output предыдущего
body node. Полный текст и большие traces сохраняются как artifacts, а в prompt
передаются ссылка, digest и bounded projection. Неизвестный node или field
остаётся authoring/render error; пустота разрешена только для корректной ссылки
`$LOOP_PREV` на отсутствующую первую итерацию.

Runtime может переиспользовать существующие lookup/template primitives, но
`${nodes...}` и `${loop.previous...}` не являются вторым публичным языком и не
обязаны сохраняться после alpha-перехода.

В prompt/display значения подставляются как raw text; в `bash`/`until_bash`
node values shell-quote'ятся, а user-controlled reserved inputs доставляются
через environment. Inline script source не получает raw user substitution —
для него используются args/env. Эти правила являются частью языка, потому что
без них одинаковый YAML имеет разную safety/behavior semantics.

Takt lexer распознаёт в shell только полные target references: declared
`$node.output|state|artifacts`, `$LOOP_PREV...`, `$INPUTS...`, `$FANOUT...` и
reserved bare contexts. Обычные `$PATH`, `${PATH}`, `$?`, `$$`, `$((...))` и
`$(...)` остаются shell syntax. Это необходимо для byte-for-byte
Archon fixtures и не возвращает старый `${nodes...}` dialect.

`output_format` остаётся optional Takt extension и сохраняет текущий
`takt-schema-subset/v1`, fail-closed JSON validation, authoring-проверку nested
paths и `items_from`; A0 переносит его без изменения.

Пример:

```yaml
- id: implement
  prompt: |
    Исправь реализацию.

    Feedback предыдущей итерации:
    $LOOP_PREV.review.output
```

Автору не нужно вручную копировать report из одного шага в другой.

### 2.5. Агент создаёт данные, scheduler создаёт обязательные Runs

Analysis-agent может определить набор work items, но не вызывает произвольные
следующие nodes.

```text
analysis output
  → typed work items
  → scheduler fan-out
  → child Runs
  → join
```

Worker profile, allowed tools, skill и join policy определены flow. Агент
может выбрать данные элемента, но не расширить граф неизвестным типом команды.

Нативные subagents OpenCode/Pi разрешены для внутреннего reasoning, если их
результат не является отдельной обязательной фазой процесса. Обязательные
работы должны быть Takt nodes/child Runs, иначе Takt не сможет дождаться,
ограничить и проверить их результат.

### 2.6. Archon-first fields получают Takt semantics

- `provider` разрешается через существующий `assistants` Config registry,
  `model` — через `models`; unknown binding fail-before-Run и не игнорируется;
- node `context:fresh|shared` имеет safe default `fresh`; `shared` продолжает
  только однозначную последовательную upstream-сессию explicit `depends_on`
  ancestor того же provider binding и logical model, не создаёт implicit edge,
  concurrent reuse/mismatch fail-closed; root context не нужен;
- `loop` нормализуется в одноузловой `loop_group`, не создавая второй executor;
- `cancel` завершает весь Run как durable `cancelled` с workflow source/reason и
  каскадом на child Runs; внутри `loop_group` сохраняются active iteration/path
  и не запускаются predicate или следующая iteration;
- `until_bash` является required deterministic predicate: numeric non-zero
  повторяет iteration, execution failure останавливает Run.

Root становится плоским: `metadata.name|description|labels` переносятся в
`name|description|labels`, `defaults.assistant|model` — в `provider|model`, а
`input|hooks|worktree` сохраняются прямыми Takt extensions. Node `assistant`
переименовывается в `provider`; остальные governance fields сохраняются.
Старый `session: resume` не маппится вслепую в новый cross-node
`context: shared`: retry принадлежит `attempts.retry_session`/hook policy,
межитерационная continuity — A1 `fresh_context`, а неподтверждённый first entry
остаётся fresh.

## 3. Публичная модель Flow

Ниже сокращённая проекция нормативной схемы target spec; полный reference,
failure и migration contracts принадлежат спецификации.

### 3.1. Линейный workflow

```yaml
name: pull-request-review
description: Review an existing pull request and publish findings

nodes:
  - id: load-change
    command: scm-load-change

  - id: review
    command: pr-review
    depends_on: [load-change]

  - id: publish-review
    command: scm-publish-review
    depends_on: [review]
```

Узел `command` — bounded work unit. Он не обязан содержать весь процесс.
Markdown frontmatter принимает `description`, `argument-hint`, `provider` и
`model`; текущий `assistant` переименовывается в `provider` при A0. Node fields
имеют приоритет над command, затем используются root defaults.

### 3.2. Feature development с repair loop

```yaml
name: feature-development
description: Implement a planned feature and create a pull request

nodes:
  - id: plan
    command: feature-plan

  - id: build
    depends_on: [plan]
    loop_group:
      until: BUILD-CLEAN
      max_iterations: 5

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

Markdown command `feature-implement` содержит рабочую инструкцию и читает
`$LOOP_PREV.review.output`. Команда `feature-review` читает
`$validate.output` deterministic `bash`-узла, сопоставляет результат с
ticket и выдаёт `BUILD-CLEAN` только при готовности. Assistant `command` не
заменяет deterministic validator. `command` и inline `prompt` уже сейчас не
смешиваются в одном node.

Текст `create-pr` не является доказательством внешнего эффекта. Project-owned
`verify-pr.sh` (или verified SCM adapter) заново читает SCM state и проверяет
receipt/base/head; только этот node является deterministic publication check.

Поведение при падении тестов:

```text
iteration 1:
  implement → validate FAIL → review без BUILD-CLEAN

iteration 2:
  implement получает $LOOP_PREV.review.output
  → исправляет код
  → validate PASS
  → review выдаёт BUILD-CLEAN

после loop_group:
  create-pr → verify-pr
```

Если `validate` не может запуститься из-за отсутствия Docker, бинарника или
workspace, это execution failure, а не обычный product RED. Такой failure
останавливает loop/Run и требует retry или operator action.

### 3.3. Dynamic fan-out после анализа

Если анализ действительно порождает разные обязательные работы, он возвращает
данные, а не вызовы:

```json
{
  "work_items": [
    {
      "id": "tests",
      "kind": "test-author",
      "goal": "Добавить regression tests для R-4"
    },
    {
      "id": "implementation",
      "kind": "feature-builder",
      "goal": "Реализовать обработку null input"
    }
  ]
}
```

Scheduler проверяет список, создаёт worker Runs и ждёт join:

```text
analysis
  → work_items
  → fan-out(test-author, feature-builder)
  → join(all_success)
  → validate
  → review
```

Если список меняется между resume/retry, flow фиксирует новый fingerprint и
не должен молча переиспользовать старую группу child Runs.

Сейчас `items_from` уже проверяет JSON array и durable fingerprint, но элементы
остаются произвольным JSON. Проверка `kind` против объявленного flow allowlist —
отдельный P1 gap (U-07); приведённый пример не описывает существующую гарантию.

## 4. Семантика loop и failure

### 4.1. Product validation failure внутри loop

Целевая семантика: внутри repair loop RED проверки — ожидаемый результат
итерации:

```text
check completed
report.valid = false
review completed without until signal
loop iteration ends unsatisfied
next iteration starts
```

Archon часто печатает `VALIDATION: FAIL` и завершает process с exit code 0.
Target Takt contract выбирает более проверяемую границу: validator возвращает
ненулевой exit code с `allow_failure: true`, а review signal и validator exit
объединяются scheduler predicate. Ядро не декодирует `takt-validation/v1alpha1`;
envelope остаётся domain/evaluation evidence.

### 4.2. Execution failure внутри loop

Это другой класс:

```text
command not found
provider timeout
session resume failed
tool protocol error
workspace lock error
```

Такой node получает failed/timed_out/errored status. `loop_group` не должен
интерпретировать его как обычный feedback и бесконечно продолжать.

### 4.3. Failure обычного DAG node

Если node находится вне loop:

```text
node failed
→ dependent nodes skipped/blocked
→ always_run cleanup может выполниться
→ Run failed
```

Resume повторяет failed/unfinished часть DAG и не создаёт обратное ребро.
Completed mutating nodes не должны молча повторяться, если это могло создать
внешний side effect.

### 4.4. Exhausted loop

Если `max_iterations` исчерпан без `until` signal:

```text
loop_group = failed/exit
last review output и artifacts сохранены
Run = failed; исчерпанный limit требует fork с новой definition/лимитом
```

`max_iterations`, token/tool/wall-time budgets и repeated diagnostic
fingerprints являются отдельными ограничителями. Ошибка не превращается в
успешное завершение только потому, что loop закончился.

### 4.5. Final external check

После успешного `loop_group` внешний deterministic node всё равно может
упасть:

```text
build loop PASS
→ create-pr
→ verify-pr FAIL
→ Run failed
```

Автоматического возврата к `create-pr` нет. Для такого поведения `create-pr`
и `verify-pr` должны быть помещены в отдельный `loop_group`, либо Run должен
быть возобновлён оператором.

## 5. Сессии, worktree и artifacts

### 5.1. Три разные плоскости состояния

```text
Session
  приватный reasoning и provider history конкретной роли

Worktree
  изменяемые тесты, код, Route YAML и текущий candidate

Artifacts
  immutable reports, traces, diffs, receipts и digests
```

Обязательный результат workflow не может существовать только в provider
session. Если следующий node или review/check должен его проверить, результат должен
быть записан в worktree или artifact store.

### 5.2. Session continuity

В текущем runtime exact resume уже действует для retry одного node. Между
итерациями `loop_group` child `NodeState` удаляется, а вместе с ним теряется
`SessionID`; повторный logical node запускается fresh. Для continuity нужен
новый durable mapping `loop node path → session`.

Target spec фиксирует следующие правила:

```text
первый вход в node → context:fresh по умолчанию
последовательный другой node → shared только при explicit context:shared
повторный вход в тот же logical loop node → fresh_context policy
retry попытки → attempts.retry_session policy
```

Это позволяет исправлять результат в том же причинном контексте:

```text
implement session I
  → validate RED
  → следующая loop iteration
  → implement session I resume с новым feedback
```

Exact resume failure не заменяется fresh session молча. Fresh context должен
быть отдельным осознанным выбором flow/profile.

Continuity не означает безусловный provider guarantee: одна resumed session растёт с
каждой итерацией вплоть до `max_iterations = 64`, а provider может быть не
способен безопасно rebinding-нуть session на новый cwd/worktree. Нужен явный
`fresh_context` mode; на итерации N>1 он имеет приоритет над root/node
`context`, а `attempts.retry_session` управляет retry внутри итерации. Скрытый
fresh fallback запрещён.

### 5.3. Worktree policy

Целевая hybrid policy:

- последовательные шаги одного Run используют общий execution worktree;
- read-only parallel workers могут использовать parent workspace;
- параллельные mutating child Runs получают отдельные managed worktrees;
- fan-out join агрегирует outputs/artifacts, но не делает implicit merge в candidate;
- конфликт merge — failure, а не silently-last-writer-wins;
- worktree snapshot/digest фиксируется до review/check.

Child Run всегда сохраняет собственные state/events/artifacts. `inherit`
может разделять execution workspace, но не должен смешивать provenance.

Первые три свойства уже поддерживаются. Merge mutating child worktrees в один
candidate отсутствует. До U-05 child file changes не попадают в parent; join
агрегирует только outputs/artifacts. Автор ограничивает worker read-only либо
использует явную merge command; runtime не заявляет недоказуемую
mutating/read-only классификацию.

### 5.4. Artifact propagation

Runtime уже знает текущий execution workspace и bounded outputs зависимостей,
но prompt получает output/artifact paths/digests только через явные template
expressions. Target spec сохраняет явную передачу:

- текущий execution workspace;
- bounded outputs зависимостей;
- artifact manifest зависимостей;
- paths и digests доступных reports;
- предыдущую итерацию через `$LOOP_PREV`.

Полные traces не копируются в prompt. В prompt передаётся projection и ссылка:

```text
test-report:
  artifact: .takt/runs/.../artifacts/test-report.json
  sha256: sha256:...
  summary: 1 failed assertion, 184 events
```

Automatic manifest projection не входит в target core: profile может скрыть
повторяющийся bounded manifest в reusable command. Artifacts, SHA-256 и explicit
references уже реализованы.

## 6. Review/gate reports и LLM interaction

### 6.1. Review node

Review — обычный `command`/`prompt` node внутри loop. Он получает:

- исходное ТЗ/ticket;
- current worktree candidate;
- deterministic validation outputs;
- artifact manifest;
- `$LOOP_PREV.review.output`;
- read-only domain tools, если они нужны для анализа.

Он не меняет worktree и не запускает произвольный следующий node.

### 6.2. Стандартизированный runtime result

Пользователь может писать обычный Markdown prompt. Ниже показана целевая
inspector/eval projection, а не существующий persisted review envelope:

```json
{
  "node": "review",
  "iteration": 2,
  "signal": "BUILD-CLEAN",
  "output": "...bounded review text...",
  "artifacts": ["review-report"],
  "workspace_digest": "sha256:...",
  "session_id": "..."
}
```

Для downstream machine branching можно дополнительно объявить `output_format`.
Он не является обязательным для каждого prompt.

### 6.3. Deterministic authority

Это целевая гарантия, но не текущая семантика `until`. Target `requires`
predicate оставляет LLM signal условием выхода, но не заменой validator:
scheduler обязан увидеть required validation PASS и только затем принять
`BUILD-CLEAN`. Сила predicate равна source node: `bash`/`script`/verified
adapter дают deterministic evidence, assistant `command` остаётся advisory.

Полный reviewer report может быть текстовым artifact. Если domain package
умеет выдавать структурированный validation report, Takt сохраняет его
отдельно. Core не интерпретирует domain-specific codes; он видит только общий
validation contract, node status, artifacts, digest и iteration.

## 7. Data flow на примере feature development

### 7.1. Input

```text
tracker issue / user request
  → Run input
  → managed execution worktree
  → initial plan artifact
```

### 7.2. Iteration

```text
plan/previous review
  → implement agent session
  → files in worktree
  → deterministic tests/lint/build
  → report artifact
  → review LLM
  → until signal or feedback
```

### 7.3. Repair

```text
review FAIL
  → loop_previous snapshot
  → same logical implement node
  → resume или fresh согласно явной session policy
  → current worktree + report artifacts
```

### 7.4. Success

```text
review BUILD-CLEAN
  → loop_group completed
  → create-pr child/action
  → verify receipt/base/side-effect
  → final Run result loaded from Store
```

## 8. Data flow на примере schema-constrained YAML generation

Domain package не переносит семантику DSL в Takt core. Он предоставляет
skills, catalog tools, validators и acceptance commands.

```yaml
name: route-generation

nodes:
  - id: route-build
    loop_group:
      until: ROUTE-CLEAN
      max_iterations: 6
      nodes:
        - id: route-implement
          command: route-builder

        - id: route-check
          bash: ./tools/route-validate
          allow_failure: true
          depends_on: [route-implement]

        - id: route-review
          command: route-review
          depends_on: [route-check]

  - id: runtime-acceptance
    bash: ./tools/route-runtime-acceptance
    depends_on: [route-build]

  - id: same-digest-repeat
    bash: ./tools/route-repeat-acceptance
    depends_on: [runtime-acceptance]
```

Команда `route-builder` читает исходное ТЗ, workspace artifacts и
`$LOOP_PREV.route-review.output`. Команда `route-review` читает
`$route-check.output`, сопоставляет Route YAML с ТЗ и выдаёт `ROUTE-CLEAN`
только после успешной validation и проверки покрытия требований.

Если route validator возвращает domain diagnostic, review передаёт точный
report в следующую итерацию. Takt не знает, что означает этот код. Domain
reviewer знает его смысл; Takt знает только node status, artifacts, digest,
iteration и budgets.

## 9. Data flow на примере PR review

```text
load PR
  → parallel read-only reviewers
      security review
      correctness review
      tests review
  → join(all_done)
  → synthesis review loop (если нужны уточнения)
  → approval
  → publish review
```

Read-only reviewers могут разделять workspace. Их отчёты — immutable artifacts.
Mutating remediation, если разрешена, запускается отдельным bounded worker Run
после approval и снова проходит deterministic checks.

## 10. Boundaries

### 10.1. Takt owns

- DAG/loop scheduler;
- node ordering, join и trigger semantics;
- Run lifecycle, resume, pause, cancel;
- session request fresh/resume contract;
- worktree lifecycle и isolation policy;
- artifact persistence и provenance;
- tool/skill capability policy at node boundary;
- budgets и stop reasons;
- normalized events, usage и inspector;
- generic output/`until`/`when` mechanics.

### 10.2. Coding-agent host owns

- model conversation and context compression;
- internal filesystem/tool/MCP loop;
- provider-specific session implementation;
- native subagents that are not required Takt phases;
- provider-specific tool schema and event transport.

Takt не реализует второй universal tool loop.

### 10.3. Domain package owns

- domain DSL/catalog/schema;
- domain tools and commands;
- domain validation and runtime acceptance;
- domain report details;
- transaction/rollback semantics for domain mutations;
- domain-specific reviewer prompt/skill.

Feature, PR и security packages владеют своими критериями. Они подключаются
как commands, MCP/process tools и skills, без Go-адаптера на каждый инструмент.

### 10.4. User owns

- flow structure;
- command/skill definitions;
- loop bounds;
- tools/skills/models per node;
- deterministic commands;
- review criteria and completion signal;
- approval points;
- acceptable side effects.

Пользователь не обязан описывать внутренний event protocol, artifact digest,
provider argv, retry state или domain-specific status mapping.

## 11. Failure matrix

| Ситуация | Внутри `loop_group` | Вне `loop_group` |
|---|---|---|
| Validator non-zero + `allow_failure` | Report, no `until`, next iteration | Node completed с RED evidence; downstream поведение задаёт DAG |
| Validator non-zero без `allow_failure` | Loop failed, downstream skipped | Node failed, Run failed |
| Reviewer без signal | Feedback в `$LOOP_PREV...`, next iteration | Обычный completed output; сам по себе не является failure |
| Command/provider error | Loop failed, retry policy may run | Node failed, Run failed |
| Timeout/cancel | Parent reason has priority | Run timed_out/cancelled |
| Exact session resume failed | No silent fresh fallback | Node failed; operator/new explicit fresh path |
| Max iterations | Loop exhausted with final report | N/A |
| Merge conflict | Fan-out/join failed | N/A |
| External side effect unknown | Reconcile, no blind retry | Reconcile/needs_input |

## 12. Budgets и observability

Каждый Run хранит snapshot:

```text
flow fingerprint
command/skill/profile fingerprints
assistant/provider/model identity
session IDs и resume attempts
worktree revision/digest
artifact manifest
```

Runtime metrics:

- input/output tokens и cost;
- tool calls, denied calls и повторяющиеся inputs;
- wall time и idle intervals;
- retries и backoff;
- loop iterations;
- provider/transport failures;
- validation fingerprints;
- artifact/digest transitions;
- reason for terminal stop.

Usage, attempts, events, fingerprints и loop iteration history уже сохраняются
в durable state; это не означает, что scheduler умеет остановить каждый loop по
token/tool budget. В workflow spec нет отдельного loop budget contract.
Если host не отдаёт live tool/usage events, Takt не заявляет hard online
tool/token budget для этого adapter. Wall-time/process timeout и
`max_iterations` остаются enforceable.

Предлагаемый inspector:

```text
takt run inspect <run-id>
takt run inspect <run-id> --node build
takt run inspect <run-id> --iteration 2
takt run inspect <run-id> --json
```

Такой node/iteration projection отсутствует в текущем CLI/appapi (есть
`run.get`, `run.summary`, `run.events` и durable `loop_iterations`). Proposal
предлагает добавить тонкую read-only проекцию, не новый runtime state.

Она должна показывать не только последний output, но и:

```text
iteration 1: validation FAIL, report R1, session I, digest A
iteration 2: validation PASS, report R2, resumed session I, digest B
loop exit: BUILD-CLEAN
```

## 13. Evaluation

Eval запускает один и тот же corpus через версии flow/profile/command и
сравнивает две плоскости результата.

### Product plane

- final deterministic acceptance;
- checks/reports produced by domain validator;
- gate completion signal;
- artifact/digest validity;
- safe success vs safe stop.

### Process plane

- iterations;
- tool calls and repeated calls;
- tokens/cost;
- exact resumes/fresh sessions;
- time idle и retries;
- failure fingerprints;
- which iteration changed candidate;
- human interventions;
- reason for exhaustion.

Для изменения prompt, command, model или toolset baseline/candidate должны
получать одинаковые cases и одинаковый validator. Provider wall time не должен
использоваться как качество стратегии при неравномерной загрузке провайдера.

Aggregate `success@1` остаётся одной метрикой, но не единственным ответом.
Основной вопрос eval:

```text
какое изменение process policy исправило конкретный failure pattern
без регрессии других gate/results?
```

## 14. Запуск flow из coding agent

Flow и command/profile регистрируются как skill/catalog entry.

Coding agent может запросить запуск именованного flow:

```text
/takt feature-development --input issue-123
```

или вызвать Takt management tool, если host integration его поддерживает.

Agent может запустить flow, но не становится его scheduler:

```text
agent → request named flow
Takt → creates Run, DAG, budgets, worktree
Takt → returns Run ID/status/artifacts
```

Внутренние child Runs и approvals видны через тот же Run tree.

## 15. Tool и skill integration без custom adapters

Поддерживаются три универсальных пути:

```text
MCP server
  command + discovery

process tool
  JSON request/stdout result

deterministic executable/script
  command, stdout/stderr, exit code, artifacts
```

Domain-tool developer не пишет Go-код для подключения каждого validator/tool.
Takt отвечает за process lifecycle, cwd, timeout, redaction, artifact capture.
Domain package отвечает за report и semantics.

Skills определяют знания и prompt-процедуру. Takt profile определяет, какие
skills/tools доступны конкретному node.

Обязательный tool sequence внутри provider loop не задаётся. Если порядок
действительно является process boundary, следующий шаг становится отдельным
node/episode с другим toolset.

## 16. Что не входит в proposal

Не вводятся:

- отдельный сложный Gate DSL;
- arbitrary model-selected workflow targets;
- обязательные `before/after` hooks для каждого user tool;
- domain-specific rules в Takt core;
- новый универсальный agent tool loop;
- обязательный MCP-only validation;
- сложные ручные adapter implementations;
- интерпретация всех domain JSON outputs в runtime;
- автоматический выбор другой модели по одному failure без eval evidence;
- nested `loop_group` без отдельного production evidence;
- remote/multi-user/untrusted execution.

## 17. Тестируемый vertical slice до реализации

Сначала A0 contract suite должен доказать atomic language switch: pinned real
Archon fixtures, единую reference/`when` grammar, context-aware interpolation,
provider/model preflight, safe `context:fresh`, сохранение
`output_format`/schema-subset contract, полную миграцию in-repo
Workflow/Markdown-command content и fail-closed old Run resume. Dual-parse не
допускается.

После A0 нужен один минимальный A1 fake-host/fake-tool vertical slice:

```text
feature plan
  → loop_group
      implement output A
      check returns validation FAIL
      review returns feedback
      implement follows selected session policy and returns output B
      check returns validation PASS
      review returns BUILD-CLEAN
  → final command
```

Он должен доказать именно выбранные семантики:

1. loop завершается только когда одновременно выполнены safe signal,
   optional required `until_bash` и все `requires`;
2. product RED выражается выбранным validation contract и не маскируется как
   node execution error;
3. provider/process failure завершает loop safe-stop;
4. `$LOOP_PREV.<node>.output` содержит прошлый feedback;
5. full output и artifacts сохраняются per iteration;
6. same logical node выполняет явную `resume`/`fresh` policy без fallback;
7. downstream node запускается только после loop completion;
8. `max_iterations` и timeout останавливают runaway loop; token/tool budgets
   проверяются отдельно только после появления их loop contract;
9. final external check failure не создаёт скрытый backward transition;
10. operator retry failed Run не повторяет успешные mutating nodes без explicit policy.
11. `$LOOP_PREV.<node>.output` на первой итерации пуст, а неизвестные node/field
    остаются fail-closed.
12. native `loop` использует тот же `loop_group` scheduler, а `cancel`
    сохраняет workflow source/reason и каскадирует cancellation.

После fake slice нужен schema-constrained YAML vertical slice с реальным domain
validator, generated artifact и acceptance report. Его задача — доказать не
качество модели, а корректность границы Takt/domain package и удобство тюнинга
flow.

## 18. Open design questions

P0 закрыты target spec:

- U-01: bounded `until.requires` predicates;
- U-02: product RED = non-zero exit + `allow_failure`; core не декодирует domain
  envelope;
- U-03: `loop_group.fresh_context` и durable seeding из `loop_previous`.

До production-изменений эти решения должны быть доказаны fake-host contract
tests. Провал proof возвращает задачу в design review, а не создаёт workaround.

### U-04 / P1 — первая итерация (закрыто target spec)

`$LOOP_PREV.<node>.output` является валидной нативной ссылкой и на первой
итерации возвращает пустую строку. Неизвестный node/field и обычная `$node...`
ссылка на недоступный output остаются fail-closed.

### U-05 / P1 — parallel mutating merge (отложено)

Определить конфликт, provenance и resume двух child worktrees. До реального
потребителя использовать последовательную mutation; read-only fan-out уже
достаточен.

### U-06 / P1 — live budgets (guarded target spec)

Hard online token/tool budgets и pre-tool policies зависят от live Pi/OpenCode
event/control capabilities. Target loop budget использует
`enforcement: required|optional`; без capability proof hard claim запрещён.

### U-07 / P1 — typed work-item allowlist (закрыто ограничителем)

Model-selected `kind` не выбирает worker/node/toolset; `items_from` уже
обеспечивает array/fingerprint/join. Для разных ролей автор объявляет разные
fan-out nodes.

### U-08 / P0 — атомарная миграция (закрыто target spec)

A0 одновременно меняет schema/loader/generators и весь актуальный in-repo
Workflow/Markdown-command reference content. Отдельного Slice D и обещания
«старый content остаётся рабочим» нет; dual-parse запрещён.
Config/package/evaluation schemas не входят в switch.

### U-09 / P0 — единый Archon-first Takt YAML (закрыто target spec)

`until: BUILD-CLEAN`, `$ARGUMENTS`, `$INPUTS`, `$node.output/state/artifacts`,
`$LOOP_PREV`, `$FEEDBACK` и `$FANOUT` — first-class публичный контракт Takt
YAML. `when` использует те же references. Внутренняя нормализация допустима, но
не создаёт второй внешний syntax; старые `${...}` и `nodes.*` отклоняются.

### U-10 / P0 — interpolation safety (закрыто target spec)

Raw prompt, direct `when` resolution, shell-quoted bash/until_bash и argv/env
script delivery имеют разные explicit contracts. Unknown reference и escaping
failure fail-closed; silent empty и raw shell injection запрещены.

### U-11 / P1 — Archon execution parity (ограничено)

Pinned fixtures доказывают parse/validate/normalization, а не execution parity.
Takt намеренно использует required-AND `until_bash`, default `context:fresh` и
свой Config registry. Это не open compatibility promise.

## 19. Рекомендуемый порядок работ

1. Не менять production runtime до failing A0 contract tests.
2. Выполнить A0 одним mergeable language switch: schema/loader/renderer/when,
   output-format validation, generators, pinned fixtures и полная in-repo
   migration; завершить зелёным `make check`.
3. Выполнить A1: signal/`requires`, native `loop`/`cancel`, required
   `until_bash`, node `context:shared`, `fresh_context` и
   interrupted/operator retry semantics.
4. Добавить тонкий inspector projection поверх durable `loop_iterations`.
5. Прогнать schema-constrained YAML vertical slice с deterministic validator.
6. Реализовывать budgets и merge только после capability/use-case evidence.

Proposal остаётся rationale-документом; target spec уже отражена в карте
документов, но не объявлена текущим runtime-контрактом. `docs/05` фиксирует
нереализованные gaps. Workflow contract меняется только целиком в A0; до этого
production продолжает использовать текущий `takt/v1alpha1`.

## 20. Критерий успеха proposal

Proposal считается подтверждённым, если пользователь может описать feature,
schema-constrained generation и PR-review flow в едином Takt YAML на базе
Archon-конструкций, не зная внутренних Takt
protocols, и при этом runtime обеспечивает:

```text
same flow structure
→ bounded loop
→ real evidence
→ explainable repair iterations
  → durable artifacts/worktree state и доказанная session policy
→ safe stop when recovery is not justified
```

Качество domain result принадлежит проектным tools. Takt доказывает
повторяемость процесса, корректность границ, причины остановки и влияние
изменений flow/profile на результат.

## 21. Archon evidence

Референсный Archon source, проверенный на `dev` commit
`41765d6a1448da73f398a30e161f3b4eaba0b768`:

- `.archon/workflows/defaults/archon-feature-development.yaml` — feature flow
  с `implement → create-pr → verify-pr-base`;
- `.archon/commands/defaults/archon-implement.md` — внутренний validation loop;
- `.archon/workflows/rasmus-tests/t1-fix-issue.yaml` — multi-node
  `loop_group`, где `check` возвращает `VALIDATION: PASS/FAIL`, а `review`
  выдаёт `BUILD-CLEAN`;
- `packages/workflows/src/schemas/loop.ts` — `until`, `max_iterations`,
  `fresh_context`, `until_bash`;
- `packages/workflows/src/executor.ts` и `dag-executor.ts` — failure/resume
  semantics.

A0 vendored byte-for-byte `archon-feature-development.yaml`, A1 —
`t1-fix-issue.yaml`. Рядом сохраняются Archon MIT License и source metadata:
repository URL, исходный path, полный commit и SHA-256. Fixture tests проверяют
parse/validate/normalization; execution semantics принадлежат отдельным Takt
fake-host/E2E tests.

Этот proposal заимствует authoring model Archon, но не принимает без изменений
его ограничения: Takt должен durable-сохранять события, artifacts, usage,
worktree и session evidence и явно различать product RED от execution failure.
