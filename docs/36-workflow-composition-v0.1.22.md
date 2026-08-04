# Композиция workflow в v0.1.22-alpha

## Цель

Добавить переиспользуемые фазы и последовательную обработку явного списка, сохранив текущую архитектуру Takt: один scheduler, один Run state и Markdown как основной пользовательский формат плана.

## Реализовано

### Subworkflow

```yaml
- id: implementation
  subworkflow:
    path: workflows/implementation.yaml
    inputs:
      plan: ${input}
    output_node: result
```

Подключённый файл является обычным `takt/v1alpha1 Workflow`. Его узлы компилируются в родительский DAG. Публичный ID контейнера сохраняется для `depends_on` и `${nodes.implementation.output}`.

Если terminal-узел один, `output_node` определяется автоматически. При нескольких terminal-узлах пользователь обязан выбрать результат явно.

### Последовательный foreach

```yaml
- id: checks
  foreach:
    as: check
    items: [lint, test]
    subworkflow:
      path: workflows/check.yaml
      inputs:
        name: ${check}
```

Итерации выполняются строго по порядку. Публичный узел возвращает output последней итерации. Поддерживаются scalar и JSON objects, включая `${check.<field>}` и индекс.

## Архитектурная семантика

- композиция выполняется после загрузки YAML и до `workflow.Validate`;
- scheduler получает только обычные DAG-узлы и внутренние no-op/result узлы;
- retry, hooks, timeout, approval, Pi/OpenCode sessions и persistence работают без отдельной ветки runtime;
- дочерние IDs имеют устойчивый namespace с `__`;
- approval внутри subworkflow приостанавливает родительский Run;
- изменение подключённого workflow или локальной команды меняет workflow fingerprint и блокирует resume.

## Ограничения

- `foreach.items` задаётся явно;
- Markdown не преобразуется в task AST;
- `subworkflow` и `foreach` внутри `loop_group` пока не поддерживаются;
- контейнерные attempts, timeout, hooks и model/assistant задаются внутри подключённого workflow;
- публичный output foreach — результат последней итерации;
- динамические источники относятся к будущим input adapters.

## Проверки

Добавлены:

- unit-тесты expansion, recursion, output selection и sequential dependencies;
- runtime-тесты approval/resume и public output;
- fingerprint-регрессия изменения подключённого workflow;
- `scripts/test-composition.sh`;
- профиль `code` 0.2.0 и authoring skill 0.4.0.
