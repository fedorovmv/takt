# Test results — v0.1.49-alpha

Дата: 2026-08-08.

Среда этого прогона: Linux amd64, Go 1.23.2. Фактический macOS-прогон этой поставки в текущей среде не выполнялся; macOS-specific regressions остаются в suite и не выдаются за live macOS verification.

## Release scope

`v0.1.49-alpha — Reference External Adapters` начинает P2 External seams и проверяет два публичных extension contracts реальными reference implementations, не добавляя provider-specific логику в scheduler/runtime:

- `cmd/qwen-takt-adapter` / `reference/qwencode` реализуют внешний Qwen Code wrapper через public `sdk/agentadapter` и `takt-assistant/v1alpha2`;
- `cmd/takt-github-scm-adapter` / `reference/githubscm` реализуют neutral SCM contract через public `sdk/domainadapter` и `gh`;
- public domain `InvokeRequest`/`ReconcileRequest` получают execution `workspace` и request validators;
- process и MCP domain transports используют одинаковый request validation/cwd contract;
- multi-repo publication передаёт candidate child worktree как `repository_workspace`;
- process-v1alpha2 больше не выводит `tool_control` и другие lifecycle guarantees только из версии протокола: configured capabilities должны подтверждаться фактической stream declaration wrapper-а;
- reference implementations и binaries не импортируют `takt/internal/*`;
- roadmap/backlog/status переводят P2 из planned в started: reference external wrapper и SCM adapter закрыты, live host conformance и structured task source adapter остаются следующими seams.

## Qwen Code reference wrapper

Wrapper запускает Qwen Code в headless режиме:

```text
qwen --prompt <prompt>
     --output-format stream-json
     --safe-mode
     --approval-mode yolo
     --model <model-id>
     [--resume <session-id>]
     [--max-wall-time <duration>]
```

и нормализует stream в `takt-assistant/v1alpha2`:

- capability declaration;
- `session.started|session.resumed`;
- assistant `message`;
- `usage`;
- `diagnostic`;
- terminal `completed|failed` result.

Reference capability surface намеренно ограничен:

```text
agent_events_v2
session_events
usage_events
```

Wrapper fail-closed отклоняет effective policy, требующую Takt tool restrictions, selected skills, MCP projection, filesystem/network sandbox или другие capabilities, которых он не может доказать. `--safe-mode + --approval-mode yolo` поэтому разрешены только для trusted execution workspace и unrestricted node policy; wrapper не заявляет `tool_control`.

Unit/contract tests подтверждают fresh и exact resume, model/timeout flags, public conformance, unsupported-policy fail-closed и отсутствие `internal/` imports.

Live Qwen model/credential в release gate не используется. Актуальные headless flags были сверены с официальной Qwen Code documentation 2026-08-08; correctness wrapper-а доказывается deterministic fake upstream stream.

## GitHub SCM reference adapter

Reference adapter публикует neutral SCM operations:

```text
repository.get
change.get
change.create
change.comment
change.review
checks.get
```

и reconcile capabilities для:

```text
change.create
change.comment
change.review
```

Repository identity выводится из `repository_workspace`, существующего path относительно execution workspace, Git `origin`, явного `[HOST/]OWNER/REPO` или `GH_REPO`. Provider-specific `gh` commands остаются внутри adapter.

Mutating operations используют SHA-256-derived marker:

```text
<!-- takt-idempotency:<sha256> -->
```

Raw Takt idempotency key наружу не публикуется. После ambiguous transport failure reconcile сначала ищет внешний факт по receipt/marker и возвращает `applied|not_applied|unknown`, поэтому runtime не повторяет mutation до доказанного `not_applied`.

Tests отдельно подтверждают:

- public declaration содержит все SCM core operations;
- execution `repository_workspace` и Git remote действительно определяют cwd/GH_REPO;
- `change.create` ambiguous failure → reconcile existing PR → `applied`, без второго create;
- `change.comment` и `change.review` используют тот же hashed-marker reconciliation;
- `checks.get` принимает официальный `gh pr checks` exit code 8 как pending data;
- unknown operation input fields fail-closed;
- local relative path имеет приоритет над лексически похожим `owner/repo` slug;
- production reference source не импортирует `takt/internal/*`.

Настоящий GitHub mutation с credentials в release gate не выполнялся. E2E использует deterministic fake `gh`, но вызывает настоящий adapter binary через обычный Takt domain process transport.

## Public SDK / transport seams

### Agent process v1alpha2

Static process preflight теперь консервативен. `protocol: takt-assistant/v1alpha2` означает возможность transport-а передавать v2 records, но сам по себе не означает `tool_control`, `tool_events`, `artifact_events` или другие guarantees.

Runtime проверяет фактическую первую stream declaration:

- declaration должна идти до events/result;
- declaration проходит public `sdk/agentadapter.ValidateDeclaration`;
- каждая configured capability должна быть подтверждена wrapper-ом;
- event должен входить в declared `event_types`, если список объявлен;
- `tool.request` допустим только при фактическом `tool_control: true`.

Regression tests подтверждают отсутствие inference `tool_control` из protocol version, mismatch configured/stream capabilities и reject tool request без tool control.

### Domain requests

`InvokeRequest`/`ReconcileRequest` получили provider-neutral `workspace` и public validators:

- `run_id` / `node_id` required;
- `attempt >= 1` для invoke;
- valid domain/operation;
- side-effect mode и idempotency invariants;
- nonblank workspace, если он передан.

Process и MCP transports вызывают тот же validator до provider и запускают provider process в request workspace. Unit tests подтверждают cwd для обоих transports.

## Ordinary Go gate

На финальной рабочей копии завершились:

```text
gofmt -d cmd internal sdk reference       PASS
go vet ./...                              PASS
go test ./... -count=1                    PASS — 49 packages total / 36 with tests
go build ./...                            PASS
31 JSON schemas syntax                    PASS
scripts/check-docs.sh                     PASS
```

Первый schema syntax gate запускал отдельный `python -m json.tool` на каждый файл и был остановлен внешним лимитом после старта этого этапа. Та же проверка затем выполнена одним Python process через `json.load` для всех 31 schema и завершилась PASS.

## Race detector

Изменённый и непосредственно затронутый контур завершился под `-race`:

```text
./reference/qwencode                      PASS
./reference/githubscm                     PASS
./sdk/agentadapter                        PASS
./sdk/domainadapter                       PASS
./internal/assistant                      PASS
./internal/domainadapter                  PASS
./internal/runtime                        PASS
./internal/dynamicplan                    PASS
./internal/compatibility                  PASS
./cmd/takt                                PASS
```

Несколько batch-команд инструментальной оболочки были остановлены между пакетами; каждый незавершившийся пакет после этого запускался отдельно и завершился PASS.

Агрегированный `go test -race ./... -count=1` также запускался. Он завершил все пакеты от `cmd/takt` до `internal/rolecontract` без test FAIL, после чего был остановлен внешним лимитом. Оставшийся хвост (`runtime`, store/workflow/workspace, reference packages и SDK) затем был прогнан отдельно под `-race` и завершился PASS. Поэтому один непрерывный aggregate race PASS не заявляется.

## Reference adapter E2E

`scripts/test-reference-adapters.sh`: PASS.

Сценарий Qwen:

```text
Takt process-v1alpha2
→ qwen-takt-adapter
→ fake upstream qwen stream-json
→ public declaration/events/result
→ completed Run
```

Проверяются `--safe-mode`, `--output-format stream-json` и успешная нормализация через настоящий Takt process host.

Сценарий GitHub:

```text
adapter node
→ takt-github-scm-adapter
→ fake gh pr create
→ external fact created, transport error
→ runtime sees unknown
→ reconcile pr list finds hashed marker
→ applied
→ completed Run
```

`pr create` вызывается ровно один раз, raw idempotency key отсутствует во внешнем body.

## Contract / E2E regressions

Отдельными завершившимися прогонами подтверждены:

```text
test-fake-assistant.sh                  PASS
test-pi-adapter.sh                      PASS
test-opencode-adapter.sh                PASS
test-route-dsl-e2e.sh                   PASS
test-route-dsl-eval.sh                  PASS
test-composition.sh                     PASS
test-takt-skill.sh                      PASS
test-code-profile.sh                    PASS
test-worktree.sh                        PASS
test-child-runs.sh                      PASS
test-policies.sh                        PASS
test-child-fanout.sh                    PASS
test-script-artifacts.sh                PASS
test-mcp.sh                             PASS
test-external-executor.sh               PASS
test-deep-code-workflows.sh             PASS
test-authoring.sh                       PASS
test-daemon.sh                          PASS
test-dynamic-takt.sh                    PASS
test-block-packages.sh                  PASS
test-host-control.sh                    PASS
test-host-integrations-typescript.sh    PASS
test-autonomous-runs.sh                 PASS
test-simple-reliable-router.sh          PASS
test-evidence-routing.sh                PASS
test-adapter-platform.sh                PASS
test-package-distribution.sh            PASS after fixture declaration fix
test-multi-repo.sh                      PASS
test-runtime-reliability-security.sh    PASS
test-iteration-history.sh               PASS
test-compatibility.sh                    PASS
test-reference-adapters.sh              PASS
test-route-dsl-benchmark.sh             PASS
test-task-evaluation.sh                 PASS
scripts/check-docs.sh                    PASS
```

`test-package-distribution.sh` сначала обнаружил реальную fixture incompatibility после ужесточения capability negotiation: fake-assistant config требовал `mcp`, а его v1alpha2 stream declaration заявляла только `skills/tool_control`. Runtime корректно отклонил этот mismatch. Fixture declaration была исправлена так, чтобы перечислять фактически поддерживаемые test capabilities; после этого package-distribution contract завершился PASS. Runtime-проверка не ослаблялась.

Две длинные группы contract scripts были помечены внешним timeout уже после того, как все входившие в них scripts напечатали PASS. Эти wrapper timeouts не считаются test PASS сами по себе; в таблицу выше включены только фактические PASS каждого script.

## Production evidence boundary

`v0.1.49` доказывает публичные extension seams, но не подменяет live provider evidence:

- Qwen reference wrapper проверен против deterministic stream-json fixture, без реальной модели/credential;
- GitHub SCM adapter проверен через deterministic `gh` fixture, без настоящей mutation;
- bundled Pi/OpenCode host integrations остаются `guarded` до live conformance на pinned host versions;
- production Route DSL/Go/Document evidence остаётся P0 gate перед финальным `v1beta1` freeze.

Следующий открытый P2 seam — structured task source adapter.

## Clean archive verification

Первый release ZIP был собран из отдельного staging без `bin/`, распакован в новый каталог и проверен как фактическая поставка:

```text
MANIFEST.sha256                         573 files — PASS
bin/                                    absent in archive
VERSION                                 0.1.49-alpha
skills/takt/VERSION                     0.31.0
gofmt -d cmd internal sdk reference     PASS
go vet ./...                            PASS
go test ./... -count=1                  PASS
go build ./...                          PASS
31 JSON schemas                         PASS
scripts/test-reference-adapters.sh      PASS
scripts/test-adapter-platform.sh        PASS
scripts/test-package-distribution.sh    PASS
scripts/test-multi-repo.sh              PASS
scripts/test-runtime-reliability-security.sh PASS
scripts/test-compatibility.sh           PASS
scripts/test-route-dsl-benchmark.sh     PASS
scripts/test-task-evaluation.sh         PASS
scripts/check-docs.sh                   PASS
```

Для запуска contract scripts в каталоге распаковки были собраны временные `bin/takt`/fake binaries; это произошло **после** проверки отсутствия `bin/` и не меняет содержимое release ZIP.

Clean-archive race verification:

```text
./reference/qwencode                    PASS
./reference/githubscm                   PASS
./sdk/agentadapter                      PASS
./sdk/domainadapter                     PASS
./internal/assistant                    PASS
./internal/domainadapter                PASS
./internal/runtime                      PASS
./internal/dynamicplan                  PASS
./internal/compatibility                PASS
./cmd/takt                              PASS
```

Первая объединённая clean-race команда была остановлена инструментальной оболочкой после PASS `internal/assistant`; оставшиеся пакеты запущены второй командой из той же распаковки и завершились PASS.

После этой clean verification исходный код больше не меняется. В финальном пересборочном проходе обновляются только данный report, `MANIFEST.sha256` и archive checksum, после чего manifest финального ZIP проверяется ещё раз.
