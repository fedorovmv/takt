# Сквозное создание и использование evaluation

Этот документ описывает актуальный production flow evaluation в Takt
`v0.1.64-alpha`: как подготовить corpus, написать evaluation workflow и
детерминированный validator, запустить оценку и исследовать сохранённый Run.

Для нового evaluation не используйте `suite.yaml` версии
`takt-flow-evaluation/v1alpha1`: это deprecated compatibility path с
фиксированным runner. Новый evaluation является обычным `takt/v1alpha1`
workflow.

## Канонический authoring path

Команда

```bash
takt eval flow init <workflow-selector> --output <directory>
```

по умолчанию создаёт authored evaluation scaffold: обычный
`workflows/evaluate.yaml`, cases и deterministic tools. Это рекомендуемый путь
для новых наборов. Старый fixed-stage scaffold создаётся только по явному

```bash
takt eval flow init <workflow-selector> --output <directory> --legacy
```

и предназначен для чтения и запуска существующих compatibility suites. Legacy
режим не является вторым runtime и не расширяет новый authoring contract.
Scaffold намеренно не создаёт `config.yaml`: создайте или скопируйте Config с
assistant/model bindings перед первым запуском.

Route DSL/micro DSL, их examples, validators, benchmarks и evaluation fixtures
остаются публичными OSS surfaces. Authored workflow должен выражать предметную
логику обычными nodes и deterministic validation; переход на authored path не
отключает и не удаляет эти поверхности.

## 1. Модель выполнения

Команда `takt eval flow` выполняет две функции:

1. проверяет и материализует заданные cases и repeats в строгий JSON-вход
   `takt-evaluation-input/v1alpha1`;
2. один раз запускает authored evaluation workflow как обычный root Run.

Дальше работает общий runtime:

```text
cases directory + --repeat
          |
          v
takt-evaluation-input/v1alpha1
          |
          v
evaluation root Run
  `-- matrix branch для каждого case + repeat
        |-- произвольные authored nodes
        |-- optional governed child Run
        |-- deterministic validation
        |-- evidence artifact
        `-- primary/advisory assessment
```

`matrix` — общий structural node, а не встроенный evaluation engine. Имена и
число узлов внутри branch определяет автор workflow. Последовательность
`candidate -> validate -> evidence -> assess` является полезным шаблоном, но не
зашита в runtime. Только явно объявленный `workflow` action создаёт child Run.

Technical status и качество разделены:

- `valid:false` — корректно измеренный результат и сам по себе не ломает
  evaluation Run;
- malformed validation envelope, отсутствующее evidence или ошибка persistence
  ломают измерительный Run;
- gate failure меняет exit code команды после durable reload, но не переписывает
  `Run.status=completed`.

## 2. Рекомендуемая структура

Минимальный проект evaluation выглядит так:

```text
evaluation/
|-- config.yaml
|-- workflows/
|   `-- evaluate.yaml
|-- validator/
|   `-- main.go
|-- tools/
|   |-- validate/
|   `-- collect-evidence/
`-- cases/
    |-- case-a/
    |   |-- input.md
    |   |-- expected.yaml
    |   `-- workspace/
    `-- case-b/
        |-- input.md
        |-- expected.yaml
        |-- workspace/
        `-- scm/                 # optional
```

Полный рабочий пример находится в
`examples/flow-evaluation/mini-du/`. Его shared evaluation DAG —
`examples/flow-evaluation/mini-du/workflows/evaluate.yaml`.

## 3. Контракт case

Один case — self-contained исходное состояние одного эксперимента:

```text
cases/<case-id>/
|-- input.md          # обязательно
|-- expected.yaml     # обязательно
|-- workspace/        # обязательно и непусто
`-- scm/              # необязательно
```

Case ID равен имени каталога и должен соответствовать
`[A-Za-z0-9][A-Za-z0-9._-]*`. Имена сравниваются также без учёта регистра:
`Basic` и `basic` в одном corpus недопустимы.

Corpus проверяется до первого model call:

- `input.md` и `expected.yaml` должны быть обычными файлами;
- `workspace/` должен существовать и быть непустым;
- symlink и другие non-regular entries запрещены;
- вложенный `.git` запрещён;
- `.takt/eval`, `.takt/runs`, `.takt/worktrees` и `.takt/evals` зарезервированы;
- содержимое `input.md`, `expected.yaml`, `workspace/` и optional `scm/` входит
  в fingerprint case.

### 3.1. `input.md`

`input.md` содержит только задание целевому workflow. Оно должно описывать
публичный контракт задачи достаточно точно, чтобы агент мог выполнить её без
знания hidden oracle. Не добавляйте сюда секретные проверки validator только
ради повышения score.

При materialization файл копируется в изолированный control workspace. Способ
передачи целевому workflow определяется его profile input contract; готовые
профили могут передать JSON byte-for-byte либо обернуть Markdown содержимым и
путём к authoritative source.

### 3.2. `workspace/`

`workspace/` — полное начальное дерево проекта именно этого case. Поместите в
него исходники, manifest/build-файлы и локальные тесты, которые действительно
должен видеть агент. Не используйте общий mutable workspace с setup script:
зафиксированное дерево проще проверить и воспроизвести.

Takt копирует дерево, создаёт локальный Git baseline и сохраняет отдельную
baseline-копию для validator. Исходный corpus не должен изменяться во время
Run.

### 3.3. `expected.yaml`

Минимальная форма:

```yaml
oracle: {}
```

Реальный validator обычно задаёт собственный bounded contract:

```yaml
oracle:
  allowed_paths:
    - cmd/tool/**
    - internal/**
    - go.mod
  required_artifacts:
    - implementation.md
    - validation.md
  scenarios:
    - empty_input
    - nested_directory
    - invalid_option
```

Takt строго принимает только top-level `oracle` и optional legacy `takt`, а
также требует, чтобы `oracle` присутствовал и отличался от `null`. Содержимое
`oracle` Takt не интерпретирует: его строгую схему, допустимые значения и
семантику определяет конкретный deterministic validator. Полный путь к файлу
доступен как `$MATRIX.item.expected_path`.

Не используйте legacy `takt.approval_answer` в новом ordinary evaluation.
Одинаковый автоматический ответ задаётся через `takt eval flow --answer`, а
case-specific ответы требуют отдельных запусков с `--case ... --answer ...`
либо явно authored preparation/input path; materializer не извлекает их из
`expected.yaml`.

### 3.4. Optional `scm/`

Без `scm/` Takt создаёт локальный Git repository с baseline branch `main`.
SCM fixture нужен, когда production workflow обращается к repository или pull
request через `gh`.

Repository fixture:

```text
scm/
`-- repository.yaml
```

```yaml
repository: example/project
base_branch: main
head_branch: eval/case-a
```

Pull-request fixture дополнительно требует:

```text
scm/
|-- repository.yaml
|-- pull-request.yaml
`-- head.patch
```

```yaml
# pull-request.yaml
number: 17
title: Fix accounting
base: main
head: eval/case-a
state: OPEN                 # OPEN | CLOSED | MERGED
ci_status: passed           # passed | failed | pending
fixes_permitted: true
```

`base`/`head` обязаны совпадать с `repository.yaml`. `head.patch` должен быть
непустым текстовым Git patch без binary payload и escaping paths. Во всех
выбранных cases одного запуска тип SCM fixture должен совпадать: без fixture,
repository или pull request.

Fixture подменяет `gh` внутри изолированной evaluation workspace и сохраняет
детерминированное состояние вызовов. Это тестовый input, а не доказательство
эффекта в реальном GitHub.

## 4. Вход evaluation workflow

Launcher формирует один JSON-объект. Его основная часть выглядит так:

```json
{
  "protocol_version": "takt-evaluation-input/v1alpha1",
  "type": "evaluation_input",
  "cases": [
    {
      "case_id": "case-a",
      "repeat": 1,
      "input": "...prepared target input...",
      "input_path": "/absolute/cases/case-a/input.md",
      "expected_path": "/absolute/cases/case-a/expected.yaml",
      "baseline_path": "/absolute/eval/workspaces/case-a/repeat-001/baseline",
      "repository": ".takt/evals/evaluate/.../control",
      "workflow_path": "/absolute/eval/.../target-workflow.yaml",
      "case_fingerprint": "<sha256>",
      "workflow_fingerprint": "<sha256>",
      "prepared_fingerprint": "<sha256>"
    }
  ],
  "gates": {},
  "identity": {
    "fingerprint": "<sha256>",
    "workflow_fingerprint": "<sha256>",
    "config_fingerprint": "<sha256>",
    "dataset_fingerprint": "<sha256>",
    "target": "code:feature-development",
    "model_preset": "candidate",
    "models": {
      "implementation": "provider/model-id"
    }
  }
}
```

Обычно этот JSON вручную не создаётся. Evaluation workflow объявляет
`input.format: json`, читает массив как `$INPUTS.cases`, а текущий элемент
matrix branch — как `$MATRIX.item`.

## 5. Evaluation workflow

Ниже полный каркас ordinary evaluation. Он запускает целевой workflow в
отдельном worktree, проверяет результат, собирает evidence и создаёт primary
assessment:

```yaml
name: project-evaluation
description: Evaluate the target workflow for every materialized case.

input:
  format: json
  schema:
    type: object
    additionalProperties: false
    properties:
      protocol_version: {type: string}
      type: {type: string}
      cases:
        type: array
        items:
          type: object
          additionalProperties: false
          properties:
            case_id: {type: string}
            repeat: {type: integer}
            input: {type: string}
            input_path: {type: string}
            expected_path: {type: string}
            baseline_path: {type: string}
            repository: {type: string}
            workflow_path: {type: string}
            case_fingerprint: {type: string}
            workflow_fingerprint: {type: string}
            prepared_fingerprint: {type: string}
          required: [case_id, repeat, input, input_path, expected_path, baseline_path, repository, workflow_path, case_fingerprint, workflow_fingerprint, prepared_fingerprint]
      gates: {type: object}
      identity: {type: object}
    required: [protocol_version, type, cases, gates, identity]

nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      nodes:
        - id: candidate
          workflow:
            path: $MATRIX.item.workflow_path
            repository: $MATRIX.item.repository
            input: $MATRIX.item.input
            isolation: worktree
            keep_worktree: true
          allow_failure: true

        - id: validate
          depends_on: [candidate]
          trigger_rule: all_done
          script:
            runtime: go
            path: ../tools/validate/main.go
            dependencies: [../validator/main.go]
            stdin: |
              {
                "case_id": "$MATRIX.item.case_id",
                "repeat": $MATRIX.item.repeat,
                "workspace": "$candidate.child_execution_workspace",
                "baseline_workspace": "$MATRIX.item.baseline_path",
                "expected_path": "$MATRIX.item.expected_path",
                "run_id": "$candidate.child_run_id",
                "run_status": "$candidate.status"
              }
          timeout: 2m
          allow_failure: true

        - id: evidence
          depends_on: [candidate, validate]
          trigger_rule: all_done
          script:
            runtime: go
            path: ../tools/collect-evidence/main.go
            args:
              - --workspace
              - $candidate.child_execution_workspace
              - --base
              - $candidate.child_base_commit
              - --output
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

Почему в шаблоне стоят `allow_failure: true` и `trigger_rule: all_done`:
неуспешный candidate — это измеряемый исход, поэтому validator и evidence всё
равно должны получить возможность завершиться. `allow_failure` разрешает
только обычный ненулевой exit code; timeout, cancellation, ошибка старта и
protocol error не превращаются в success.

`matrix` выполняет branches последовательно и сохраняет completed branches для
resume. Вложенные `matrix` и `loop_group` внутри него запрещены. Максимум —
1024 уникальных canonical JSON items.

Каркас можно менять: добавлять preparation, несколько candidate workflows,
deterministic checks или advisory agent judge. Если branch создаёт primary
assessment, для каждого item должна существовать ровно одна такая запись.

## 6. Validator

Validator должен независимо проверять итоговый продукт, а не доверять summary,
verdict или exit code агента. Предпочтительный порядок:

1. строго декодировать request из `script.stdin`;
2. проверить абсолютные paths и containment;
3. прочитать и строго проверить собственную схему `expected.yaml.oracle`;
4. сравнить baseline и candidate, проверить scope изменений;
5. выполнить предметные проверки продукта;
6. напечатать ровно один `takt-validation/v1alpha1` JSON в stdout;
7. писать operational diagnostics только в stderr.

Минимальный результат:

```json
{
  "protocol_version": "takt-validation/v1alpha1",
  "type": "validation_result",
  "valid": true
}
```

Расширенный результат:

```json
{
  "protocol_version": "takt-validation/v1alpha1",
  "type": "validation_result",
  "valid": false,
  "score": 65,
  "checks": {
    "tests": {"passed": true, "score": 100, "weight": 2},
    "scope": {"passed": false, "score": 30, "weight": 1,
              "message": "unexpected generated file"}
  },
  "diagnostics": [
    {
      "code": "OUT_OF_SCOPE_CHANGE",
      "severity": "error",
      "path": "generated.txt",
      "message": "file is outside allowed_paths"
    }
  ],
  "metadata": {"validator": "project", "version": "1"}
}
```

Обязательны `protocol_version`, `type` и boolean `valid`. `score` и check score
должны быть в диапазоне `0..100`; diagnostic severity принимает только
`info|warning|error`. Не выводите `null` вместо отсутствующего optional поля и
не добавляйте неизвестные top-level поля.

Для primary assessment источник `result_from` обязан быть детерминированным
`bash`, `script`, `adapter` или детерминированным child workflow. Agent judge
разрешён только для `role: advisory`.

Новый authored DAG не навязывает формат validator request. Можно использовать
собственный строгий JSON. Если нужна совместимость с готовыми flow validators,
используйте `schemas/evaluation-validator-request.schema.json`; результат в
любом случае обязан соответствовать `schemas/validation-result.schema.json`.

## 7. Evidence и assessment

Primary assessment требует хотя бы один immutable artifact. Evidence должно
позволять независимо восстановить вывод validator: обычно это diff, Git bundle,
manifest и bounded snapshot результата. Не включайте секреты; textual artifacts
проходят redaction, а binary artifact с известным секретом отклоняется
fail-closed.

Узел `assessment` сам ничего не проверяет и не вызывает модель. Он:

- строго декодирует `result_from`;
- загружает terminal target Run;
- фиксирует его `result_revision` и status;
- проверяет checksum evidence;
- сохраняет immutable `takt-assessment/v1alpha1` artifact.

Outcome вычисляется из status target и `valid`:

| Target status | Validation | Outcome |
|---|---|---|
| `completed` | `valid:true` | `true_accept` |
| `completed` | `valid:false` | `false_accept` |
| не `completed` | `valid:false` | `true_reject` |
| не `completed` | `valid:true` | `false_reject` |

После operator retry target получает новую terminal revision; старая assessment
становится stale, но не переписывается.

## 8. Config, модели и таймауты

Evaluation использует обычный Config. Все model aliases, необходимые целевому
workflow, должны присутствовать в выбранном preset:

```yaml
apiVersion: takt/v1alpha1
kind: Config

model_preset: candidate
model_presets:
  candidate:
    implementation: provider/model-id
    review: provider/model-id
    routing: provider/model-id

assistants:
  pi:
    type: pi
    binary: pi
    project_trust: approve
    settings:
      httpIdleTimeoutMs: 900000
    max_output_bytes: 10485760
```

`--model-preset` выбирает preset для конкретного запуска, а повторяемый
`--model alias=provider/model-id` переопределяет отдельный alias.

Не смешивайте разные таймауты:

- Pi `settings.httpIdleTimeoutMs` — отсутствие HTTP headers/body, миллисекунды;
- Pi `settings.retry.provider.timeoutMs` — deadline одного provider request;
- node `timeout` — общий deadline попытки узла;
- node `idle_timeout` — отсутствие нормализованной assistant activity;
- `--assistant-idle-timeout` — только evaluation fallback для assistant nodes
  без собственного `idle_timeout`.

Каждый лимит действует независимо. Для медленной модели provider timeout Pi не
должен обрывать запрос раньше выбранного evaluation idle limit. Evaluation
копирует native Pi `settings` в изолированный `.pi/settings.json` и отключает
Pi/SDK retries, чтобы durable provider retries оставались у Takt.

## 9. Preflight

До live запуска проверьте evaluation DAG:

```bash
takt validate evaluation/workflows/evaluate.yaml \
  --config evaluation/config.yaml \
  --workspace . \
  --json
```

Если используется исходный checkout Takt:

```bash
go run ./cmd/takt validate evaluation/workflows/evaluate.yaml \
  --config evaluation/config.yaml \
  --workspace . \
  --json
```

Отдельно проверьте validator unit/contract tests без модели. Тесты должны как
минимум покрывать `valid:true`, `valid:false`, malformed request, нарушение
scope, недоступный предметный oracle и строгий stdout/stderr contract.

`takt validate` проверяет сам workflow и Config. Case materialization,
динамические target paths, SCM fixture и полная target model reference проверка
выполняются launcher preflight непосредственно перед Run.

## 10. Запуск

`--target` задаёт workflow/profile, исполняемый для каждого case. Built-in
profile Takt устанавливает в подготовленную workspace автоматически. Custom
profile должен уже находиться в `workspace/.takt/profiles/<name>/` каждого
case. Если target — относительный путь к YAML, этот файл должен существовать
внутри каждого case workspace по тому же пути; target вне подготовленного
repository отклоняется containment preflight.

Один case для smoke:

```bash
takt eval flow evaluation/workflows/evaluate.yaml \
  --target code:feature-development \
  --config evaluation/config.yaml \
  --cases evaluation/cases \
  --case case-a \
  --repeat 1 \
  --gate validation_error_rate.max=0 \
  --model-preset candidate \
  --assistant-idle-timeout 15m \
  --trace
```

Полный corpus со статистически полезными repeats:

```bash
takt eval flow evaluation/workflows/evaluate.yaml \
  --target code:feature-development \
  --config evaluation/config.yaml \
  --cases evaluation/cases \
  --repeat 3 \
  --gate valid_rate.min=0.8 \
  --gate validation_error_rate.max=0 \
  --model-preset candidate \
  --assistant-idle-timeout 15m \
  --trace
```

`--trace` сообщает полный Run ID в stderr сразу после принятия Run. Финальный
JSON stdout с тем же `run_id` появляется после завершения команды. Потоки можно
сохранить раздельно:

```bash
takt eval flow ... --trace > evaluation-result.json 2> evaluation-trace.log
```

Поддерживаемые rate gates: `valid_rate`, `false_accept_rate`,
`false_reject_rate`, `flow_completion_rate`, `validation_error_rate`. Каждый
threshold находится в `0..1` и задаёт ровно одно из `min|max`.

Без `--output` launcher создаёт preparation root вида
`.takt/evals/<evaluation-workflow>/<timestamp>/` внутри invocation workspace.
Флаг `--output` меняет этот путь, но не создаёт отдельный canonical report:
state, events, artifacts и assessments остаются данными ordinary Run в общем
Store. Output path обязан находиться внутри invocation workspace.

Для bundled mini-du:

```bash
cp examples/flow-evaluation/mini-du/config.pi.example.yaml \
  examples/flow-evaluation/mini-du/config.yaml

EVAL_PRESET=qwen36 EVAL_IDLE_TIMEOUT=15m make eval-feature-smoke
EVAL_PRESET=qwen36 EVAL_IDLE_TIMEOUT=15m make eval-feature
```

Замените `qwen36` на собственный preset, например `qwen38`, если он объявлен в
локальном `config.yaml`.

Live evaluation требует установленного assistant, credentials, доступной
модели и предметного oracle. Он не входит в release checks.

## 11. Наблюдение и диагностика

Новый evaluation — обычный Run, поэтому для него используются общие команды:

```bash
takt run status <run-id> --json=false
takt run stats <run-id> --check-gates --json=false
takt run inspect <run-id> --json=false
takt run inspect <run-id> --case case-a --repeat 1 --json=false
takt run inspect <run-id> --node validate --json=false
takt run assessment <run-id> --role primary --json=false
```

Команды предполагают тот же invocation workspace, где был создан Run. Из
другого каталога передайте `--workspace <path>`.

`status` показывает technical status, активный node, matrix progress, attempts,
usage и число assessments. `stats` агрегирует outcomes и rates. `inspect`
показывает causal failure, case/repeat, target Run, evidence и узлы.
`assessment` возвращает immutable envelopes; `--include-stale` добавляет записи,
которые больше не соответствуют текущей result revision target.

Совместимые aliases также принимают Run ID:

```bash
takt eval status <run-id> --json=false
takt eval stats <run-id> --json=false
takt eval inspect <run-id> --case case-a --repeat 1 --json=false
```

Они делегируют тем же application queries. Directory argument нужен только для
read-only просмотра legacy `progress.json`/`report.json`.

Полезная классификация проблем:

- `valid:false` и validator diagnostic — дефект или ограничение продукта;
- `validation_error_rate > 0` — validator/evidence/assessment не дали пригодное
  измерение;
- `timed_out` — сначала определите, какой именно Pi/Takt timeout сработал;
- `configuration` — исправьте provider/model/assistant config, retry той же
  конфигурации бессмыслен;
- `protocol` — stdout не соответствует строгому envelope;
- `evidence_missing` — artifact отсутствует или его checksum изменился.

## 12. Повторный запуск и сравнение

Для повторной оценки запускайте новый root evaluation Run с тем же corpus,
workflow, Config и repeat. Fingerprints в input identity позволяют доказать,
что сравнивались одинаковые определения. Не редактируйте сохранённые assessment
artifacts и не используйте их как mutable report.

Для точечной перепроверки создайте новый запуск с `--case <id> --repeat 1`.
Отдельной команды «перезапустить только validator старой matrix branch» сейчас
нет: это создало бы новую assessment target revision или потребовало бы явно
authored assessor Run. Старые assessment artifacts остаются immutable evidence.

`takt eval compare` относится к legacy/benchmark directory reports и пока не
сравнивает два ordinary evaluation Run ID. Для нового пути сопоставляйте
`run stats`, assessments и identity fingerprints либо автоматизируйте такое
сравнение отдельным consumer общего Run API.

Для стохастических моделей один case с одним repeat не доказывает стабильность.
Практический baseline — несколько разных cases и не меньше трёх repeats на
каждый case.

## 13. Checklist автора

Перед первым live запуском:

- [ ] evaluation описан обычным `takt/v1alpha1` workflow;
- [ ] каждый case содержит `input.md`, `expected.yaml` и непустой `workspace/`;
- [ ] case не содержит `.git`, symlink, secret или runtime state;
- [ ] `oracle` имеет строгую validator-owned схему;
- [ ] validator проверяет продукт независимо и пишет один JSON только в stdout;
- [ ] primary assessment получает deterministic result и immutable evidence;
- [ ] Config содержит все aliases выбранного target workflow;
- [ ] Pi/provider и Takt idle/deadline limits согласованы;
- [ ] `takt validate` и validator tests проходят;
- [ ] smoke запускает один case до полного corpus;
- [ ] Run ID сохраняется и исследуется через `run status|stats|inspect|assessment`;
- [ ] результаты разных fingerprints или validator generations не смешиваются.

## 14. Источники истины

- внешний Workflow/Config контракт: `docs/03-specification.md`;
- durable semantics matrix и assessment: `docs/09-runtime-semantics.md`;
- стратегия измерений и метрики: `docs/13-evaluation-plan.md`;
- schemas: [evaluation input](../schemas/evaluation-input.schema.json),
  [validation result](../schemas/validation-result.schema.json),
  [assessment](../schemas/assessment.schema.json);
- рабочий corpus: [mini-du](../examples/flow-evaluation/mini-du/);
- authoring reference для coding agents:
  [evaluation](../skills/takt/references/evaluation.md).
