# Architecture Contracts v0.1.57

`v0.1.57-alpha` закрепляет три архитектурных правила, полезных для долгой
эволюции runtime: дисциплину языка workflow, декларативную регистрацию
расширений и schema-first описание canonical operations. В том же release
boundary реализован A0/A1 Archon-first language/runtime slice; это миграция
Workflow authoring contract, а не второй runtime.

Идеи осознанно адаптированы из практик Archon (`coleam00/Archon`), который является идеологическим родителем Takt. Takt перенимает ограничения, уменьшающие архитектурную энтропию, но сохраняет собственные принципы: один production composition root, отсутствие глобальных mutable registries и небольшой provider-neutral stable core.

## 1. Конституция языка workflow

Нормативная формула:

> **YAML координирует. Код вычисляет. Агент принимает решения.**

Workflow YAML описывает только то, что runtime должен видеть для управления выполнением: структуру DAG, зависимости, gates, retries, approvals, sessions, artifacts и композицию. Вычисления и преобразования данных остаются внутри `bash`/`script`/`command`; решения, требующие модельного суждения, остаются внутри `prompt`/assistant node.

Каждое новое поле или выражение YAML проходит три вопроса:

1. Должен ли runtime видеть это значение, чтобы управлять Run?
2. Это декларативные данные с фиксированной семантикой, а не вычисление?
3. Можно ли выразить задачу существующим script/command/prompt node и текущей связкой узлов?

Если третий ответ положительный, новое поле требует отдельного доказательства governance-ценности: наблюдаемости, resumability, auditability или безопасного управления жизненным циклом.

### `when` не становится самодельным языком программирования

В `takt/v1alpha1` `when` намеренно ограничен:

- `==`;
- `!=`;
- `&&`;
- `||`, при этом `&&` имеет больший приоритет;
- слева — только `nodes.<id>.<path>` или `inputs.input|inputs.message`;
- справа — литерал.

Скобки, функции, арифметика, regex и операторы порядка не добавляются по одному. Более сложное решение вычисляется отдельным script/command/prompt node и публикуется как структурированный output, после чего `when` лишь разрешает или блокирует следующую ветвь.

Если практическая потребность когда-либо докажет необходимость полноценного expression language, Takt принимает зрелый специфицированный язык целиком отдельным versioned contract change, а не выращивает собственный parser оператор за оператором.

`internal/whenexpr` является единственной реализацией этой небольшой семантики. `internal/workflow` проверяет выражение до Run, а `internal/runtime` использует тот же пакет при выполнении.

### A0/A1 target language boundary

Workflow authoring использует target root `name`/`description`/`provider`/`model`/
`nodes`, node `provider`/`context` и единый `$...` reference grammar. Legacy
`apiVersion`/`kind`/`metadata`/`defaults`, frontmatter `assistant` и `${...}` не
имеют compatibility path и отклоняются до Run. `output_format` остаётся тем же
проверяемым `takt-schema-subset/v1` contract.

A1 loop semantics переиспользует общий scheduler и durable state: scalar
`loop`, `until.signal`, `until.requires`, `until_bash`, immutable iteration
evidence, `fresh_context`, `context: shared`, approval continuation и exact
Session ID resume. Hard token/tool budgets требуют отдельного live capability
proof и не входят в этот contract.

### Структура workflow

Load-time композиция (`subworkflow`, обычный `foreach`) должна раскрываться в обычный DAG до исполнения. Runtime-resolved работа оформляется governed child Run с собственным state/events/artifacts, а не динамической мутацией структуры родительского DAG. Experimental Dynamic Flow может строить планы поверх stable core, но не расширяет неявно язык workflow.

## 2. Immutable registration descriptors расширений

Stable `internal/assistant` знает только provider-neutral контракт и `ProviderRegistration`:

```text
extension package
    └─ ProviderRegistration
          id / display name / stage / factory / version probe
                     │
                     ▼
              internal/bootstrap
                     │
                     ▼
             immutable Registry
                     │
        ┌────────────┴────────────┐
        ▼                         ▼
     runtime                  tooling
```

Правила:

- extension package только **декларирует** registration;
- `init()`-регистрация и package-global mutable registry запрещены;
- production registry собирается ровно один раз в `internal/bootstrap`;
- constructor копирует registrations, проверяет ID/дубликаты и после сборки предоставляет только read-only snapshots;
- runtime и tooling получают один и тот же registry instance/value graph;
- stable core не импортирует Pi/OpenCode или другие concrete extension packages.

Это заимствует удобство registration descriptor из Archon, но не его process-global registration map. Состояние доступных providers определяется object graph Takt, а не порядком скрытых вызовов `register...()`.

## 3. Schema-first canonical operation contracts

Canonical application operation теперь описывается один раз в `internal/appapi`:

```text
OperationDescriptor
  id
  stage
  title / description
  MCP tool name
  InputSchema
  annotations
        │
        ├────► appapi input validation
        ├────► typed Go request decode/handler
        ├────► MCP tools/list
        └────► generated operation documentation
```

`registerOperation[T]` связывает descriptor с конкретным Go request type. Вход сначала проходит JSON Schema descriptor, затем strict typed decode. Поэтому transport не может принимать поле, которое отсутствует в опубликованной schema, или публиковать schema, отличающуюся от application validation.

MCP canonical tools строятся из `appapi.CanonicalOperations()`. В `internal/mcp` остаются собственные descriptors только для MCP-specific external-worker protocol operations, которые не являются canonical application API.

`docs/71-canonical-operation-contracts.generated.md` генерируется из тех же descriptors. Contract test сравнивает файл с `RenderOperationDocs()` и не допускает ручной drift документации.

## Architecture gates

`internal/architecture` закрепляет правила релиза:

- production assistant registry собирается только в `bootstrap`;
- extensions не имеют global registration state;
- stable assistant не импортирует extensions;
- `workflow` и `runtime` используют общий `whenexpr`;
- runtime не содержит второго expression parser;
- appapi содержит schema-first typed registration и input validation;
- MCP canonical tools проецируются из appapi descriptors;
- generated canonical operation document обязан присутствовать.

Дополнительные contract tests проверяют immutability registry, загрузочную проверку `when`, точное соответствие MCP tools appapi descriptors и отсутствие drift generated docs.

## Что намеренно не изменилось

- Config/Profile/Run/protocol API сохраняют свои versioned границы; Workflow
  authoring в `takt/v1alpha1` использует target A0/A1 language surface;
- существующие CLI/MCP operation names сохраняются;
- Dynamic Flow остаётся experimental;
- Pi/OpenCode остаются bundled extensions;
- новый generic plugin framework или DI-container не вводится;
- глобального provider registry нет;
- `when` не получает новых операторов.

Решение зафиксировано ADR-090.
