# Archon-first Takt YAML Review Addendum Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Закрыть blind spots исходного A0/A1 implementation plan, обнаруженные при ревью незакоммиченной реализации, не дублируя уже описанные loop/runtime fixes.

**Architecture:** Addendum уточняет только контракты, которых не было либо которые были недостаточно однозначны в основном плане. Исправления остаются внутри существующих `flowref`, authoring, workflow, runtime и redaction boundaries; новые executor, parser, persistence layer или plugin abstraction не добавляются.

**Tech Stack:** Go, существующие `internal/flowref`, `internal/workflow`, `internal/runtime`, `internal/redact`, JSON Schema contracts и Go unit/contract/E2E tests.

---

## Связь с основным планом

Основной план: `docs/superpowers/plans/2026-08-11-archon-first-takt-yaml-a0-a1.md`.

Этот addendum не повторяет уже явно заданные там исправления `$LOOP_PREV`,
predicate truncation, `requires`, `until_bash`, cancel/recovery, retry N+1,
полную миграцию Go fixtures и A1 acceptance suite. Он добавляет шесть
контрактов, которые исходный план не превратил в однозначные negative tests.

Все checkbox здесь изначально незакрыты. Наличие production-кода или зелёного
`make check` не является основанием поставить `[x]`: сначала должен пройти
указанный failing-then-passing test.

---

### Task 1: Redact every new durable textual field

**Files:**
- Modify: `internal/redact/state.go`
- Modify: `internal/redact/redact_test.go`
- Read for field ownership: `internal/store/store.go`

- [x] **Step 1: Добавить failing redaction test.**

  Расширить `TestRedactRunStateCoversDurableFields` либо создать
  `TestRedactRunStateCoversArchonLoopEvidence` с одним зарегистрированным
  secret во всех новых текстовых полях:

  ```go
  state := &store.RunState{
      CancelReason: "cancel=" + secret,
      Nodes: map[string]*store.NodeState{
          "repair": {
              LoopIterations: []store.LoopIterationState{{
                  Iteration: 1,
                  UntilNode: "review",
                  Nodes: map[string]store.NodeState{},
                  UntilBash: &store.PredicateEvidence{
                      Stdout: "stdout=" + secret,
                      Stderr: "stderr=" + secret,
                  },
              }},
          },
      },
  }
  RedactRunState(redactor, state)
  raw, _ := json.Marshal(state)
  if strings.Contains(string(raw), secret) {
      t.Fatalf("secret remained in durable Archon fields: %s", raw)
  }
  ```

- [x] **Step 2: Запустить тест и подтвердить текущий дефект.**

  ```bash
  go test ./internal/redact -run TestRedactRunStateCoversArchonLoopEvidence -count=1
  ```

  Expected before fix: FAIL; secret остаётся в `cancel_reason` либо
  `loop_iterations[].until_bash.stdout/stderr`.

- [x] **Step 3: Добавить минимальную redaction в существующий traversal.**

  В `RedactRunState` обработать `state.CancelReason`. В цикле
  `RedactNodeState` по `LoopIterations` обработать `UntilBash.Stdout` и
  `UntilBash.Stderr` через тот же `Redactor.String`. Не создавать второй
  redactor и не сериализовать state повторно только ради обхода полей.

- [x] **Step 4: Проверить redaction и persistence regressions.**

  ```bash
  go test ./internal/redact ./internal/runtime ./internal/store -run 'Redact|LoopIteration|Cancel' -count=1
  ```

  Expected: PASS; измеренный stdout/stderr сохраняется, известный secret заменён
  на `<redacted>` до persistence.

---

### Task 2: Specify the complete shell quote state and non-shell data boundary

**Files:**
- Modify: `internal/flowref/flowref.go`
- Modify: `internal/flowref/flowref_test.go`
- Modify: `internal/runtime/script.go`
- Modify: `internal/runtime/template_strict_test.go`
- Modify: `internal/authoring/authoring.go`
- Modify: `internal/authoring/authoring_test.go`

- [x] **Step 1: Добавить table-driven negative tests для double-quoted context.**

  Тест обязан проверять не только точную форму `"$build.output"`, но любое
  нахождение Takt node reference внутри открытого double-quoted shell segment:

  ```go
  cases := []string{
      `echo "$build.output"`,
      `echo "prefix=$build.output"`,
      `echo "$build.output:suffix"`,
      `value="before $build.output after"`,
  }
  for _, source := range cases {
      if _, err := flowref.Render(source, flowref.Shell, resolver); err == nil {
          t.Errorf("double-quoted Takt reference accepted: %s", source)
      }
  }
  ```

  Положительные cases обязаны сохранить обычные `"$PATH"`, `"${PATH}"`,
  `$(command)`, `$((1 + 1))`, `$?` и `$$` byte-for-byte.

- [x] **Step 2: Добавить test на required `$BASE_BRANCH`.**

  Проверить три состояния shell surface:

  ```go
  // Durable base exists: env receives the exact value.
  // Durable base is absent: required $BASE_BRANCH fails before bash starts.
  // Durable base is absent with suffix ?: empty value is intentional.
  ```

  `until_bash` и обычный `bash` должны использовать один helper построения
  bare-context env; запрещено иметь отдельные неполные map literals.

- [x] **Step 3: Добавить test, запрещающий interpolation inline source.**

  Для `runtime: python|node` source вида:

  ```yaml
  inline: |
    print("$ARGUMENTS")
  ```

  authoring должен вернуть ошибку с указанием передать значение через
  `script.args` или `script.env`. `args` и `env` при этом обязаны принять то же
  значение byte-for-byte без shell evaluation.

- [x] **Step 4: Реализовать минимальный quote-state scanner.**

  В существующем однопроходном lexer хранить состояние `unquoted|single|double`
  с учётом backslash внутри double quotes. Takt node/input/artifact/previous
  reference в `double` отклоняется независимо от позиции в строке. Обычные shell
  variables остаются native. Inline source не проходит через `Render`; renderer
  вызывается только для `args`, `env`, path и working directory соответствующей
  surface.

- [x] **Step 5: Запустить focused checks.**

  ```bash
  go test ./internal/flowref ./internal/runtime ./internal/authoring -run 'Shell|Quote|BaseBranch|InlineScript|ScriptArg|ScriptEnv' -count=1
  ```

  Expected: PASS; embedded double quoting отклонено, native shell syntax не
  изменена, inline source не получает user-controlled substitution.

---

### Task 3: Define nearest shared-session selection and concurrent-consumer rejection

**Files:**
- Modify: `internal/workflow/validate.go`
- Modify: `internal/workflow/loop_contract_test.go`
- Modify: `internal/runtime/assistant_node.go`
- Modify: `internal/runtime/runner_test.go`

- [x] **Step 1: Добавить authoring tests для nearest semantics.**

  Зафиксировать следующие DAG cases:

  ```text
  A(assistant) -> B(assistant) -> C(context: shared)  = source B
  A(assistant) -> bridge(bash) -> C(shared)          = source A
  A(assistant) -> B1(assistant) -> C(shared)
               -> B2(assistant) -> C(shared)         = ambiguous error
  A(assistant) -> C1(shared)
               -> C2(shared)                         = concurrent reuse error
  A(assistant) -> C1(shared) -> C2(shared)            = valid sequential reuse
  ```

  Provider/model mismatch исключает candidate до выбора nearest source.

- [x] **Step 2: Зафиксировать алгоритм выбора.**

  В одном explicit DAG scope собрать совместимых assistant ancestors. Candidate
  считается затенённым, если существует другой совместимый candidate, который
  является его descendant и одновременно ancestor target node. После удаления
  затенённых candidates должен остаться ровно один источник. Ноль — missing,
  несколько несравнимых — ambiguous.

- [x] **Step 3: Запретить конкурентный resume одного Session ID.**

  После вычисления source для всех `context: shared` nodes проверить пары
  consumers одного source. Если между consumers нет ordering path ни в одном
  направлении, workflow отклоняется до Run. Не добавлять implicit dependency.

- [x] **Step 4: Использовать результат authoring resolution в runtime.**

  Runtime обязан повторить тот же bounded algorithm либо получить заранее
  нормализованный source ID; отдельная эвристика «посчитать все session IDs»
  запрещена. Scope не пересекает loop/subworkflow/child-Run boundary.

- [x] **Step 5: Запустить validation/session tests.**

  ```bash
  go test ./internal/workflow ./internal/runtime -run 'SharedContext|SharedSession|Nearest|Concurrent' -count=1
  ```

  Expected: linear chain выбирает ближайшую сессию; diamond и parallel consumers
  fail-before-Run; silent fresh fallback отсутствует.

---

### Task 4: Distinguish foreign signal from missing signal

**Files:**
- Modify: `internal/runtime/signal.go`
- Modify: `internal/runtime/signal_test.go`

- [x] **Step 1: Добавить недостающие matcher cases.**

  ```go
  cases := []struct {
      output string
      want   SignalDiagnostic
  }{
      {`ordinary prose without a promise`, SignalMissing},
      {`<promise>OTHER</promise>`, SignalAmbiguous},
      {"<promise>OTHER</promise>\nBUILD-CLEAN", SignalAmbiguous},
      {"<promise>BUILD-CLEAN</promise>\n<promise>OTHER</promise>", SignalAmbiguous},
  }
  ```

- [x] **Step 2: Подтвердить неправильный diagnostic до исправления.**

  ```bash
  go test ./internal/runtime -run TestMatchSignalForeignPromise -count=1
  ```

  Expected before fix: foreign-only promise возвращает `signal_missing`.

- [x] **Step 3: Исправить порядок классификации.**

  После удаления fences сначала посчитать все валидные promise/final-line
  occurrences. Любой чужой valid promise или более одного occurrence даёт
  `signal_ambiguous`. `signal_missing` используется только когда expected signal
  отсутствует и чужих completion signals нет.

- [x] **Step 4: Запустить matcher suite.**

  ```bash
  go test ./internal/runtime -run MatchSignal -count=1
  ```

  Expected: PASS для exact promise/final line, missing prose, foreign signal,
  duplicates, fences и unclosed fence.

---

### Task 5: Make A1 field placement fail closed in schema and Go validation

**Files:**
- Modify: `schemas/workflow.schema.json`
- Modify: `internal/workflow/validate.go`
- Modify: `internal/workflow/loop_contract_test.go`
- Modify: `internal/schemacontract/schema_registry_test.go`

- [x] **Step 1: Добавить loader tests на допустимое расположение полей.**

  Positive YAML cases:

  ```yaml
  # cancel и context: shared разрешены как loop body node actions/settings.
  loop_group:
    max_iterations: 2
    until: BUILD-CLEAN
    nodes:
      - id: source
        prompt: work
      - id: stop
        depends_on: [source]
        cancel: stop
  ```

  Negative cases: `fresh_context` и `until_bash` на обычном `bash`, `prompt`,
  `command` или `cancel` node вне `loop|loop_group` container.

- [x] **Step 2: Синхронизировать `$defs.node` и `$defs.loopChildNode`.**

  `loopChildNode` должен принимать `cancel` и `context: shared`, если они
  поддержаны runtime/authoring контрактом. Container-only `fresh_context` и
  `until_bash` остаются только внутри `$defs.loop`/`$defs.loopGroup`, а не как
  общие node properties. Go validation отклоняет любое значение, которое могло
  попасть из programmatic definition в неподдерживаемое место.

- [x] **Step 3: Проверить schema/loader agreement.**

  ```bash
  go test ./internal/schemacontract ./internal/workflow -run 'WorkflowSchema|LoopChild|FieldPlacement|Cancel' -count=1
  ```

  Expected: JSON Schema и `workflow.Validate` принимают и отклоняют одинаковые
  definitions; ни одно governance field не игнорируется молча.

---

### Task 6: Include root README and current user-facing docs in A0 inventory

**Files:**
- Modify: `README.md`
- Modify: `tests/e2e/archon_flow_language_test.go`
- Read/scan: `examples/**/README.md`, `skills/takt/**/*.md`, current non-historical docs listed by `docs/12-document-map.md`

- [x] **Step 1: Добавить failing current-doc inventory test.**

  Test должен читать root `README.md`, active example READMEs и `skills/takt`
  references и отклонять executable Workflow snippets с:

  ```text
  kind: Workflow
  apiVersion: takt/v1alpha1
  ${input}
  ${nodes.
  ${fanout.
  $USER_MESSAGE
  ```

  Config/Profile/BlockPackage snippets и исторические release documents не
  классифицируются как Workflow snippets. Исключения перечисляются typed list
  по document kind/path, а не общей regexp allowlist.

- [x] **Step 2: Подтвердить текущий README failure.**

  ```bash
  go test ./tests/e2e -run TestArchonCurrentDocumentationInventory -count=1
  ```

  Expected before fix: FAIL на root `README.md` quick start, composition,
  governed child/fan-out или artifact examples.

- [x] **Step 3: Мигрировать root README examples.**

  Минимальный workflow использует `name`/`nodes`; composition использует
  `$ARGUMENTS`/`$INPUTS.*`; governed child и fan-out используют target
  `$node.output...`; typed artifact example использует
  `$build-index.artifacts.plan-index.path`. Не переписывать исторический
  changelog и ADR rationale.

- [x] **Step 4: Проверить пользовательский onboarding.**

  ```bash
  go test ./tests/e2e -run 'ArchonCurrentDocumentationInventory|UserJourney' -count=1
  ./bin/takt validate examples/composition/workflow.yaml --config examples/composition/config.yaml --workspace examples/composition --json >/dev/null
  ```

  Expected: current documentation содержит один target dialect, а Config/Profile
  examples сохраняют собственные `apiVersion`/`kind`.

---

## Addendum acceptance gate

- [x] Все шесть focused failing tests сначала воспроизвели соответствующий дефект.
- [x] Новые durable text fields проходят общий redactor и имеют regression test.
- [x] Quote-state tests доказывают отсутствие shell evaluation user-controlled values.
- [x] `$BASE_BRANCH` использует только durable resolved base и fail-closed при отсутствии.
- [x] Shared-session tests доказывают nearest selection и reject concurrent reuse.
- [x] Foreign promise сериализуется как `signal_ambiguous`.
- [x] JSON Schema и Go validation совпадают по размещению A1 fields.
- [x] Root README и current user docs используют только target Workflow dialect.
- [x] `TEST_RESULTS.md` обновляется только фактически выполненными командами и тестами.
- [x] После focused checks выполнены `make check`, `./scripts/verify.sh` и `git diff --check`.

В текущей review-сессии commit выполняется по прямому запросу пользователя
после verification gate; addendum считается закрытым только по фактическому
test output.
