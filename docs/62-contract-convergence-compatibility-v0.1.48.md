# Contract Convergence & Compatibility Matrix — v0.1.48-alpha

`v0.1.48-alpha` продолжает стабилизацию `v0.2` без нового scheduler/runtime. Срез закрывает два открытых решения после `v0.1.47`: фиксирует границу structured JSON contract и превращает compatibility adapters/hosts из разрозненных примечаний в проверяемую матрицу.

## `takt-schema-subset/v1`

Workflow `input.schema` и node `output_format` теперь явно используют один контракт `takt-schema-subset/v1` и один Go validator (`internal/schemasubset`). Это **не полный JSON Schema**.

Поддерживаются типы:

```text
object
array
string
number
integer
boolean
```

Поддерживаемые keywords:

```text
type
description
properties
required
enum                 # только string
items
minItems / maxItems
uniqueItems
minLength / maxLength
pattern              # Go RE2-compatible regexp
minimum / maximum
minProperties / maxProperties
additionalProperties # только boolean
```

Не поддерживаются и не принимаются как обещанная семантика: `$ref`, `$defs`, `oneOf`, `anyOf`, `allOf`, `const`, `default`, `format`, conditional schemas, schema-valued `additionalProperties` и другие keywords полного JSON Schema.

Решение для `v0.2`: **не расширять subset без production evidence**. Если потребуется новая семантика, она должна появиться как совместимое расширение/новая версия schema subset, а не как скрытая смена `v1`.

Машиночитаемая декларация:

```bash
takt compatibility schema
```

Schema определения: `schemas/schema-subset-v1.schema.json`.

## Compatibility matrix

Новые команды:

```bash
takt compatibility matrix
takt compatibility fields
takt compatibility schema
takt compatibility check --config .takt/config.yaml
takt compatibility check --config .takt/config.yaml --live
takt compatibility check --config .takt/config.yaml --strict
```

### `matrix`

Разделяет три независимых контракта:

1. **assistant session adapter** — как Takt запускает Pi/OpenCode/process wrapper;
2. **coding-agent host integration** — как `/takt` и tool/completion guards встроены в основную пользовательскую сессию;
3. **domain adapter** — SCM/Tracker/CI transport и capabilities.

Это важно: успешный Pi session-adapter contract test не делает Pi host-control `strict`.

Текущий bundled status:

| Контур | Статус |
|---|---|
| `takt-assistant/v1alpha2` process | supported-alpha, public conformance kit |
| `takt-assistant/v1alpha1` process | deprecated, read-compatible |
| Pi session adapter | supported-alpha, synthetic contract fixture; live pin required |
| OpenCode session adapter | supported-alpha, synthetic contract fixture; live pin required |
| Pi host extension | guarded, contract target `0.73.1`, strict=false до live smoke |
| OpenCode host extension | guarded, V2 beta, strict=false до live smoke |
| domain process/MCP adapters | supported-alpha; live `Describe` available through check/doctor |
| public `agent` MCP surface | stable-candidate |
| host/worker/operator MCP surfaces | supported-alpha |

### `check`

`compatibility check` проверяет конкретный Config:

- разрешает configured assistant через тот же `assistant.Factory`;
- показывает фактические declared capabilities;
- предупреждает о legacy raw process mode и deprecated `v1alpha1` protocol;
- `--live` выполняет version probe Pi/OpenCode и `Describe()` domain adapters;
- domain adapter live-check сравнивает configured operations/reconcile с declaration;
- `--strict` превращает warning/error в non-zero, чтобы использовать check как CI/preflight gate.

Version probe **не является live conformance** и не повышает host enforcement автоматически.

## Field-by-field v1beta1 audit

`takt compatibility fields` выдаёт машиночитаемую таблицу полей для stable-candidate authoring/config contracts. Contract-test фиксирует точный набор JSON-полей: новое публичное поле нельзя добавить незаметно, не обновив audit.

Основные решения:

- `Workflow`, базовый `Node`, `BlockPackage`, `PolicySpec`, governed `WorkflowRunSpec` — `stable-candidate/keep`;
- `apiVersion` у Workflow/Config/BlockPackage — `migrate-value`: текущий `takt/v1alpha1` продолжает читаться в `v0.2`, будущий документ может получить `takt/v1beta1`;
- `OutputFormat` — `stable-candidate/keep` как `takt-schema-subset/v1`;
- `Node.executor`, `Node.native_hooks`, `Node.tool_approval` — `supported-alpha/defer` до доказательства внешних seams;
- поле `Config.assistants` остаётся, но вложенный `AssistantSpec` пока supported-alpha;
- поле `Config.adapters` остаётся, но transport/config `DomainAdapterSpec` пока supported-alpha.

Schema отчёта: `schemas/v1beta1-field-matrix.schema.json`.

## Что это меняет в плане v0.2

Закрыты стабилизационные пункты:

- решение по границе `output_format`;
- формальная adapter/host compatibility matrix;
- первый машиночитаемый field-by-field audit stable-candidate API.

До выпуска `v1beta1` остаются внешние evidence-gates и финальная migration policy на основании фактического использования. `v0.1.48` не меняет `apiVersion` существующих Workflow/Config/BlockPackage.
