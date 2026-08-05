# Профиль совместимости с Archon

Takt не заявляет бинарную или YAML-совместимость с Archon. Цель — перенести полезные процессы и сохранить знакомую модель DAG без второго runtime. В `v0.1.24-alpha` встроенный профиль `code` содержит 19 процессов, соответствующих стандартному каталогу Archon, и умный роутер внутри обычного Run.

## Перенесённые конструкции

| Archon | Takt |
|---|---|
| `.archon/commands/*.md` | `.takt/profiles/code/commands/*.md` |
| каталог default workflows | `profile.workflows` и селектор `code:<workflow>` |
| workflow router | `code/workflow.yaml` с проверяемым `output_format` и условными `subworkflow` |
| `command` / `prompt` / `bash` | те же типы узлов |
| DAG `nodes`, `depends_on`, `when` | DAG Takt с JSON-путями в output |
| параллельные независимые узлы | параллельные scheduler-волны |
| adaptive review fan-out | структурированный classifier + условные review-ветви + `one_success` |
| `loop` / human-in-the-loop | `loop_group` + сохраняемый `approval` в каждой итерации |
| fan-out по списку | `foreach.parallel` с детерминированной агрегацией |
| reusable include | compile-time `subworkflow` |
| structured output | `output_format`, проверяемый runtime |
| provider/model | assistant/model |
| retry | attempts + portable hooks |

Полный список процессов и соответствий описан в `38-archon-workflow-catalog-v0.1.24.md`.

## Отличия платформенного уровня

- собственная `apiVersion` и другой реестр model/assistant;
- `subworkflow` компилируется в тот же Run, а не создаёт управляемый дочерний Run;
- worktree isolation и автоматическое управление ветками не являются функцией runtime; workflow вызывают `git`/`gh` через агента;
- отсутствуют per-node `allowed_tools`, `denied_tools`, skills, MCP-конфигурация и sandbox policy;
- отсутствуют script nodes, `output_type`, `cancel`, Web UI, сервер, БД, адаптеры сообщений и push-уведомления;
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
```

Запуск `code` использует роутер; явный селектор обходит классификацию и запускает выбранный процесс.
