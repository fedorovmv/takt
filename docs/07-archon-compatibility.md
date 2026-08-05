# Профиль совместимости с Archon

Takt не заявляет бинарную или YAML-совместимость с Archon. Цель — перенести полезные процессы и сохранить знакомую модель DAG без второго runtime. В `v0.1.28-alpha` встроенный профиль `code` 0.7.0 содержит 19 процессов, соответствующих стандартному каталогу Archon, и умный роутер с no-tool policy как корневой Run и выбранный процесс как governed child Run.

## Перенесённые конструкции

| Archon | Takt |
|---|---|
| `.archon/commands/*.md` | `.takt/profiles/code/commands/*.md` |
| каталог default workflows | `profile.workflows` и селектор `code:<workflow>` |
| workflow router | `code/workflow.yaml` с проверяемым `output_format` и условными governed `workflow` |
| `command` / `prompt` / `bash` | те же типы узлов |
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
| provider/model | assistant/model |
| retry | attempts + portable hooks |

Полный список процессов и соответствий описан в `38-archon-workflow-catalog-v0.1.24.md`.

## Отличия платформенного уровня

- собственная `apiVersion` и другой реестр model/assistant;
- `subworkflow` компилируется в тот же Run; отдельный `workflow` создаёт governed child Run;
- managed worktree isolation и автоматическое создание ветки реализованы; выбранный child Run применяет собственную политику или `isolation` родительского узла;
- script nodes и семантический `output_type` пока отсутствуют;
- `one_success` fan-out пока ждёт всю группу вместо досрочного завершения;
- Web UI, сервер, БД, адаптеры сообщений и уведомления остаются proposal для нелокального режима;
- native hooks передаются адаптеру, portable hooks выполняются runtime;
- state хранится локально.

Эти различия не урезают 19 процессов: каждый процесс присутствует и запускается. Они определяют, какая часть гарантий обеспечивается ядром Takt, а какая остаётся инструкцией внешнему coding agent и окружению проекта.

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
