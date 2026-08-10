# Live Host Conformance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Проверить реальные Pi `0.83.0` и OpenCode `1.18.14` на одном Qwen 3.6 27B, закрепить fresh/resume smoke и записать точные границы bundled `guarded` host integrations.

**Architecture:** Существующие adapter opt-in tests остаются единственной автоматизируемой live-границей и расширяются до двух последовательных попыток в одной session. Host-control evidence собирается изолированно через реальные extension/plugin entrypoints; capability получает PASS только при наблюдаемом host behavior, иначе остаётся NOT VERIFIED. Production runtime меняется только после отдельной детерминированной регрессии воспроизведённого live-дефекта.

**Tech Stack:** Go `testing`, Pi RPC CLI `0.83.0`, OpenCode NDJSON CLI `1.18.14`, TypeScript host extensions, локальный `takt daemon`.

## Global Constraints

- Обычная ветка `stabilize/live-host-conformance`; worktree не создавать.
- Pi model: provider `aihub`, ID `Qwen/Qwen3.6-27B`.
- OpenCode model: provider `aihub-sbt`, ID `Qwen/Qwen3.6-27B`.
- Bundled integrations остаются `guarded`; `strict` не заявлять.
- Не добавлять новый shell test framework, dependency или публичное Workflow/Config поле.
- Live prompts выполняются в temporary workspace и не получают задачу изменять проект.
- Не сохранять credentials, resolved provider configuration или raw secret-bearing diagnostics.
- Любая production-правка следует TDD: deterministic regression RED, минимальный fix GREEN.

---

### Task 1: Pi fresh/resume live contract

**Files:**
- Modify: `internal/extensions/assistants/pi/pi_contract_test.go`

**Interfaces:**
- Consumes: `Pi.Run(context.Context, assistant.Request) (assistant.Result, error)`.
- Produces: `TestPiAdapterOptInSmoke`, подтверждающий fresh Session ID, version и exact resume.

- [x] **Step 1: Расширить opt-in test второй попыткой**

После существующих fresh assertions добавить проверку identity и resume:

```go
if result.SessionID == "" || result.AssistantVersion == "" {
	t.Fatalf("Pi smoke did not expose session/version: %+v", result)
}
freshSessionID := result.SessionID
req.Attempt = 2
req.Prompt = "Reply with exactly: TAKT_PI_RESUME_OK"
req.SessionMode = "resume"
req.SessionID = freshSessionID
resumed, err := adapter.Run(ctx, req)
if err != nil {
	t.Fatal(err)
}
if !strings.Contains(resumed.Output, "TAKT_PI_RESUME_OK") || resumed.SessionID != freshSessionID || !resumed.Resumed {
	t.Fatalf("Pi smoke did not resume exact session: fresh=%+v resumed=%+v", result, resumed)
}
```

- [x] **Step 2: Запустить live test и наблюдать фактический результат**

Run:

```bash
NODE_OPTIONS=--use-system-ca \
TAKT_PI_SMOKE=1 \
TAKT_PI_SMOKE_PROVIDER=aihub \
TAKT_PI_SMOKE_MODEL=Qwen/Qwen3.6-27B \
go test ./internal/extensions/assistants/pi -run '^TestPiAdapterOptInSmoke$' -count=1 -v
```

Expected: PASS с Pi `0.83.0`, либо FAIL на конкретной fresh/resume/version границе. `NODE_OPTIONS=--use-system-ca` нужен локальному Node для корпоративной TLS-цепочки AIHub; без него direct Pi и adapter одинаково завершаются `UNABLE_TO_VERIFY_LEAF_SIGNATURE`. При FAIL не ослаблять assertions: сохранить sanitized diagnostic, применить `superpowers:systematic-debugging`, добавить deterministic regression в `TestPiAdapterContract` и только затем менять `pi.go`.

- [x] **Step 3: Проверить deterministic Pi contract**

Run:

```bash
go test ./internal/extensions/assistants/pi -count=1
```

Expected: PASS; opt-in test SKIP без env.

- [x] **Step 4: Commit**

```bash
git add internal/extensions/assistants/pi/pi_contract_test.go
git commit -m "test: cover live Pi resume"
```

### Task 2: OpenCode fresh/resume live contract

**Files:**
- Modify: `internal/extensions/assistants/opencode/opencode_contract_test.go`

**Interfaces:**
- Consumes: `OpenCode.Run(context.Context, assistant.Request) (assistant.Result, error)`.
- Produces: `TestOpenCodeAdapterOptInSmoke`, подтверждающий fresh Session ID, version и exact resume.

- [x] **Step 1: Расширить opt-in test второй попыткой**

После существующих fresh assertions добавить:

```go
if result.AssistantVersion == "" {
	t.Fatalf("OpenCode smoke did not expose version: %+v", result)
}
freshSessionID := result.SessionID
req.Attempt = 2
req.Prompt = "Reply with exactly: TAKT_OPENCODE_RESUME_OK"
req.SessionMode = "resume"
req.SessionID = freshSessionID
resumed, err := adapter.Run(ctx, req)
if err != nil {
	t.Fatal(err)
}
if !strings.Contains(resumed.Output, "TAKT_OPENCODE_RESUME_OK") || resumed.SessionID != freshSessionID || !resumed.Resumed {
	t.Fatalf("OpenCode smoke did not resume exact session: fresh=%+v resumed=%+v", result, resumed)
}
```

- [x] **Step 2: Запустить live test на том же model family**

Run:

```bash
TAKT_OPENCODE_SMOKE=1 \
TAKT_OPENCODE_SMOKE_PROVIDER=aihub-sbt \
TAKT_OPENCODE_SMOKE_MODEL=Qwen/Qwen3.6-27B \
TAKT_OPENCODE_SMOKE_AGENT=build \
go test ./internal/extensions/assistants/opencode -run '^TestOpenCodeAdapterOptInSmoke$' -count=1 -v
```

Expected: PASS с OpenCode `1.18.14`, либо FAIL на конкретной NDJSON/session границе. При FAIL не подменять resume fresh-запуском: сохранить sanitized diagnostic, применить `superpowers:systematic-debugging`, добавить deterministic regression в `TestOpenCodeAdapterContract` и только затем менять `opencode.go`.

- [x] **Step 3: Проверить deterministic OpenCode contract**

Run:

```bash
go test ./internal/extensions/assistants/opencode -count=1
```

Expected: PASS; opt-in test SKIP без env.

- [x] **Step 4: Commit**

```bash
git add internal/extensions/assistants/opencode/opencode_contract_test.go
git commit -m "test: cover live OpenCode resume"
```

### Task 3: Реальные host entrypoints и capability evidence

**Files:**
- Modify: `TEST_RESULTS.md`
- Modify only after observed incompatibility: `integrations/coding-agent-host-control/pi/index.ts`
- Modify only after observed incompatibility: `integrations/coding-agent-host-control/opencode/index.ts`

**Interfaces:**
- Consumes: Pi `--extension`, OpenCode `OPENCODE_CONFIG_CONTENT.plugin`, `takt host.*` daemon API.
- Produces: versioned capability table with `PASS`, `FAIL` or `NOT VERIFIED` for command/input/tool/completion/recovery.

- [x] **Step 1: Проверить загрузку Pi extension на фактической версии**

Run из repository root:

```bash
pi --extension integrations/coding-agent-host-control/pi/index.ts \
  --no-context-files --no-skills --no-tools --no-session --print \
  --provider aihub --model Qwen/Qwen3.6-27B \
  "Reply with exactly: TAKT_PI_EXTENSION_LOAD_OK"
```

Expected: process загружает extension без type/API error и выводит marker. Это доказывает только extension load на Pi `0.83.0`, не blocking hooks.

- [x] **Step 2: Проверить загрузку OpenCode plugin на фактической версии**

Run с абсолютным file URL текущего `index.ts`:

```bash
TAKT_OPENCODE_PLUGIN_URL="file://$(pwd)/integrations/coding-agent-host-control/opencode/index.ts"
printf '%s\n' "Reply with exactly: TAKT_OPENCODE_PLUGIN_LOAD_OK" | \
  OPENCODE_CONFIG_CONTENT="{\"plugin\":[\"$TAKT_OPENCODE_PLUGIN_URL\"]}" \
  opencode run --format json --dir "$(pwd)" \
    --model aihub-sbt/Qwen/Qwen3.6-27B
```

Expected: NDJSON stream завершается успешно с marker и без plugin API error. Это доказывает только plugin load на OpenCode `1.18.14`.

- [x] **Step 3: Проверить command interception в disposable workspace**

Создать workspace через `mktemp -d`, выполнить `bin/takt init code --dir <workspace> --json`, механически заменить три model provider/ID в созданном `.takt/config.yaml` на `aihub-sbt` + `Qwen/Qwen3.6-27B`, добавить repository `bin` в `PATH` и запустить daemon. Затем:

- Pi: запустить TUI с `--extension <repo>/integrations/coding-agent-host-control/pi/index.ts`, отправить `/takt <bounded goal>` и убедиться, что показывается Takt preview/confirmation, а не ответ основной модели.
- OpenCode: запустить `opencode run` с plugin file URL и текстом `TAKT_HOST_BEGIN:<bounded goal>`; ожидать preview/error от Takt до model response.

Expected: command interception PASS только при наблюдаемом preview и durable host session. Temporary workspace path и host session artifacts не копировать в repository.

- [x] **Step 4: Проверить guarded boundaries без завышения результата**

На созданной active host session проверить:

- обычный последующий input получает steering/blocking response и не вызывает main model;
- unknown/mutating tool получает deny до side effect, если host предоставляет воспроизводимый trigger;
- после остановки daemon cached managed session остаётся fail-closed;
- после запуска daemon `host find` восстанавливает durable session;
- completion blocking записывается `NOT VERIFIED`, пока нет отдельного подтверждённого hook.

Expected: ни одна capability не получает PASS по source inspection или deterministic fixture; только по наблюдаемому live host event.

- [x] **Step 5: Записать sanitized evidence**

Добавить в `TEST_RESULTS.md` таблицу:

```markdown
| Host | Version | Adapter fresh | Adapter resume | Extension load | Command | Input | Tool | Recovery | Completion |
|---|---|---|---|---|---|---|---|---|---|
| Pi | 0.83.0 | PASS/FAIL | PASS/FAIL | PASS/FAIL | PASS/FAIL/NOT VERIFIED | PASS/FAIL/NOT VERIFIED | PASS/FAIL/NOT VERIFIED | PASS/FAIL/NOT VERIFIED | NOT VERIFIED |
| OpenCode | 1.18.14 | PASS/FAIL | PASS/FAIL | PASS/FAIL | PASS/FAIL/NOT VERIFIED | PASS/FAIL/NOT VERIFIED | PASS/FAIL/NOT VERIFIED | PASS/FAIL/NOT VERIFIED | NOT VERIFIED |
```

Заменить каждое значение фактическим результатом и кратко перечислить exact commands/models. Не включать raw provider configuration, credentials, Session ID или абсолютные user paths.

- [x] **Step 6: Commit evidence**

```bash
git add TEST_RESULTS.md integrations/coding-agent-host-control/pi/index.ts integrations/coding-agent-host-control/opencode/index.ts
git commit -m "docs: record live host conformance"
```

Не добавлять integration files в commit, если live run не потребовал их изменения.

### Task 4: Compatibility/status boundaries and release gate

**Files:**
- Modify: `docs/05-implementation-status.md`
- Modify: `docs/10-assistant-adapter-spec.md`
- Modify: `CHANGELOG.md`
- Modify: `MANIFEST.sha256`

**Interfaces:**
- Consumes: evidence table из Task 3.
- Produces: документация, которая не смешивает adapter smoke и strict host conformance.

- [x] **Step 1: Обновить только доказанные статусы**

Записать exact Pi/OpenCode versions и результаты fresh/resume. Bundled host status оставить `guarded`, `strict_allowed: false`; непроверенные tool/completion guarantees не повышать. Удалить gap только для capability, получившей live PASS; общий strict-host gap сохраняется до полного набора.

- [x] **Step 2: Обновить manifest**

Run:

```bash
find . -type f \
  -not -path './.git/*' \
  -not -path './bin/*' \
  -not -path './MANIFEST.sha256' \
  -print \
  | sed 's#^./##' \
  | LC_ALL=C sort \
  | while IFS= read -r manifest_path; do shasum -a 256 "$manifest_path"; done \
  > MANIFEST.sha256
```

Expected: `MANIFEST.sha256` содержит новые design/plan документы и актуальные hashes изменённых файлов.

- [x] **Step 3: Выполнить focused gates**

Run:

```bash
go test ./internal/extensions/assistants/pi ./internal/extensions/assistants/opencode ./internal/tooling/compatibility -count=1
./scripts/test-host-integrations-typescript.sh
./scripts/check-docs.sh
./scripts/verify-manifest.sh
```

Expected: PASS; opt-in live tests SKIP без env.

- [x] **Step 4: Выполнить полный release gate**

Run:

```bash
TAKT_REQUIRE_TYPESCRIPT=1 TSC=/tmp/takt-ts.UIqpa0/node_modules/.bin/tsc make check
TAKT_REQUIRE_TYPESCRIPT=1 TSC=/tmp/takt-ts.UIqpa0/node_modules/.bin/tsc ./scripts/verify.sh
```

Expected: оба завершатся PASS, включая race, E2E, TypeScript, docs и manifest.

- [x] **Step 5: Проверить отсутствие live artifacts/secrets**

Run:

```bash
git status --short
git diff --check
git diff --no-ext-diff -- TEST_RESULTS.md docs/05-implementation-status.md docs/10-assistant-adapter-spec.md \
  | rg -n '(apiKey\s*[:=]|Bearer\s+[A-Za-z0-9]{20,}|sk-[A-Za-z0-9]{20,})'
```

Expected: только запланированные tracked changes до commit; secret scan не выводит совпадений и завершается с exit code `1` от отсутствия matches.

- [x] **Step 6: Commit**

```bash
git add docs/05-implementation-status.md docs/10-assistant-adapter-spec.md CHANGELOG.md MANIFEST.sha256
git commit -m "docs: record guarded host evidence"
```
