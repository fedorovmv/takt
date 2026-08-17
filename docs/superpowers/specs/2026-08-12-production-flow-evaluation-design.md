# Production Flow Evaluation: дизайн оценки фактического результата

Статус: **APPROVED DESIGN; implementation not started**
Дата: 2026-08-12

## 1. Цель

Takt уже проверяет корректность scheduler, workflow-контрактов, assistant adapters,
retry/resume и deterministic nodes. Эти проверки не отвечают на более важный
продуктовый вопрос: решил ли production flow задачу фактически, если его агенты и
внутренние проверки считают работу завершённой.

Первый срез добавляет воспроизводимую оценку трёх точных workflow профиля `code`:

- `code:feature-development`;
- `code:comprehensive-pr-review`;
- `code:architect`.

Оценка сравнивает два независимых факта:

1. принял ли production flow результат (`Run.status == completed`);
2. признал ли результат корректным независимый executable post-run validator.

Главная метрика — `false_accept_rate`: доля запусков, в которых production flow
завершился успешно, но внешний oracle вернул `valid: false`.

## 2. Не-цели и границы

Срез не добавляет:

- второй runtime, scheduler или evaluation executor;
- eval-копии production workflow;
- внутренний tool loop coding-agent;
- LLM judge как источник `valid: true`;
- общий plugin/setup/teardown framework;
- пользовательские Go adapters;
- shell-код в expectation YAML;
- эмуляцию произвольных SCM, CI и tracker providers;
- обязательные live model evals в `make check`;
- универсальную формулу качества архитектуры.

Сохраняется конституция Takt: YAML координирует, код вычисляет, агент принимает
решения. Suite выбирает flow, corpus, approval, validator и gates. Предметную
корректность вычисляет versioned executable validator.

## 3. Выбранная архитектура

Flow evaluation является новым режимом существующего
`internal/tooling/evaluation`, а не новой подсистемой исполнения:

```text
takt eval flow
      │
      ├── загружает suite и self-contained cases
      ├── готовит изолированный Git/SCM fixture
      ├── запускает точный production selector штатным Run use case
      ├── удерживает managed worktree до oracle
      ├── запускает post-run validator
      └── сохраняет существующие metrics/fingerprints/evidence/gates
```

Tooling не собирает `runtime.Runner` или `store.FS` как второй composition root.
Он получает узкий callback исполнения case. Production wiring находится в
`internal/bootstrap` и вызывает стабильный application Run boundary. Это повторяет
подход существующего task benchmark и соблюдает архитектурные инварианты проекта.

Существующие `takt eval run|benchmark|report|compare` сохраняются. Первый срез не
переписывает и не удаляет старый interface: `eval flow` переиспользует его типы
результата, агрегацию и compare там, где их семантика совпадает.

## 4. Suite как единица воспроизводимого запуска

### 4.1. Структура

```text
evals/flows/feature-development/
  suite.yaml
  cases/
    implement-basic/
      input.md
      expected.yaml
      workspace/
        go.mod
        cmd/mini-du/main.go
      scm/                         # optional
        repository.yaml
        pull-request.yaml          # review cases
        head.patch                 # review cases
```

`suite.yaml` определяет общий production selector, config, validator, approval и
gates. Каталог case определяет один независимый эксперимент: точный input, исходный
репозиторий, bounded expectations и, при необходимости, SCM fixture.

### 4.2. Suite contract

Нормативная форма первого среза:

```yaml
version: takt-flow-evaluation/v1alpha1

workflow: code:feature-development
config: ../../config/opencode.yaml

cases:
  directory: cases

approvals:
  default: approved

external:
  github:
    mode: fixture
    require: repository

validator:
  id: mini-du
  version: "1"
  command: [go, run, ../../validators/mini-du]
  path: ../../validators/mini-du
  timeout: 2m
  max_output_bytes: 1048576

gates:
  validation_error_rate:
    max: 0
  valid_rate:
    min: 0.80
  false_accept_rate:
    max: 0.10
  unstable_cases:
    max: 1
```

Правила:

- YAML декодируется строго; неизвестные поля являются authoring error;
- все относительные пути разрешаются от каталога `suite.yaml`;
- `workflow` принимает production selector или путь, поддерживаемый штатным
  application resolver;
- `validator.command` является argv, а не shell-строкой;
- `validator.path` указывает файл или каталог, который входит в fingerprint;
- command должен быть доступен, а `path` существовать до первого model call;
- `timeout > 0`, `max_output_bytes > 0`, rate-пороги находятся в `[0,1]`;
- явный `--repeat` переопределяет значение suite, если оно будет добавлено позднее;
  в `v1alpha1` default равен `1`.

После materialization profile и до первого model call штатный workflow preflight
проверяет все достижимые provider/model references. Config suite обязан определять
логические model slots, используемые выбранным production graph. Для первых трёх
suite это `implementation`, `review` и, из-за `smart-review-block`, `routing`.
Отсутствующий slot является authoring error, а не неуспешным eval outcome.

`external.github` является закрытой границей первого среза, а не generic adapter
registry:

- отсутствие блока означает, что eval не подменяет `gh`;
- `mode` принимает только `fixture`;
- `require` принимает `repository|pull_request`;
- `fixture + repository` требует `scm/repository.yaml` и используется feature flow;
- `fixture + pull_request` дополнительно требует `scm/pull-request.yaml` и
  `scm/head.patch`, используется review flow;
- `fixture` всегда подменяет обычное разрешение команды `gh` fail-closed; при
  неполном fixture model call не начинается.

`validator.path` не исполняется Takt. Это только явная граница содержимого
валидатора для identity. Например, при `go run` fingerprint получает каталог
исходников, а не случайно найденный бинарник `go`.

### 4.3. Self-contained case

Обязательны:

```text
input.md
expected.yaml
workspace/
```

Case ID равен имени каталога и должен соответствовать
`[A-Za-z0-9][A-Za-z0-9._-]*`. После нормализации коллизии не допускаются.
Символические ссылки, выходящие из case directory, отклоняются до исполнения.

`input.md` передаётся production flow через штатную profile input preparation.
Eval не дописывает в него oracle criteria и не сообщает агенту, какие скрытые
дефекты проверяются.

`workspace/` — исходное состояние проекта именно этого case. Общий mutable
workspace template не используется: feature, review и architecture tasks требуют
разных seed code, defects и architecture.

`expected.yaml` имеет маленькую Takt-обёртку и непрозрачные для Takt данные oracle:

```yaml
takt:
  approval_answer: approved       # optional case override

oracle:
  allowed_paths:
    - cmd/mini-du/**
    - internal/**
  required_behavior:
    flags: [s, k]
    cases: [empty, nested, unicode, spaces, symlink, hardlink]
  required_artifacts:
    - summary.md
```

Takt понимает только `takt.approval_answer`. Поле `oracle` является bounded
declarative data конкретного validator; Takt не интерпретирует его как expression
или script. Полный файл передаётся validator через `expected_path`.

### 4.4. Почему cases не используют setup scripts

Общий template + setup script скрывает фактическое исходное состояние и создаёт
ещё один программируемый lifecycle. Self-contained fixture можно открыть и сразу
увидеть:

- какой код получил агент;
- какое задание он получил;
- какой дефект был посеян;
- какие свойства проверяет oracle.

Небольшое дублирование `go.mod` и skeleton-кода дешевле системы inheritance,
overlays или пользовательских adapters. Такие механизмы добавляются только после
фактического повторяющегося второго use case.

## 5. Подготовка case и порядок одного repeat

Порядок является частью контракта:

1. Eval копирует `case/workspace/` в новый case control workspace.
2. Если selector ссылается на профиль, resolver сначала ищет профиль в копии.
   Если его нет и имя является built-in profile, вызывается существующий
   `profile.Init`. Неизвестный внешний профиль должен уже находиться в fixture или
   обычном поддерживаемом profile location.
3. Suite config копируется в case под стабильным относительным путём **после**
   `profile.Init` и заменяет созданный им `.takt/config.yaml`. Для SCM case в
   assistant `env` добавляется стабильный overlay с `{{workspace}}`, а не
   абсолютный временный путь.
4. Подготавливаются repository, branches, commits, local bare remote и fake SCM
   fixture. Для простого workspace initial commit создаётся здесь; для PR case
   здесь же создаются base/head commits.
5. Только после profile/config/SCM preparation фиксируется immutable baseline
   snapshot точного agent-input checkout.
6. Production selector повторно разрешается через штатный application path уже в
   подготовленном control workspace.
7. Run запускается с `KeepWorktree=true`.
8. Каждый `ErrWaiting` получает approval согласно §6 и продолжается через штатный
   resume. Без ответа состояние остаётся `waiting`.
9. После `completed`, `failed`, `cancelled`, `abandoned` или `waiting` eval получает
   durable state и использует точный `state.ExecutionWorkspace`. Node timeout
   остаётся `Run.status=failed` с `error_code=timed_out`; отдельный Run status
   `timed_out` не вводится.
10. При доступном workspace запускается post-run validator.
11. Report и evidence сохраняются до cleanup.
12. Managed worktree и созданный case workspace удаляются, если пользователь не
   передал `--keep-workspaces`.

Cases и repeats одного suite исполняются строго последовательно в порядке
лексикографического case ID, затем номера repeat. `v1alpha1` не имеет
`--parallel`/`max_parallel`: реальные coding-agent процессы дороги, а последовательный
порядок проще воспроизводить и сравнивать. Независимая копия repository на каждый
repeat остаётся обязательной и позволяет позже добавить bounded parallelism без
изменения corpus contract.

Profile устанавливается до baseline commit. Поэтому `.takt/profiles/code`, его
commands и tools доступны managed worktree, но не выглядят как изменение агента.
Исходный каталог `case/workspace/` никогда не мутируется.

Каждый repeat начинает с новой копии case. Session ID, Git changes, approvals,
artifacts и fake SCM state предыдущего repeat не переиспользуются.

## 6. Approval semantics

Приоритет ответа:

```text
expected.yaml:takt.approval_answer
  → suite.yaml:approvals.default
  → оставить Run в waiting
```

В первом срезе один ответ применяется к каждому встретившемуся approval node.
Этого достаточно для выбранных production flows: только `architect` имеет один
approval. Mapping `node_id → answer` не добавляется без второго фактического case.

Отсутствующий ответ не является infrastructure error. Eval сохраняет `waiting`,
при наличии workspace запускает oracle и обычно проваливает completion gate.

## 7. Git и fake SCM boundary

### 7.1. Базовый Git fixture

Если `workspace/` не является Git repository, eval создаёт его сам с фиксированными
author name/email, branch и initial commit. Вложенный `.git` в source fixture в
первом срезе запрещён: Git history строится декларативно и воспроизводимо.

Для mutating flow создаётся локальный bare `origin`; настоящий network remote не
используется. Git commands остаются настоящими, поэтому commit/push behaviour
проверяется без публикации наружу.

Для PR review case базовая форма такова:

```yaml
# scm/repository.yaml
repository: acme/mini-du
base_branch: main
head_branch: feature/recursive-du
```

```yaml
# scm/pull-request.yaml
number: 17
title: Implement recursive disk usage
base: main
head: feature/recursive-du
state: OPEN
ci_status: passed
fixes_permitted: true
```

`workspace/` содержит base tree. `scm/head.patch` применяется через bounded
`git apply` на head branch и коммитится до baseline snapshot. Произвольный setup
script не выполняется. Unsupported/binary patch отклоняется в первом срезе.

После fixture preparation checkout соответствует состоянию, которое должен видеть
production flow. `baseline_workspace` для validator — именно этот agent-input
snapshot, включая посеянный дефект, а не только base branch.

### 7.2. Fake `gh`

Переиспользуется и расширяется `scripts/fixtures/fake-gh`. Текущий файл является
только seed implementation: разбор `FAKE_GH_FIXTURE_DIR`, dynamic repository/PR
metadata, head/base refs и полный call log являются новой работой Slice 2, а не
существующей возможностью. Для каждого repeat создаются:

```text
.takt/eval/bin/gh                 # committed baseline tool
.takt/eval/scm-fixture/           # committed declarative input
.takt/evals/scm/                  # ignored mutable calls/state
```

Stable assistant env overlay:

```text
PATH={{workspace}}/.takt/eval/bin:<host PATH captured at suite start>
FAKE_GH_FIXTURE_DIR={{workspace}}/.takt/eval/scm-fixture
FAKE_GH_STATE_DIR={{workspace}}/.takt/evals/scm
```

These paths are derived first from runtime-provided `TAKT_WORKSPACE` and then,
for an installed `.takt/eval/bin/gh`, from the script location. Both sources
are authoritative, so `FAKE_GH_*` cannot redirect fixture or state.
Environment lookup remains only for standalone fixture tests.

`{{workspace}}` рендерится существующим assistant environment renderer. Overlay
попадает в effective config fingerprint; временный абсолютный output path — нет.
Один snapshot host PATH применяется ко всему suite и входит в environment identity.

`{{workspace}}` означает `state.ExecutionWorkspace`. Поэтому fake `gh` и immutable
SCM fixture копируются **до baseline commit и коммитятся в agent-input Git tree**:
только тогда они существуют в managed worktree. Mutable `.takt/evals/scm` до
commit не создаётся и остаётся незакоммиченным runtime state, исключённым через
существующий Git local exclude. Реализация, положившая tool/fixture только в case
control workspace после commit, является ошибочной.

Глобальный `os.Setenv` запрещён: отдельные repeats и одновременно запущенные suite
processes не должны видеть SCM state друг друга. Unsupported fake `gh` invocation
завершается fail-closed с exit code `2` и пишется в call log. Поддерживаемый набор
команд расширяется только по реальным production-flow smoke, а не до общего GitHub
emulator.

Это не новая security boundary. Takt остаётся trusted local runtime: процесс,
который намеренно обходит `PATH` или использует другой сетевой клиент, не
изолируется этим fixture. В первом срезе inventory contract фиксирует, что выбранные
production flows обращаются к GitHub только обычной командой `gh` из assistant
process. Если flow добавит deterministic action, выполняющий внешний SCM side effect,
adapter или иной GitHub transport, suite отклоняется до обновления безопасной
fixture boundary. Проверяющий `pr-effect-gate` внешний side effect не выполняет.
Настоящие GitHub
credentials не передаются через suite overlay; для полного network deny нужен
отдельный доказанный assistant sandbox capability, а не заявление eval-runner.

Наличие GitHub boundary и обязательный fixture не выводятся из имени workflow.
Их задаёт `external.github`. Отсутствующий обязательный SCM-файл является authoring
error до model call. Это не позволяет новому workflow случайно обратиться к
настоящему `gh` из-за неизвестной эвристики selector.

## 8. Запуск точного production flow

Suite ссылается на selector:

```yaml
workflow: code:feature-development
```

Он не ссылается на файл внутри исходников Takt и не содержит eval-копию DAG.
Resolver загружает материализованный профиль, его subworkflows, Markdown commands,
tools и config так же, как обычный пользовательский Run.

Три первых flow выполняются без сокращений:

```text
feature-development:
  implement → validate-agent → deterministic validate → create-pr
    → eval-only pr-effect-gate → summary

comprehensive-pr-review:
  scope → perspectives → reviews → synthesize → optional fixes
        → validate → deterministic validation commands
        → review-acceptance-gate → summary

architect:
  architecture-review → approval → plan → implementation → smart review → summary
```

Eval подменяет только fixture workspace/input/approval, локальные внешние системы и
независимый post-run oracle. Он не меняет nodes, commands, retries, models, hooks,
gates или output formats production flow.

Production execution идёт через application use cases. Bootstrap callback запускает
case как `RunService.Start(... Detached:true, KeepWorktree:true)` и получает durable
Run ID, затем читает `GetRun` до terminal/waiting state. Approval продолжается
только через `RunService.Answer`; cancellation — через `RunService.Cancel`.

Detached здесь не означает fire-and-forget CLI: flow-eval остаётся foreground
командой и ждёт durable result. Этот путь выбран потому, что обычный foreground
`Start` при terminal failure возвращает ошибку без `StartResult`, тогда как eval
обязан проверить частичный workspace. Прямой вызов `runtime.Runner`, чтение
`store.FS` из tooling или второй executor запрещены.

Callback возвращает повторно загруженный durable `RunState`. Ошибка до durable Run
ID является infrastructure failure; terminal failed state является flow outcome.
Polling использует bounded interval и context. При отмене eval context callback
запрашивает cancellation активного Run, ждёт durable terminal state в ограниченном
cleanup context и не запускает post-run validator как новую работу.

## 9. Post-run validator protocol

### 9.1. Process boundary

Validator запускается одним локальным process:

```text
Takt ── validation_request JSON / stdin ──▶ validator
Takt ◀── takt-validation/v1alpha1 / stdout ── validator
                              stderr ── diagnostics only
```

Command выполняется относительно каталога suite, без shell. Timeout и общий
stdout+stderr output budget обязательны. Validator работает в локальном trusted
режиме Takt, но получает только явно перечисленные paths.

### 9.2. Request

```json
{
  "protocol_version": "takt-evaluation-validator/v1alpha1",
  "type": "validation_request",
  "case_id": "implement-basic",
  "repeat": 1,
  "workspace": "/tmp/.../result",
  "baseline_workspace": "/tmp/.../baseline",
  "expected_path": "/repo/.../expected.yaml",
  "run": {
    "id": "run-01",
    "status": "completed",
    "artifacts_dir": "/tmp/.../.takt/runs/run-01/artifacts"
  },
  "external_state": {
    "scm_dir": "/tmp/.../result/.takt/evals/scm"
  }
}
```

`workspace` берётся из durable `state.ExecutionWorkspace`, а не вычисляется из
имени flow. `baseline_workspace` является read-only snapshot состояния до Run.
`expected_path` указывает исходный case expectation. `external_state.scm_dir`
может отсутствовать у case без SCM.

Абсолютные paths допустимы только в runtime request. В сохранённом portable report
они нормализуются относительно evidence root; validator request сохраняется как
локальный debugging artifact.

### 9.3. Result

stdout содержит ровно один существующий строгий envelope:

```json
{
  "protocol_version": "takt-validation/v1alpha1",
  "type": "validation_result",
  "valid": false,
  "score": 78,
  "checks": {
    "build": {"passed": true, "score": 100},
    "tests": {"passed": true, "score": 100},
    "differential_behavior": {
      "passed": false,
      "score": 65,
      "weight": 4,
      "message": "2 of 20 scenarios differ"
    }
  },
  "diagnostics": [
    {
      "code": "HARDLINK_DOUBLE_COUNT",
      "severity": "error",
      "path": "internal/du/du.go",
      "message": "Two paths referencing one inode are counted twice"
    }
  ]
}
```

Используется `validation.Decode`; второй validation result format не создаётся.
`checks` остаются domain-specific и не интерпретируются Takt, кроме существующей
валидации envelope и агрегации score/diagnostics.

### 9.4. Exit и failure semantics

Нормативная матрица post-run process:

| Process | Stdout | `validation.status` | Quality |
|---|---|---|---|
| exit `0` | valid envelope | `completed` | `valid` из envelope |
| exit `0` | malformed/empty | `error` (`validator_protocol`) | unavailable |
| non-zero | любой | `error` (`validator_exit`) | не участвует в rates |
| timeout/cancel | любой | `error` (`validator_timeout|cancelled`) | unavailable |
| output overflow | любой | `error` (`validator_protocol`) | unavailable |

`valid: false` является штатным измеренным результатом и поэтому возвращается с
process exit `0`. Это отделяет плохой продукт от поломки измерительного контура.

Если process завершился non-zero, доступный корректный envelope может быть сохранён
как diagnostic evidence, но не считается completed validation. Эта process
семантика намеренно строже старого in-workflow quality-node: post-run validator
контролируется eval suite и обязан явно разделять quality от собственной ошибки.

Missing/unexecutable validator и отсутствующий `validator.path` проверяются до
model call и останавливают suite. Runtime timeout/protocol/exit одного validator
сохраняется в record, не маскируется как `valid: false`, не прерывает сбор остальных
cases и проваливает стандартный infrastructure gate.

### 9.5. Когда запускается validator

Oracle запускается после каждого доступного результата:

- `completed`;
- `failed`;
- `cancelled`;
- `abandoned`;
- `waiting`, если approval отсутствует;
- частичный Run с ошибкой, если durable state и workspace доступны.

Node-level timeout попадает сюда через `Run.status=failed` и сохранённый
`error_code=timed_out`. Если внешний context самого `takt eval flow` отменён,
новый validator process не стартует: durable Run/evidence сохраняются насколько
возможно, а команда возвращает cancellation.

`pausing|paused` не являются terminal evaluation outcome и недостижимы без внешней
operator-команды в выбранных flows. Если polling наблюдает их, eval сохраняет
durable state, не запускает oracle, не удаляет resumable workspace и завершает
suite с infrastructure diagnostic `run_paused`. Классификатор не должен паниковать
или считать paused как reject.

Если workspace отсутствует, validation получает `status: error` с точным
diagnostic. Oracle никогда не меняет `Run.status` и не превращает failed Run в
успешный.

## 10. Outcome model и метрики

### 10.1. Независимые результаты record

```yaml
execution:
  status: completed
  failed_node: null

validation:
  status: completed
  valid: false
  diagnostics:
    - code: HARDLINK_DOUBLE_COUNT

outcome: false_accept
gate:
  passed: false
```

`accepted` определяется только `Run.status == completed`. Все остальные Run
statuses являются `not_accepted`; исходный status сохраняется для анализа.

| Flow decision | Oracle | Outcome |
|---|---|---|
| accepted | valid | `true_accept` |
| accepted | invalid | `false_accept` |
| not accepted | invalid | `true_reject` |
| not accepted | valid | `false_reject` |

Термин `reject` в агрегате означает `Run.status != completed`, а не утверждает, что
каждый такой Run принял явное продуктовое решение. Например, `waiting` отдельно
виден в execution status.

Validation `status:error` не получает один из четырёх outcomes и учитывается как
`validation_error`.

### 10.2. Знаменатели

```text
total_runs = все начатые repeats

evaluated_runs = repeats с validation.status == completed

valid_rate = valid / evaluated_runs

false_accept_rate = completed_and_invalid / evaluated_runs

false_reject_rate = non_completed_and_valid / evaluated_runs

flow_completion_rate = completed_runs / total_runs

validation_error_rate = validation_errors / total_runs
```

Если `evaluated_runs == 0`, quality rates сериализуются как `null`, не `0`.
Измеренный ноль сериализуется как `0`. Это сохраняет действующую evaluation
семантику Takt.

### 10.3. Existing metrics

Переиспользуются:

- attempts, duration, input/output tokens и cost;
- assistant/version/requested/resolved model по execution record;
- score/checks/diagnostics;
- stable-valid, stable-invalid и unstable cases;
- cost per valid и amortized end-to-end duration;
- pairwise comparison и diagnostic fingerprints.

`time_to_valid_ms` старого quality-node измеряет durable время внутреннего узла и не
может ложно применяться к post-run oracle. Для flow-eval он равен времени от старта
Run до завершения post-run validator и должен храниться как flow-eval measurement,
не как synthetic node timestamp.

`attempts_to_valid` для post-run oracle не выводится из произвольного generation
node: весь production flow является оцениваемой стратегией. В первом срезе поле
недоступно (`null`), пока не появится доказанная межзапусковая repair semantics.

Для каждого evaluated repeat сохраняется также строгий per-run gate:

```text
run_passed = Run.status == completed && validation.status == completed && valid
```

Он не меняет ни execution, ни validation status и нужен для быстрого чтения одного
record.

### 10.4. Gates

Допустимые абсолютные gates `v1alpha1`:

- `validation_error_rate.max`;
- `valid_rate.min`;
- `false_accept_rate.max`;
- `false_reject_rate.max`;
- `flow_completion_rate.min`;
- `unstable_cases.max`.

Если `gates` отсутствует, применяются безопасные defaults:

```yaml
gates:
  validation_error_rate: {max: 0}
  flow_completion_rate: {min: 1.0}
  valid_rate: {min: 1.0}
```

Чтобы получить первый live baseline без заранее выдуманного quality threshold,
suite обязан явно задать только `validation_error_rate.max: 0`. Явный `gates`
полностью заменяет defaults, а не частично сливается с ними.

Deterministic fake-host suite может требовать:

```yaml
gates:
  validation_error_rate: {max: 0}
  valid_rate: {min: 1.0}
  unstable_cases: {max: 0}
```

Live model suite сначала запускается без quality threshold, кроме
`validation_error_rate: 0`, чтобы получить честный baseline. После baseline
фиксируются достижимые пороги `valid_rate`, `false_accept_rate` и instability.
Сразу требовать `valid_rate: 1.0` от стохастической модели не следует.

Нарушенный gate возвращает ненулевой CLI exit code после сохранения полного
report. Это не infrastructure error конкретного Run.

## 11. Fingerprints и воспроизводимость

Flow-eval report включает существующие strategy/benchmark identities и дополнительно
гарантирует fingerprint:

- разрешённого production workflow и всех subworkflows;
- materialized commands/tools/profile manifest;
- effective config вместе со стабильным eval env overlay;
- input, workspace tree, expected и SCM fixture каждого case;
- executable mode файлов;
- validator ID/version/path content;
- suite YAML;
- Takt version, GOOS/GOARCH/Go version;
- assistant/version/requested/resolved model и usage каждой attempt.

Mutable runtime output (`.takt/runs`, `.takt/worktrees`, `.takt/evals`) исключается
так же, как в существующем evaluation fingerprint. Изменение corpus, production
flow, command, config, validator или suite создаёт другой experiment identity.

Host PATH не сохраняется открытым в portable report. Сохраняется его SHA-256 и
использованный environment identity. Secret values проходят существующий redactor;
`secret://` разрешается только перед process execution.

## 12. Evidence и cleanup

На repeat сохраняются внутри выбранного evidence root:

```text
cases/<case>/repeat-001/
  run.json
  validation-request.json
  validation-result.json
  validator.stderr
  diff.patch
  artifacts/
  scm/
```

`run.json` строится из повторно загруженного durable state. Textual evidence проходит
общий redactor. Artifacts копируются после штатной runtime artifact/redaction
проверки; manifest сохраняет SHA-256 и provenance. Ошибка записи evidence является
infrastructure failure и возвращается вызывающему коду.

Cleanup начинается только после успешной записи state, validation result и
обязательного evidence. Удаляются только канонизированные каталоги, созданные
текущим eval repeat, внутри известного output root. Source suite/case/workspace не
может быть целью cleanup. Если удаление не удалось, report сохраняет точный cleanup
diagnostic и путь; уже измеренный quality не теряется.

`--keep-workspaces` оставляет baseline, control workspace, managed worktree и SCM
state, а report печатает их пути. Без флага тяжёлые workspaces удаляются после
evidence.

## 13. CLI user journey

### 13.1. Запуск

```bash
takt eval flow evals/flows/feature-development/suite.yaml
```

```bash
takt eval flow evals/flows/feature-development/suite.yaml \
  --case implement-multiple-paths \
  --repeat 5 \
  --keep-workspaces
```

Поддерживаемые flags первого среза:

- `--case ID` — точный case ID;
- `--repeat N` — число независимых повторов;
- `--output DIR` — явный output root;
- `--keep-workspaces` — не выполнять workspace cleanup;
- `--json` — machine-readable CLI output.

Без `--output` evidence root имеет единственную форму:
`.takt/evals/<suite-name>/<UTC timestamp>`. Поэтому repeat artifacts из §12
фактически находятся в
`.takt/evals/<suite-name>/<UTC timestamp>/cases/<case>/repeat-001/`.
Существующий explicit output не
удаляется и вызывает fail-before-model; destructive `--replace` новому interface не
нужен.

Неизвестный `--case` отклоняется до model call и сопровождается списком допустимых
IDs. Отдельные `retry`, interactive runner и glob language не добавляются. Повтор
провала — новый чистый запуск того же case.

### 13.2. Report и compare

Переиспользуются:

```bash
takt eval report <result-directory>
takt eval compare <baseline-directory> <candidate-directory>
```

Compare дополняется flow-level rates и pairwise outcome transitions, но не получает
второй report engine.

### 13.3. Init — отдельный поздний срез

После стабилизации suite schema добавляется:

```bash
takt eval flow init code:feature-development --output evals/my-flow
```

Она создаёт только `suite.yaml`, `cases/example/input.md`, `expected.yaml` и пустой
workspace. Validator не генерируется и предметная корректность не угадывается.
Генератор не входит в первый срез, чтобы не закреплять формат до реального corpus.

## 14. Первый corpus: `mini-du`

### 14.1. Контракт

```text
mini-du [-s] [-k] PATH...
```

Differential validator:

1. собирает candidate;
2. отклоняет делегирование системному `du`;
3. создаёт deterministic файловые деревья;
4. сравнивает exit code/stdout/stderr с системным `du` в выбранной переносимой
   семантике;
5. проверяет Git scope, tests, artifacts и SCM effects;
6. возвращает `takt-validation/v1alpha1`.

До model call validator выполняет self-check host oracle и фиксирует в benchmark
identity канонический путь, SHA-256 executable и доступную version signature
системного `du`. Reports с разной oracle/environment identity нельзя сравнивать как
один эксперимент. Это сохраняет честность различий GNU и BSD/macOS `du` без
попытки реализовать ещё один эталон `du` внутри Takt.

Первый corpus покрывает empty/nested trees, несколько PATH, Unicode и пробелы,
symlink и hardlink. GNU-only flags, mount boundaries и sparse-file edge cases не
входят в первый срез. Системный `du` используется только oracle, никогда candidate.

### 14.2. Cases

```text
feature-development:
  implement-basic
  implement-multiple-paths
  implement-symlink-and-hardlink

comprehensive-pr-review:
  review-hardlink-bug
  review-path-with-spaces
  review-unrelated-change

architect:
  remove-single-implementation-factories
  collapse-redundant-layers
  preserve-behavior-during-simplification
```

`feature-development` начинает с отсутствующей функциональности.
`comprehensive-pr-review` начинает с PR head patch с известным дефектом.
`architect` начинает с корректной, но заведомо переусложнённой реализации.

Architecture oracle не оценивает абстрактную «красоту». Он сначала требует
byte-for-byte observable behaviour equivalence baseline/candidate, затем проверяет
только заранее объявленные bounded smells: лишние single-implementation factories,
package-global registry, известные redundant layers и diff scope.

После стабилизации `mini-du` нужен второй независимый домен — небольшой HTTP/API
fixture. Он не входит в этот implementation cycle.

## 15. Реальные production-flow сценарии

### 15.1. Feature development

Oracle проверяет одновременно:

- сборку и differential behaviour;
- tests и разрешённый Git scope;
- обязательные artifacts;
- создание PR через fake `gh`;
- push в локальный bare remote.

Работающий binary без созданного PR является `valid: false`, потому что обещанный
workflow outcome не выполнен.

### 15.2. Comprehensive PR review

Fixture содержит base/head и известный defect. Oracle проверяет, что дефект найден и
исправлен, unrelated changes отсутствуют, а candidate проходит differential corpus.

Наиболее ценный outcome:

```text
Run.status = completed
review report = approved/fixed
oracle.valid = false
outcome = false_accept
```

Он показывает, что агенты и внутренние проверки приняли неработающий результат.

### 15.3. Architect

Flow выполняется полностью, включая approval, plan, implementation и smart review.
Oracle сравнивает baseline/candidate behaviour, затем bounded structural criteria.
Упрощение, изменившее CLI или поведение, всегда невалидно независимо от убедительности
architecture report.

## 16. Implementation slices

### Slice 1 — flow-evaluation mechanism

- strict suite/case loader;
- `takt eval flow` и selection/repeat/output/keep flags;
- application callback вместо второго executor;
- profile materialization до baseline;
- clean Git fixture без SCM emulation;
- production selector + `KeepWorktree`;
- approval continuation;
- post-run validator protocol;
- outcome/rates/gates;
- evidence, fingerprints и safe cleanup;
- fake-host contract на маленьком fixture.

### Slice 2 — local Git remote и fake SCM

- deterministic branches/commits/head patch;
- local bare `origin`;
- stateful existing fake `gh`;
- stable per-repeat assistant env overlay;
- PR create/review fixtures и call log;
- production-flow smoke трёх точных selectors.

### Slice 3 — `mini-du` corpus и validator

- один versioned Go validator на standard library;
- три suite и первые cases;
- deterministic validator self-checks;
- manual Pi/OpenCode smoke `repeat=1`;
- opt-in real runs и сохранённый release evidence.

### Slice 4 — authoring convenience

- `takt eval flow init` после доказательства schema;
- docs/examples/skill references;
- только затем решение, нужен ли прежний flag-heavy `eval run` interface.

Срезы не требуют отдельного runtime language field или durable scheduler semantics.
Если реализация обнаружит такую необходимость, работа возвращается на design review.

## 17. Verification strategy

### 17.1. Unit/contract

- strict suite decode и invalid fields;
- preflight отсутствующих `implementation|review|routing` model slots;
- case ID/collision/path containment;
- approval precedence;
- validator success/invalid/exit/timeout/malformed/overflow;
- exact outcome classification;
- `0` против `null` rates;
- gates;
- fingerprint sensitivity input/workspace/expected/SCM/validator;
- safe cleanup containment.

### 17.2. Fake-host integration

- profile materialized до baseline commit;
- точный production selector разрешён;
- execution проходит detached `RunService` path и durable reload, не runtime direct;
- completed/failed/waiting Run вызывает oracle;
- managed worktree доступен до oracle;
- `state.ExecutionWorkspace` является validator target;
- repeat получает чистый workspace/session/SCM state;
- cases/repeats выполняются в нормативном последовательном порядке;
- suite config заменяет config, созданный `profile.Init`;
- committed eval tools/fixture доступны из managed worktree, mutable SCM state не
  попадает в baseline;
- fake SCM не использует global environment;
- evidence записано до cleanup;
- validation error не превращается в invalid product.

### 17.3. Production-flow smoke

Точные `code:feature-development`, `code:comprehensive-pr-review` и
`code:architect` запускаются fake assistant с минимальными fixtures. Проверяются их
реальные DAG, commands, approval, worktree и SCM boundary. Smoke доказывает
measurement wiring, но не качество модели.

### 17.4. Release gates

`make check` и `scripts/verify.sh` используют только deterministic fake host и не
вызывают внешнюю модель. Live Pi/OpenCode eval выполняется opt-in при наличии
binary, credentials, model и validator; его outcome сохраняется честно и не
перезапускается до желаемого результата.

## 18. Acceptance contract

Срез считается завершённым, когда:

1. Suite запускает точный production selector, а не копию workflow.
2. Profile/commands/tools входят в baseline и fingerprints.
3. Каждый repeat изолирован и начинает с исходного case state.
4. Cases/repeats выполняются последовательно в нормативном порядке `v1alpha1`.
5. `KeepWorktree` удерживает managed worktree до oracle.
6. Oracle вызывается после completed, failed (включая node timeout), cancelled,
   abandoned и waiting при доступном workspace; cancellation самой eval-команды
   не запускает новую работу.
7. `pausing|paused` сохраняет resumable state/workspace, не запускает oracle и не
   классифицируется как reject.
8. `valid: false + exit 0` является measured invalid product.
9. Validator exit/timeout/protocol error является validation infrastructure error.
10. `completed + invalid` классифицируется как `false_accept`.
11. `non-completed + valid` классифицируется как `false_reject` без изменения
   исходного Run status.
12. Validation error не входит в знаменатель valid/false-accept rates.
13. Измеренный ноль сериализуется `0`, недоступный rate — `null`.
14. Fake `gh` сам не обращается к сети и не делит mutable state между repeats.
15. Evidence и redacted durable state записываются до safe cleanup.
16. `eval compare` показывает изменения valid/false-accept rates при совместимых
    benchmark fingerprints.
17. Все три production-flow fake smokes проходят, включая
    `review-acceptance-gate` comprehensive review.
18. Реальный model failure сохраняется как evidence, а не считается поломкой
    measurement path.
19. `make check` не требует Pi/OpenCode credentials или сети.

## 19. Contract trail при реализации

Реализация обязана обновить:

- `docs/13-evaluation-plan.md`;
- `docs/03-specification.md` для публичного suite/CLI contract;
- `docs/05-implementation-status.md`;
- `docs/12-document-map.md`;
- `docs/09-runtime-semantics.md` только если фактически меняется Run semantics
  (по дизайну не должна);
- `schemas/flow-evaluation-suite.schema.json`;
- `schemas/evaluation-validator-request.schema.json`;
- `schemas/evaluation-report.schema.json`;
- `skills/takt` references после появления стабильного authoring journey;
- changelog, `TEST_RESULTS.md` и implementation plan status.

Product correctness остаётся в Go `*_test.go`; black-box CLI contracts — в
`tests/e2e`. Новый shell test script не нужен.

## 20. Решённые неизвестные и deferred scope

Проектный шлюз: **READY for implementation planning**.

- Oracle — executable deterministic post-run validator; LLM judge deferred.
- Cases — self-contained directories; shared mutable template deferred.
- Execution — точный production selector через application boundary.
- Product failure и eval infrastructure failure разделены.
- SCM — existing fake `gh` + local bare remote; generic adapter framework deferred.
- Architecture quality — baseline equivalence + bounded known smells.
- Cost threshold — только после первого честного baseline.
- Второй HTTP/API domain — после стабилизации `mini-du`.
- `eval flow init` — после доказательства suite schema.
- Старый `eval run` — не удаляется в этом cycle; судьба решается по фактическому
  дублированию после Slice 3.

## 21. Контракт отклонений при реализации

Если код опровергает P1-границу этого дизайна — особенно семантику
`{{workspace}}`, detached `RunService`, `KeepWorktree`, durable
`ExecutionWorkspace` или состав DAG выбранного production flow — реализация
останавливается и возвращается на design review. Запись отклонения обязана назвать:

- обнаруженный факт и точный источник;
- опровергнутое решение design;
- затронутые компоненты и контракты;
- уже выполненные изменения, если они есть.

Локальное механическое отклонение, не меняющее данные, безопасность, public
contract или границы компонентов, допустимо без нового design cycle, но фиксируется
в implementation plan deviation log с причиной и проверкой.
