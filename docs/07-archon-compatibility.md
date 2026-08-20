# Профиль совместимости с Archon

Takt не заявляет бинарную или YAML-совместимость с Archon. Цель — перенести
полезные процессы и сохранить знакомую модель DAG без второго runtime. В
`v0.1.57-alpha` реализован единый Archon-first Workflow language (A0) и
bounded repair/runtime slice (A1): `loop`, scalar/structured `until`, signal
evidence, `until.requires`, `until_bash`, exact session continuity,
`fresh_context`, `context: shared`, cancel metadata и durable retry history.
Hard budgets, `run inspect` и mutating merge fan-out остаются deferred.

## Перенесённые конструкции

| Archon | Takt |
|---|---|
| `.archon/commands/*.md` | `.takt/profiles/code/commands/*.md` |
| каталог default workflows | `profile.workflows` и селектор `code:<workflow>` |
| workflow router | `code/workflow.yaml` с проверяемым `output_format` и условными governed `workflow` |
| `command` / `prompt` / `bash` / `script` | AI, shell и детерминированные script-узлы |
| DAG `nodes`, `depends_on`, `when` | DAG Takt с JSON-путями в output |
| параллельные независимые узлы | параллельные scheduler-волны |
| adaptive review fan-out | структурированный classifier + `workflow.fan_out` с отдельным governed child Run на перспективу |
| `loop` / human-in-the-loop | `loop_group` + сохраняемый `approval` в каждой итерации |
| compile-time fan-out по списку | `foreach.parallel` с детерминированной агрегацией |
| runtime fan-out | `workflow.fan_out` из структурированного output, `max_parallel`, join и resume |
| reusable include | compile-time `subworkflow` |
| governed workflow / child sub-run | отдельный узел `workflow` с parent/child lifecycle |
| cancellation tree | `takt cancel` и durable marker |
| structured output | `output_format`, проверяемый runtime |
| semantic artifacts | `output_type`, MIME, SHA-256, producer metadata и `takt artifacts` |
| provider/model | provider/model, с Config assistant binding внутри runtime |
| retry | attempts + portable hooks |
| platform control tools | локальный stdio MCP control plane поверх файлового Run store |

Историческая таблица миграции процессов сохранена в
[`archive/releases/38-archon-workflow-catalog-v0.1.24.md`](archive/releases/38-archon-workflow-catalog-v0.1.24.md).

## Отличия платформенного уровня

- собственная `apiVersion` и другой реестр model/assistant;
- `subworkflow` компилируется в тот же Run; отдельный `workflow` создаёт governed child Run;
- managed worktree isolation и автоматическое создание ветки реализованы; выбранный child Run применяет собственную политику или `isolation` родительского узла;
- `one_success`/`all_success` fan-out может досрочно отменять ненужных siblings;
  такая отмена получает `cancel_reason: fanout_result_decided`;
- Web UI, HTTP server, БД, адаптеры сообщений и уведомления остаются proposal для нелокального режима; локальное управление доступно через `takt mcp`;
- native hooks передаются адаптеру, portable hooks выполняются runtime;
- state хранится локально.

Эти различия не урезают 19 процессов: каждый процесс присутствует и запускается. Они определяют, какая часть гарантий обеспечивается ядром Takt, а какая остаётся инструкцией внешнему coding agent и окружению проекта.

## A0/A1 release boundary

A0 мигрирует актуальные Workflow и Markdown commands на target root (`name`,
`description`, `provider`, `model`, `nodes`) и единый `$...` reference grammar.
Legacy `${...}`, `$USER_MESSAGE`, root `apiVersion/kind/metadata/defaults` и
frontmatter `assistant` отклоняются fail-closed; importer, dual parser и второй
renderer не используются. `output_format` и `takt-schema-subset/v1` остаются
без изменения семантики.

A1 переиспользует один DAG scheduler: loop iterations durable, signal matcher
сохраняет `matched_signal`/`signal_diagnostic`, `until.requires` проверяет
дополнительные evidence, `until_bash` сохраняет predicate evidence, а
`fresh_context`/`context: shared` управляют exact Session ID resume. Approval
внутри loop продолжает активную iteration после `answer`; timeout/cancel имеют
приоритет над derived loop/output errors.

## Запуск

```bash
takt init code
takt workflow list code
takt workflow describe code:piv-loop
takt run code --input request.md --workspace . --json
takt run code:smart-pr-review --input request.md --workspace . --json
takt children <root-run-id> --workspace . --json
takt cancel <root-run-id> --workspace . --json
```

Запуск `code` использует роутер и создаёт отдельный child Run выбранного процесса; явный селектор обходит классификацию и запускает этот процесс как корневой Run.


## Углубление основных процессов в v0.1.32

Шесть основных workflow больше не являются только фазовыми каркасами. Они используют строгие JSON-входы, специализированные команды, обязательные checkpoint artifacts, предметные error codes, Git decision trees и validation recovery. Локальный Git/GitHub fixture проверяет успешный issue flow и восстановление после failure.

Разрыв с Archon сохраняется в объёме готовых project-specific эвристик, Web/daemon эксплуатации и native provider integrations. По проверяемой структуре основных локальных процессов Takt теперь существенно ближе, не копируя серверную архитектуру Archon.
