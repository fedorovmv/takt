# Archon-first Takt YAML A0/A1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Выпустить один нативный Archon-first Takt YAML и durable runtime для bounded repair loops, deterministic evidence, session continuity и recovery, сохранив существующие governance-поля и `output_format`.

**Architecture:** A0 атомарно меняет публичный Workflow/command language и мигрирует весь актуальный in-repo content; dual-parse, importer, второй renderer и второй executor запрещены. Один общий lexer/resolver обслуживает templates, `when`, shell surfaces и script arguments, а существующий DAG scheduler остаётся единственным исполнителем. A1 добавляет loop predicates/actions, signal evidence, session mapping и retry semantics поверх уже durable `RunState`/`LoopIterations`.

**Tech Stack:** Go, `go.yaml.in/yaml/v3`, существующие JSON Schema contracts, `internal/workflow`, `internal/runtime`, `internal/store`, `internal/application`, Go unit/contract/E2E tests, fake assistant host.

---

## Границы, исходное состояние и порядок

План покрывает только срезы A0 и A1, для которых спека имеет статус `READY`:

- A0 — language switch + полная миграция актуального Workflow/command content в одном mergeable release boundary.
- A1 — `until.signal`, `until.requires`, `until_bash`, `loop`, `cancel`, `fresh_context`, `context: shared`, signal evidence и recovery/retry.

Создание `run inspect` (срез B), hard budgets (срез C) и automatic merge mutating fan-out не входят в реализацию. До live capability proof нельзя обещать hard token/tool budget.

Исходный шлюз зафиксирован командой `make check`: `go vet`, обычные и race-тесты, user journeys и TypeScript host smoke прошли. Команда изменяет только форматирование через Makefile; перед реализацией повторно запустить её в рабочей ветке и сравнить результат.

Рабочее дерево уже содержит изменения пользователя в `.gitignore`, `docs/05-implementation-status.md`, `docs/12-document-map.md`, proposal/spec и исследовательском документе. Их не откатывать и не включать в реализацию без отдельного решения.

## Карта файлов и ответственности

| Область | Файлы | Ответственность |
|---|---|---|
| Target model | `internal/spec/spec.go` | root/node/provider/context/loop predicate типы; `output_format` без изменения семантики |
| Loader/authoring | `internal/workflow/load.go`, `internal/workflow/validate.go`, `internal/workflow/references.go`, `internal/workflow/expand.go`, `internal/authoring/authoring.go` | strict target schema, preflight bindings, ancestor checks, normalized scalar `loop` |
| Shared references | создать `internal/flowref/flowref.go`, `internal/flowref/flowref_test.go`; изменить `internal/runtime/template.go`, `internal/whenexpr/whenexpr.go` | один lexer/parser/resolver и context-aware escaping |
| Commands | `internal/command/command.go`, `internal/command/command_test.go`, command resolution call sites | `provider`, `model`, `description`, `argument-hint`; старый `assistant` frontmatter отклоняется |
| Runtime | `internal/runtime/runner.go`, `internal/runtime/actions.go`, `internal/runtime/assistant_node.go`, `internal/runtime/attempt.go`, `internal/runtime/bash.go`, `internal/runtime/script.go`, `internal/runtime/child_run.go` | predicates, signal matcher, `until_bash`, cancel, inter-iteration session и recovery |
| Durable state | `internal/store/store.go`, `schemas/run-state.schema.json` | `workflow_contract`, iteration evidence, signal diagnostics, cancel source |
| Workflow schema | `schemas/workflow.schema.json`, `schemas/schema-subset-v1.schema.json` при необходимости только для ссылок `output_format` | target fields, A0/A1 gating, output contract |
| Application lifecycle | `internal/application/service.go`, `internal/application/operations.go`, `internal/application/operations_test.go` | legacy Run inspect-only guard, retry preserving loop history |
| Generators | `internal/experimental/dynamicplan/compiler.go`, `internal/experimental/dynamicflow/*.go` и все production generators, найденные inventory-тестом | генерировать только target YAML/references |
| Contracts | `internal/workflow/*_test.go`, `internal/runtime/*_test.go`, `internal/authoring/*_test.go`, `internal/store/*_test.go`, `tests/e2e/*` | product correctness и black-box acceptance |
| Content | все файлы с `kind: Workflow` в `internal/profile/builtin/code`, `examples`, `skills/takt/assets/validated-agent-profile/.takt/workflows`, плюс активные Markdown commands и references | атомарная миграция, без legacy dialect |
| Docs | `docs/03-specification.md`, `docs/07-archon-compatibility.md`, `docs/09-runtime-semantics.md`, `docs/10-assistant-adapter-spec.md`, `docs/archive/releases/72-architecture-contracts-v0.1.57.md`, `ARCHITECTURE_DECISIONS.md`, `docs/05-implementation-status.md`, `docs/12-document-map.md`, `CHANGELOG.md`, `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md`, `skills/takt/SKILL.md` и references | публичный контракт и след реализации |

Путь `internal/flowref` оправдан двумя уже существующими потребителями (`template` и `when`) и не является plugin framework: пакет содержит только закрытую грамматику и render policy, а значения разрешаются текущим runtime/authoring state.

---

### Task 1: Зафиксировать A0 contract tests и provenance fixture

**Files:**
- Create: `internal/flowref/flowref_test.go`
- Create: `internal/workflow/archon_contract_test.go`
- Create: `internal/authoring/archon_contract_test.go`
- Create: `tests/e2e/archon_flow_language_test.go`
- Create: `internal/workflow/testdata/archon/archon-feature-development.yaml`
- Create: `internal/workflow/testdata/archon/t1-fix-issue.yaml`
- Create: `internal/workflow/testdata/archon/LICENSE`
- Create: `internal/workflow/testdata/archon/provenance.json`

- [x] **Step 1: Проверить и сохранить pinned source fixture.**

  Взять байты из commit `41765d6a1448da73f398a30e161f3b4eaba0b768`:

  ```bash
  curl --fail --location https://raw.githubusercontent.com/coleam00/Archon/41765d6a1448da73f398a30e161f3b4eaba0b768/.archon/workflows/defaults/archon-feature-development.yaml -o internal/workflow/testdata/archon/archon-feature-development.yaml
  curl --fail --location https://raw.githubusercontent.com/coleam00/Archon/41765d6a1448da73f398a30e161f3b4eaba0b768/.archon/workflows/rasmus-tests/t1-fix-issue.yaml -o internal/workflow/testdata/archon/t1-fix-issue.yaml
  curl --fail --location https://raw.githubusercontent.com/coleam00/Archon/41765d6a1448da73f398a30e161f3b4eaba0b768/LICENSE -o internal/workflow/testdata/archon/LICENSE
  shasum -a 256 internal/workflow/testdata/archon/*.yaml
  ```

  Записать в `provenance.json` URL, original path, commit, license и фактический SHA-256 каждого YAML. Не форматировать и не переписывать fixture.

- [x] **Step 2: Написать падающие lexer tests.**

  Минимальный table-driven contract должен проверять target forms, optional/default suffix, node IDs с дефисом, artifact type с точкой, reserved IDs, `$$`, numeric artifact type и старые формы:

  ```go
  func TestReferenceLexerRejectsLegacyAndReservedForms(t *testing.T) {
      cases := []string{"${nodes.build.output}", "${input}", "$USER_MESSAGE", "$ARTIFACTS_DIRX", "$INPUTS", "$build.artifacts.123.path", "$ARGUMENTS"}
      for _, source := range cases {
          if _, err := flowref.Parse(source, flowref.NonShell); err == nil {
              t.Errorf("Parse(%q) succeeded; target language must reject it", source)
          }
      }
  }
  ```

  Добавить положительные случаи `$ARGUMENTS`, `$INPUTS.item.name`, `$build.output.status`, `$validate.artifacts.report.json.path`, `$LOOP_PREV.review.output:-empty`, `$confirm.output` и literal `$$`.

- [x] **Step 3: Написать loader/authoring/E2E tests до кода.**

  `internal/workflow/archon_contract_test.go` должен требовать новый root (`name`, `description`, `provider`, `model`, `nodes`), отклонять `apiVersion/kind/metadata`, принимать `output_format` без изменений и отклонять A1-only fields до A1. `internal/authoring/archon_contract_test.go` должен фиксировать provider/model preflight, reserved IDs, nested output path и old reference errors. `tests/e2e/archon_flow_language_test.go` должен пройти по всем in-repo Workflow definitions и assert, что loader не видит legacy fields.

- [x] **Step 4: Убедиться, что тесты падают по ожидаемой причине.**

  ```bash
  go test ./internal/flowref ./internal/workflow ./internal/authoring ./tests/e2e -run 'Archon|ReferenceLexer' -count=1
  ```

  Expected: FAIL из-за отсутствующего target parser/fields и отсутствующих fixtures; не исправлять тесты под текущий старый contract.

---

### Task 2: Перевести Go model и JSON Schema на target root/node contract

**Files:**
- Modify: `internal/spec/spec.go`
- Modify: `schemas/workflow.schema.json`
- Modify: `internal/workflow/validate.go`
- Modify: `internal/workflow/load.go`
- Modify: `internal/workflow/validate_test.go`
- Test: `internal/workflow/archon_contract_test.go` (loader cases остаются в этом contract file; отдельный `load_test.go` не создаётся)

- [x] **Step 1: Зафиксировать target types минимальным изменением.**

  `spec.Workflow` получает `Name`, `Description`, `Labels`, `Provider`, `Model`, `Nodes`, сохраняя только существующие root Takt extensions (`Hooks`, `Worktree`, `Input`). `apiVersion`, `kind`, `metadata` и `defaults` не являются публичными Workflow fields. `spec.Node` получает `Provider` и `Context`; внутреннее runtime имя resolved assistant может остаться локальной переменной, но `assistant` не принимается YAML. Сохранить `OutputFormat` и все действующие governance fields без переименования. Не добавлять generic action registry.

- [x] **Step 2: Переписать schema contract.**

  В `schemas/workflow.schema.json` заменить required root на `name` и `nodes`, добавить `description`, `labels`, `provider`, `model`, разрешить node `provider`, но оставить `context: shared` schema-invalid до A1 (A0 принимает только `context: fresh` или отсутствие поля), сохранить `output_format` `$ref` и закрыть `additionalProperties`. A0-схема не принимает `loop`, `cancel`, scalar `until`, `until.signal`, `until.requires`, `until_bash` и `fresh_context`; существующий structured `loop_group.until.exit_code/output_contains` остаётся.

- [x] **Step 3: Перенести structural validation.**

  `internal/workflow/validate.go` проверяет ровно один action, public node IDs, DAG, reserved IDs и `output_format` subset. `internal/workflow/load.go` должен отказывать при old root/old reference forms до создания Run. `Config`, package, workspace и evaluation loaders не менять.

- [x] **Step 4: Запустить узкие tests.**

  ```bash
  go test ./internal/workflow ./internal/spec ./internal/schemacontract -run 'Workflow|Schema|Archon' -count=1
  ```

  Expected after implementation: target fixture parses; old `apiVersion/kind/metadata`, old `assistant/defaults` и `${nodes...}` are rejected; `output_format` positive/negative tests remain green.

---

### Task 3: Реализовать единый reference lexer/resolver и escaping surfaces

**Files:**
- Create: `internal/flowref/flowref.go`
- Modify: `internal/runtime/template.go`
- Modify: `internal/runtime/template_strict_test.go`
- Modify: `internal/whenexpr/whenexpr.go`
- Modify: `internal/whenexpr/whenexpr_test.go`
- Modify: `internal/runtime/bash.go`
- Modify: `internal/runtime/script.go`
- Modify: `internal/authoring/authoring.go`
- Test: `internal/flowref/flowref_test.go`

- [x] **Step 1: Определить небольшую API-поверхность пакета.**

  Пакет должен иметь одну parser/render implementation, например:

  ```go
  type Surface uint8
  const (NonShell Surface = iota; Shell; ScriptArg; ScriptEnv; When)

  type Reference struct {
      Kind Kind
      NodeID string
      Path []string
      Optional bool
      Default string
  }

  func Parse(source string, surface Surface) (Reference, error)
  func Render(source string, surface Surface, resolve func(Reference) (string, bool)) (string, error)
  ```

  `Kind` закрыт перечислением grammar contexts из §5. Lexer работает одним проходом и не рендерит подставленное значение повторно. Не копировать `${...}` regexp в runtime или `whenexpr`.

- [x] **Step 2: Написать parser tests на всю grammar.**

  Проверить maximal munch, правый `.META` для artifact type с точками, decimal `SEG`, optional/default, отсутствующий `$LOOP_PREV` только как allowed empty case, unknown context/node/field, reserved node IDs и rejection positional artifact index. Отдельно проверить, что `$$` превращается в literal `$` только в non-shell surfaces.

- [x] **Step 3: Реализовать context-aware render.**

  Non-shell surfaces получают raw value. Shell surface quote-ит node/artifact/input/fan-out reference как один argument, передаёт `$ARGUMENTS`, `$FEEDBACK`, `$ARTIFACTS_DIR`, `$BASE_BRANCH` через env, сохраняет `$PATH`, `${PATH}`, `$?`, `$$`, `$((...))`, `$(...)` byte-for-byte и отклоняет double quoting `"$node.output"`. Script args/env используют argv/env без shell; inline source не получает raw substitution.

- [x] **Step 4: Подключить runtime и when.**

  `internal/runtime/template.go` оставляет текущую state lookup/artifact JSON path semantics, но вызывает `flowref.Render`. `internal/whenexpr` использует `flowref.Parse` для левой ссылки и оставляет ровно `==`, `!=`, `&&`, `||`; RHS сравнивается как строка без coercion (`0` и `'0'` равны, JSON number/bool/null сериализуются как text). Runtime-only `$FEEDBACK`/`$FANOUT` в `when` отклоняются.

- [x] **Step 5: Запустить parser/runtime checks.**

  ```bash
  go test ./internal/flowref ./internal/runtime ./internal/whenexpr ./internal/authoring -run 'Template|Reference|When|Shell|Script' -count=1
  ```

  Expected: injection remains one shell argument, native shell syntax survives, old syntax fails closed, unresolved required references return execution/authoring errors rather than empty strings.

---

### Task 4: Перевести commands и binding preflight

**Files:**
- Modify: `internal/command/command.go`
- Modify: `internal/command/command_test.go`
- Modify: `internal/workflow/references.go`
- Modify: `internal/runtime/assistant_node.go`
- Modify: `internal/runtime/actions.go`
- Modify: `internal/runtime/runner.go`
- Modify: all active Markdown commands under `internal/profile/builtin/code/commands`, `examples/**/commands`, `skills/takt/**/commands`

- [x] **Step 1: Добавить failing frontmatter tests.**

  `Parse` должен принимать `provider`, `model`, `description`, `argument-hint`, сохранять body без переписывания и отклонять `assistant` как legacy key. Test должен проверить precedence `node.provider → command.provider → workflow.provider` и отсутствие silent fallback.

- [x] **Step 2: Перенести command model.**

  Переименовать публичное `Command.Assistant` в `Command.Provider`, добавить `ArgumentHint`, обновить `Resolver` и runtime call sites. Не менять Config `assistants` binding: target `provider` — это имя существующего binding, а не provider-specific adapter.

- [x] **Step 3: Закрыть preflight.**

  `internal/workflow/references.go` разрешает root/node/command provider и model через существующие Config registries; неизвестное имя, mismatch или missing command дают ошибку до Run. `context: fresh` — default; `context: shared` проверяет уникального ancestor и до A1 возвращает понятную unsupported/authoring ошибку, если реализация ещё не включена.

- [x] **Step 4: Мигрировать frontmatter и проверить grep.**

  Перенести активные `assistant:` keys в `provider:` только в Markdown command frontmatter. Исторические release docs не менять. Проверка:

  ```bash
  rg -n '^assistant:' internal/profile examples skills --glob '*.md'
  rg -n '\$USER_MESSAGE|\$\{(nodes|input|inputs|fanout|feedback|approvals|loop\.previous)' internal/profile examples skills --glob '*.md' --glob '*.yaml'
  ```

  Expected: no active hits after migration; hits in historical docs are not part of A0 content and must be explicitly excluded from inventory test.

---

### Task 5: Добавить persisted Workflow contract guard

**Files:**
- Modify: `internal/store/store.go`
- Modify: `schemas/run-state.schema.json`
- Modify: `internal/runtime/runner.go`
- Modify: `internal/application/service.go`
- Modify: `internal/application/operations.go`
- Modify: `internal/application/operations_test.go`
- Modify: `internal/store/store_test.go`

- [x] **Step 1: Добавить failing state tests.**

  Проверить новый Run с `workflow_contract: takt-flow/v1alpha1`, legacy Run без поля и unknown contract. `run.get`, `summary`, `events` должны читать все три состояния; `resume`, `retry`, `fork` должны отказать для отсутствующего/unknown contract до запуска nodes.

- [x] **Step 2: Реализовать discriminator и guard.**

  Добавить константу workflow contract и поле `RunState.WorkflowContract`. `Runner.StartWithOptions` записывает его до первого execution commit. Application lifecycle загружает state из Store, сравнивает contract с текущей definition и возвращает incompatible-definition error без изменения state. Не строить state migrator и не добавлять второй persistence format.

- [x] **Step 3: Обновить schema и durable reload tests.**

  `schemas/run-state.schema.json` допускает поле как discriminator; legacy state без поля остаётся loadable для read-only operations. Проверить, что control/CLI возвращают state, перечитанный из Store, а не in-memory pointer.

- [x] **Step 4: Запустить lifecycle checks.**

  ```bash
  go test ./internal/store ./internal/application ./internal/runtime -run 'Contract|Legacy|Resume|Retry|Fork|Reload' -count=1
  ```

  Expected: old state is inspectable but cannot mutate; new state carries `takt-flow/v1alpha1` from creation.

---

### Task 6: Атомарно мигрировать весь in-repo Workflow content и generators

**Files:**
- Modify: every YAML file returned by the inventory command below that contains `kind: Workflow`, including `internal/profile/builtin/code/workflow.yaml`, `internal/profile/builtin/code/workflows/**/*.yaml`, `examples/**/*.yaml`, and `skills/takt/assets/validated-agent-profile/.takt/workflows/**/*.yaml`
- Modify: `internal/experimental/dynamicplan/compiler.go`, `internal/experimental/dynamicflow/*.go`, other generator files reported by inventory
- Modify: `tests/e2e/*.go`, `internal/**/*_test.go`, `examples/**/README.md`, `skills/takt/SKILL.md`, `skills/takt/references/*.md` where they embed active Workflow/reference examples
- Test: `tests/e2e/archon_flow_language_test.go`

- [x] **Step 1: Получить полный inventory до миграции.**

  ```bash
  find . -type f \( -name '*.yaml' -o -name '*.yml' \) -not -path './.git/*' -print0 \
    | xargs -0 rg -l '^kind:[[:space:]]*Workflow$' | sort > /tmp/takt-workflow-inventory.before
  test "$(wc -l < /tmp/takt-workflow-inventory.before | tr -d ' ')" -eq 64
  ```

  Число 64 — текущая проверка полноты, а не нормативный предел; после миграции тест сканирует критерий `all in-repo YAML definitions with kind: Workflow`.

- [x] **Step 2: Применить field map §1.1.1 без dual parse.**

  Для каждого Workflow удалить `apiVersion`, `kind`, `metadata`, перенести `metadata.name/description/labels` в root, `defaults.assistant/model` в `provider/model`, удалить default `session: fresh`, переименовать node `assistant` в `provider`, перенести `session: fresh` в `context: fresh`, сохранить `output_format` byte-for-byte и обновить все references на target grammar. Existing `loop_group` semantics не менять до A1.

- [x] **Step 3: Обновить generators вместо post-processing.**

  Все production paths, создающие `spec.Workflow` или строки `${...}`, должны сразу создавать target fields/references: `$ARGUMENTS`, `$INPUTS.*`, `$FANOUT.*`, `$FEEDBACK`, `$node.*`, `$LOOP_PREV.*`, `$BASE_BRANCH`. Запретить helper, который возвращает legacy YAML для последующей конвертации.

- [x] **Step 4: Мигрировать examples/tests/skill references.**

  Не менять исторические release documents и Config/package/workspace/evaluation schemas. Active examples, Go fixtures, E2E fixtures и `skills/takt` должны показывать только target syntax; examples с `output_format` должны сохранить nested paths и fail-closed expectations.

- [x] **Step 5: Доказать отсутствие смешанного языка.**

  ```bash
  go test ./tests/e2e -run TestArchonFlowLanguageInventory -count=1
  rg -n 'apiVersion: takt/v1alpha1|^kind: Workflow$|metadata:|defaults:|assistant:|\$\{nodes|\$\{input|\$USER_MESSAGE' internal/profile examples skills --glob '*.yaml' --glob '*.md'
  ```

  Expected: inventory test passes; legacy hits отсутствуют в active content. Если hit принадлежит Config/package или historical doc, тест должен классифицировать его вне Workflow scope, а не молча игнорировать.

---

### Task 7: Закрыть A0 `output_format`, authoring paths и documentation trail

**Files:**
- Modify: `internal/runtime/output_format.go`, `internal/runtime/output_format_test.go`, `internal/runtime/actions.go`
- Modify: `internal/authoring/authoring.go`, `internal/authoring/authoring_test.go`
- Modify: `schemas/workflow.schema.json`, `schemas/schema-subset-v1.schema.json` only when schema reference paths require it
- Modify: `docs/03-specification.md`, `docs/07-archon-compatibility.md`, `docs/09-runtime-semantics.md`, `docs/archive/releases/72-architecture-contracts-v0.1.57.md`, `ARCHITECTURE_DECISIONS.md`, `docs/05-implementation-status.md`, `docs/12-document-map.md`, `CHANGELOG.md`, `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md`, `skills/takt/SKILL.md`, `skills/takt/references/*.md`

- [x] **Step 1: Сохранить existing structured-output tests.**

  До изменения target model добавить negative cases для missing required field, wrong type и invalid nested `items_from` path, а также positive case с `output_format` без extra fields. Test должен доказывать, что `output_format` остаётся optional, fail-closed и использует `takt-schema-subset/v1`.

- [x] **Step 2: Перенести authoring validation.**

  `internal/authoring` проверяет доступность nested JSON paths до Run, `items_from` и typed artifacts через target references. Positional artifact index отклоняется; metadata (`id`, `type`, `mime`, `path`, `sha256`, `size`, producer fields, attempt) доступна только по имени/type grammar.

- [x] **Step 3: Обновить публичные документы.**

  В `docs/03` описать target root/node/command, все reference contexts, shell/non-shell `$$`, `output_format`, A0/A1 gating и old Run guard. В `docs/09` описать runtime precedence; в ADR зафиксировать language switch и отсутствие compatibility layer. `docs/05`, `CHANGELOG.md`, `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md`, `skills/takt` должны отражать только фактически реализованный срез после прохождения tests.

- [x] **Step 4: Завершить A0 release gate.**

  ```bash
  gofmt -w cmd internal sdk reference tests
  go test ./... -count=1
  go test -race ./... -count=1
  go vet ./...
  make check
  ```

  Expected: весь active content загружается новым loader, old Workflow/reference syntax не загружается, `output_format` contracts зелёные, `make check` зелёный. Только после этого A0 можно считать mergeable language-switch boundary.

---

### Task 8: Добавить A1 model, schema и authoring tests для loop predicates/actions

**Files:**
- Modify: `internal/spec/spec.go`
- Modify: `internal/workflow/validate.go`, `internal/workflow/references.go`, `internal/workflow/expand.go`
- Modify: `schemas/workflow.schema.json`
- Create or modify: `internal/workflow/loop_contract_test.go`
- Modify: `internal/authoring/authoring.go`, `internal/authoring/authoring_test.go`

- [x] **Step 1: Написать failing A1 tests.**

  Покрыть scalar `until`, terminal-node inference, explicit `until.signal`, `requires` bounded AND, `until_bash`, `loop`, `cancel`, `fresh_context`, `context: shared`, duplicate/hidden/out-of-body references, zero/multiple terminal nodes и nested loop rejection. Проверка должна различать authoring error и runtime failure.

- [x] **Step 2: Расширить types без нового DSL.**

  Добавить в `UntilSpec` `Signal *string`/canonical signal value и `Requires []UntilRequirement`; requirement содержит `Node`, optional `ExitCode`, `OutputContains`. Добавить `UntilBash`, `FreshContext` и scalar `LoopSpec` только как public model, который loader нормализует в существующий `LoopGroupSpec`. Добавить `Cancel` reason action. Не добавлять `next`, arbitrary expressions, gate objects или model-selected worker types.

- [x] **Step 3: Реализовать validation rules.**

  Signal: `^[A-Z][A-Z0-9_-]{0,63}$`; `requires` ≤64, unique public body nodes, no primary duplicate, no `signal`, current body only. Scalar sugar разрешён ровно при одном direct public terminal node; zero/multiple terminal nodes — authoring error. `context: shared` требует одного транзитивного explicit `depends_on` ancestor с тем же provider/model и не добавляет implicit edge; missing/ambiguous/concurrent source — authoring error. Nested `loop_group` остаётся запрещён.

- [x] **Step 4: Расширить schema gating.**

  A1 schema принимает только перечисленные fields/actions и закрывает unknown keys. Все условия должны нормализоваться в существующие scheduler structures; schema не создаёт second executor.

- [x] **Step 5: Проверить validation до runtime.**

  ```bash
  go test ./internal/workflow ./internal/authoring -run 'Loop|Until|Context|Terminal|Requires|Cancel' -count=1
  ```

  Expected: malformed signal/requirements/context rejected before Run; valid Archon `loop` fixture normalizes to one `loop_group`.

---

### Task 9: Реализовать signal matcher и durable predicate evidence

**Files:**
- Create: `internal/runtime/signal.go`
- Create: `internal/runtime/signal_test.go`
- Modify: `internal/runtime/runner.go`
- Modify: `internal/runtime/actions.go`
- Modify: `internal/store/store.go`
- Modify: `schemas/run-state.schema.json`
- Modify: `internal/runtime/runner_test.go`

- [x] **Step 1: Написать table-driven matcher tests.**

  Cases: exact `<promise>BUILD-CLEAN</promise>`, exact final non-empty line, negative prose, trailing punctuation, code fence ` ``` `/`~~~`, unclosed fence, expected signal plus another valid promise, promise plus duplicate final line, invalid charset, truncation. Тесты должны возвращать `matched_signal`, `signal_missing`, `signal_ambiguous` и never accept signal inside fence.

- [x] **Step 2: Реализовать bounded matcher.**

  `internal/runtime/signal.go` принимает full normalized output, исключает fenced blocks по правилам спеки, считает ровно occurrences expected/other promise и exact final line, возвращает typed result. Не искать signal в bounded user-facing projection и не использовать substring semantics для primary authority.

- [x] **Step 3: Применить truncation scope.**

  Для узлов, участвующих в primary `until`/`requires`, `output_truncated` превращается в execution/protocol failure до записи `node.completed`; matcher не запускается. Остальные nodes сохраняют completed + `output_truncated=true`. Это изменение не должно глобально ломать существующие многословные bash/assistant nodes.

- [x] **Step 4: Сохранить evidence durable.**

  Расширить `LoopIterationState` и event data полями `MatchedSignal *string`, `SignalDiagnostic` (`nil`, `signal_missing`, `signal_ambiguous`) и `UntilBash *PredicateEvidence`. `PredicateEvidence` содержит только нормализованные stdout/stderr, exit code, duration, terminal status, truncation и error code; не добавлять domain validation JSON. В `RunState` сохранить `CancelSource`, `CancelNodePath`, `CancelIteration` и `CancelReason`. Отсутствие измеренного signal сериализовать как `null`, а не `""`; при predicate без signal оба поля `null`. Проверить Store reload.

- [x] **Step 5: Запустить matcher checks.**

  ```bash
  go test ./internal/runtime ./internal/store -run 'Signal|Truncat|LoopIteration' -count=1
  ```

  Expected: fenced/ambiguous/truncated source never completes loop; non-source truncation remains backward runtime behavior; matched signal survives reload.

---

### Task 10: Подключить predicates, `until_bash`, scalar loop и cancel к единому scheduler

**Files:**
- Modify: `internal/runtime/runner.go`
- Modify: `internal/runtime/actions.go`
- Modify: `internal/runtime/bash.go`
- Modify: `internal/runtime/runner_test.go`
- Modify: `internal/runtime/composition_test.go`
- Modify: `internal/workflow/expand.go`

- [x] **Step 1: Написать failing scheduler tests.**

  Test sequence: `review` emits signal while `validate` returns RED → iteration repeats; validator exit 0 + signal → accepted; assistant `command` exit 0 in `requires` is advisory but can match; missing/skipped required evidence → `required_evidence_missing` safe-stop; `until_bash` numeric non-zero repeats, exit 0 participates in AND, start/protocol/timeout/cancel stops; `cancel` inside body cancels whole Run and children without next iteration/downstream.

- [x] **Step 2: Обобщить `untilSatisfied`.**

  Заменить single-node check на bounded evaluation текущего iteration snapshot: primary `node/signal/exit_code/output_contains` AND `until_bash` result AND every `requires`. `requires` читает только current body nodes; `loop_previous` never participates. Не менять existing `allow_failure`: only numeric exit remains product RED; start/protocol/internal/timeout/cancel remain execution failures.

- [x] **Step 3: Выполнить `until_bash` существующим deterministic path.**

  После body completion, даже если primary predicate уже true, вызвать existing bash execution in owner execution worktree with same env/redaction/sandbox/timeout. Persist stdout/stderr/exit/duration as iteration evidence. Numeric non-zero is next iteration; other failure kinds safe-stop. No per-action retry/allow_failure field.

- [x] **Step 4: Нормализовать `loop` и `cancel`.**

  Loader turns single `prompt|command` loop into one `LoopGroupSpec` body with logical node mapping; runtime calls `runLoopGroup`. `cancel` renders reason, commits `cancel_source=workflow`, active path/iteration, cascades running/child cancellation and marks pending/downstream according to the existing scheduler cancellation semantics. Do not add back transitions or a second executor.

- [x] **Step 5: Проверить scheduler.**

  ```bash
  go test ./internal/runtime ./internal/workflow -run 'Loop|Until|Cancel|Required|Bash' -count=1
  ```

  Expected: `BUILD-CLEAN` cannot hide deterministic RED; all AND predicates are evaluated; cancel and failure precedence match §9.

---

### Task 11: Реализовать session continuity, exact resume и operator retry history

**Files:**
- Modify: `internal/runtime/assistant_node.go`
- Modify: `internal/runtime/actions.go`
- Modify: `internal/runtime/attempt.go`
- Modify: `internal/runtime/runner.go`
- Modify: `internal/application/operations.go`
- Modify: `internal/application/operations_test.go`
- Modify: `internal/runtime/runner_test.go`

- [x] **Step 1: Написать failing session tests.**

  Fake host должен подтвердить: first iteration uses fresh; `fresh_context:false` on iteration N>1 seeds exact Session ID from `loop_iterations[N-1].nodes[logical]`; `true` clears it; mismatch returned by host is execution failure; no fresh fallback. `context:shared` resumes one unambiguous sequential ancestor and rejects parallel/missing/mismatched source.

- [x] **Step 2: Исправить precedence order.**

  Resolver first applies normal node/root `context → fresh`, then applies inter-iteration `fresh_context` for N>1. `fresh_context:false` wins over default fresh; explicit conflicting `context: fresh + fresh_context:false` returns authoring error or the documented `fresh_context` winner consistently. Retry inside one iteration continues to use `attempts.retry_session` (`fresh|reuse`), not `context:shared`.

- [x] **Step 3: Seed durable session without new entity.**

  Before executing a new body iteration, copy the logical node’s prior `SessionID` from `LoopIterations[last].Nodes`/`LoopPrevious` into transient state. Ensure provider binding and logical model match. Completed nodes are not rerun after pause/process-loss; only pending/running boundary is resumed.

- [x] **Step 4: Preserve loop history on operator retry.**

  In `RunService.Retry`, retain `LoopIterations`, `LoopPrevious`, `OperatorRetries`, execution records and diagnostic fingerprints when resetting a failed loop container. Define retry as full next iteration `N+1` seeded from failed snapshot; reject when `N == max_iterations`. Do not replace the whole `NodeState` with a history-less pending object.

- [x] **Step 5: Run exact resume/retry tests.**

  ```bash
  go test ./internal/runtime ./internal/application -run 'Session|Resume|Retry|Loop' -count=1
  go test -race ./internal/runtime ./internal/application -run 'Session|Resume|Retry|Loop' -count=1
  ```

  Expected: exact Session IDs and usage survive fake-host resume; history has immutable iterations; operator retry starts N+1 and never silently restarts at 1.

---

### Task 12: Провести A1 fake-host vertical slice и vendored Archon loop fixture

**Files:**
- Modify: `tests/e2e/archon_flow_language_test.go`
- Modify: `tests/e2e/core_contracts_test.go` or create a focused `tests/e2e/archon_loop_contracts_test.go`
- Modify: `internal/runtime/runner_test.go`, `internal/runtime/composition_test.go`
- Use: `internal/workflow/testdata/archon/t1-fix-issue.yaml`
- Modify: `docs/05-implementation-status.md`, `CHANGELOG.md`, `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md`

- [x] **Step 1: Add fake-host vertical fixture.**

  Build a deterministic flow `analyze → write-tests/implement (parallel only when independent) → validate bash → review signal`, where first validation is RED, review emits feedback, second iteration exact-resumes the review session, validation becomes PASS and the loop completes. Keep deterministic validator output separate from assistant prose.

- [x] **Step 2: Add Archon A1 parse/normalize test.**

  Load `t1-fix-issue.yaml` byte-for-byte, normalize its `loop` into one Takt loop group, inject a test Config binding for `claude`, `large`, `@mini`, and verify parse/validate/normalization only. Do not invoke external `gh`, project commands or network from the fixture test.

- [x] **Step 3: Cover recovery matrix.**

  E2E/Go tests must cover pause/approval inside loop preserving active iteration, process-loss resume without completed-node replay, validator RED, signal missing/ambiguous, required evidence missing, max iterations, timeout/cancel precedence and operator retry.

- [x] **Step 4: Run A1 acceptance suite.**

  ```bash
  go test ./internal/workflow ./internal/runtime ./internal/application ./tests/e2e -run 'Archon|Loop|Session|Recovery|Approval|Cancel|Required' -count=1
  ```

  Expected: acceptance items 9–20 from the spec pass, infrastructure contracts remain separate from quality benchmarks, and no external Archon provider/tool is required.

---

### Task 13: Обновить contract trail и закрыть A0/A1 release gate

**Files:**
- Modify: `docs/03-specification.md`
- Modify: `docs/09-runtime-semantics.md`
- Modify: `docs/10-assistant-adapter-spec.md`
- Modify: `docs/07-archon-compatibility.md`
- Modify: `docs/archive/releases/72-architecture-contracts-v0.1.57.md`
- Modify: `ARCHITECTURE_DECISIONS.md`
- Modify: `docs/05-implementation-status.md`
- Modify: `docs/12-document-map.md`
- Modify: `skills/takt/SKILL.md`, `skills/takt/references/*.md`
- Modify: `CHANGELOG.md`, `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md`

- [x] **Step 1: Синхронизировать документы с фактической реализацией.**

  `docs/03` показывает единственный target YAML и examples для `output_format`, signal/requires, `until_bash`, `loop`, context/session и `$$`. `docs/09` фиксирует failure matrix, durable iteration evidence, truncation scope и retry priority. `docs/10` фиксирует exact Session ID/no fresh fallback. `skills/takt` учит target syntax, а не старый `${...}`.

- [x] **Step 2: Обновить status только после доказательств.**

  В `docs/05` пометить A0/A1 как реализованные только после полного gate; B/C и parallel mutating merge оставить deferred/conditional. `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md` содержит команды, даты, результаты и ограничения fake-host; не заявлять live Pi/OpenCode budget proof.

- [x] **Step 3: Выполнить полный release gate.**

  ```bash
  gofmt -w cmd internal sdk reference tests
  go test ./... -count=1
  go test -race ./... -count=1
  go vet ./...
  ./scripts/check-docs.sh
  make check
  ./scripts/verify.sh
  ```

  Если `scripts/check-docs.sh` отсутствует в данной alpha-ветке, зафиксировать это как ожидаемую границу и использовать существующие `make check`/`verify.sh`, не создавать новый shell test framework.

- [x] **Step 4: Выполнить self-review перед передачей.**

  Проверить по спецификации: §1 language surface/migration, §2 P0 decisions, §3 authoring, §4 iteration/recovery, §5 references/escaping, §6 sessions/worktrees/fan-out, §7 policies, §8 budgets/observability boundary, §9 failure matrix, §10 acceptance. Поискать в плане и diff placeholder markers, legacy dual-parse/importer/second executor и удалить их. Проверить, что `output_format` упомянут в model, schema, authoring, runtime tests, migration и docs.

---

## Отложенные работы и условия возврата в проектирование

- `run inspect --node/--iteration` — отдельный срез B после появления A1 durable evidence; добавлять одну canonical `internal/appapi.OperationDescriptor`, не transport-specific state.
- Loop token/tool budgets — срез C только после live Pi/OpenCode capability proof; required enforcement должен fail-before-execution, optional сохраняет degraded decision.
- Merge mutating fan-out — не реализовывать без реального use case и явной merge action; worktree children передают только outputs/artifacts.
- Если во время реализации обнаружится новый reference context, `output_format` consumer, persisted state или Archon construct без однозначной semantics, остановить кодирование, записать источник и вернуть вопрос в `design-unknowns`; не прятать решение в adapter или prompt.
- Commit в рамках этой рабочей сессии не выполнять. После прохождения отдельных задач можно делать локальные checkpoint commits в отдельной execution-сессии, но mergeable A0 остаётся одной атомарной границей.
