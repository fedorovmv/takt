# Structured Task Sources — v0.1.50-alpha

`v0.1.50-alpha` добавляет внешний ingress-контракт для задач. Источник задачи разрешается **до** Router/Dynamic Takt и преобразуется в нормализованный `Task`; scheduler/runtime и workflow DSL не получают нового типа узла.

## Public protocol

Процессный source adapter использует `takt-task-source/v1alpha1` и публичный `sdk/tasksource`:

```text
external issue / tracker / PRD / OpenSpec
        ↓
Task Source Adapter
        ↓
normalized Task + immutable source revision
        ↓
Task Router → WorkflowPlan → обычный Takt runtime
```

Конфигурация:

```yaml
task_sources:
  github:
    transport: process
    argv: [takt-github-task-source]
    env:
      GH_TOKEN: secret://GH_TOKEN
    timeout: 30s
```

Запуск:

```bash
takt task start \
  --workspace . \
  --profile code \
  --source github \
  --source-ref acme/service#42
```

Тот же контракт доступен через `takt.task.start` (`source` + `source_ref`). `goal` и `source/source_ref` взаимоисключающие способы задать вход.

## Normalized Task

Task содержит стабильные `id`, `title`, `goal`, описание, acceptance criteria, labels/references и provenance:

```text
source.adapter
source.kind
source.reference
source.revision
source.url
```

`source.revision` фиксируется в `WorkflowPlan` и не перечитывается автоматически при replan/resume. Router, Planner и Replanner получают структурированный `task_source`; старые workflow продолжают получать совместимый `GoalText`.

## Reference GitHub Issue source

`cmd/takt-github-task-source` и `reference/githubtask` являются reference implementation поверх `sdk/tasksource` и `gh issue view`. Они не импортируют `takt/internal/*`.

Поддерживаются ссылки `owner/repo#number` и GitHub issue URL. Markdown checkbox-ы issue преобразуются в acceptance criteria, labels сортируются, revision вычисляется из фактического содержимого issue.

GitHub-specific логика остаётся в reference adapter. Корпоративный tracker, OpenSpec, PRD, JSON/YAML source должны реализовывать тот же public protocol без изменений core.

## Закрытый correctness debt

В релиз также вошли накопленные исправления `v0.1.47–v0.1.49`:

- crash/resume `loop_group` продолжает с `len(loop_iterations)+1` и не дублирует side effects/history;
- exhaustion + operator retry не может увеличить bounded history сверх 64;
- добавлены regressions для `foreach` и governed child workflow внутри loop, старого state без `loop_iterations` и alias isolation;
- `.git` исключён одинаково из task-eval copy/fingerprint;
- persistence redactor fail-closed при ошибке загрузки per-run config;
- `input.schema` и unsupported schema keywords покрыты поведенческими regressions;
- schema subset получил полный contract coverage по заявленным keywords и JSON numeric equality для `uniqueItems`;
- Pi/OpenCode probes и Domain Adapter Describe имеют bounded timeout;
- все публикуемые schemas проверяются как автономный offline registry;
- MCP domain env разрешает `secret://...` так же, как process transport;
- Qwen budget exit 55 нормализуется как `timed_out`, wall-time округляется до поддерживаемых секунд;
- GitHub SCM reference tests канонизируют macOS symlink paths, имеют bounded `gh` timeout, безопасные ref/number validators и diagnostics без body;
- release manifest теперь проверяется repo-native скриптом и запрещает временные editor/build leftovers.

## Граница с Domain Adapter

Task Source отвечает на вопрос **«что за задача пришла?»**. Domain Adapter отвечает на вопрос **«какое внешнее действие выполнить внутри workflow?»**. Source resolution не становится `adapter`-узлом и не создаёт side-effect lifecycle/reconcile semantics.

