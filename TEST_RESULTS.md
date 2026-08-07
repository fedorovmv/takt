# Takt v0.1.41-alpha — результаты проверок

Дата проверки: 2026-08-07.

## Состав среза

- Takt: `0.1.41-alpha`.
- `code` profile: `0.15.0` (не менялся).
- `code-core` BlockPackage: `0.4.0` (не менялся).
- Takt authoring skill: `0.23.0`.
- MCP: 54 операции суммарно; agent surface — 5, host — 7, worker — 13, operator — 29.
- Новый публичный SDK: `sdk/domainadapter` и `sdk/agentadapter`.
- Новый Node action: `adapter`.

## Adapter Platform P3

Проверено:

- provider-neutral домены `scm|tracker|ci` и core operation names;
- `adapter`-узел в обычном scheduler/runtime;
- process transport `takt-domain-adapter/v1alpha1`;
- MCP stdio transport с `initialize`, `tools/list`, `tools/call` и mapping операций;
- capability discovery/preflight до внешнего вызова;
- process/MCP adapter doctor через CLI;
- durable `DomainOperationState` с capabilities, idempotency key, receipt и reconcile status;
- `side_effect.mode: reconcile` с проверкой reconcile capability **до** мутации;
- `applied|not_applied|unknown`, причём `unknown` запрещает blind retry;
- fake SCM/tracker/CI adapters и provider-neutral workflow без GitHub/Jira names;
- public `sdk/agentadapter` conformance для фактического `takt-assistant/v1alpha2` transcript;
- public `sdk/domainadapter` как источник protocol types, validation и core operation constants;
- публичная agent MCP surface осталась пять `takt.task.*` tools.

Новый сквозной contract:

```text
scripts/test-adapter-platform.sh             PASS
sdk/agentadapter                            PASS
sdk/domainadapter                           PASS
```

## Исправления по ревью v0.1.40

Подтверждены regression-тестами:

- основной `docs/03-specification.md` теперь описывает внешний `side_effect` контракт;
- steering очищает parking record только после принятого replan; ошибка replanner сохраняет Failure/ParkedAt;
- `partial` verdict достижим: все required evidence прошли, но preferred checks неполны;
- Pi overflow contract test использует 5s deadline, как исправленный OpenCode twin;
- `BOUNDARY_VIOLATION` использует общую parking model вместо отдельного `failed`;
- evidence re-check заменяет запись `failed → passed`;
- surface counts 5/7/13/29 и total 54 закреплены тестом;
- claim после reconcile `unknown` отклоняется напрямую;
- обязательные `verdict.candidate_sha` и `created_at` больше не `omitempty` в Go contract.

Межпроцессный notification dispatch lock и desktop sink timeout на macOS не переписывались: это платформенные integration checks, а не дефект P3 runtime. Linux/внутрипроцессные текущие проверки сохранены.

## Go quality gates

```text
gofmt -w cmd internal sdk                 PASS
go vet ./...                              PASS
go test ./... -count=1                    PASS
go build ./...                            PASS
```

Race для всех изменённых пакетов запускался отдельно и прошёл:

```text
internal/assistant                        PASS
internal/config                           PASS
internal/workflow                         PASS
internal/domainadapter                    PASS
internal/runtime                          PASS
internal/control                          PASS
internal/mcp                              PASS
internal/store                            PASS
internal/evidence                         PASS
sdk/agentadapter                          PASS
sdk/domainadapter                         PASS
```

`go test -race ./...` внутри `make check` и два агрегированных варианта `./internal/...` достигали внешнего лимита длительной команды/зависали при многопакетном запуске в этой песочнице без сообщения о test failure. Те же затронутые пакеты, включая `internal/runtime`, отдельно проходят race стабильно. Поэтому в этом релизе **не заявляется PASS агрегированного full-repo race одной командой**; подтверждён race изменённого контура и обычный полный `go test ./...`.

## Сквозные контракты

Фактически завершились PASS отдельными прогонами:

```text
scripts/test-fake-assistant.sh                 PASS
scripts/test-pi-adapter.sh                     PASS
scripts/test-opencode-adapter.sh               PASS
scripts/test-route-dsl-e2e.sh                  PASS
scripts/test-route-dsl-eval.sh                 PASS
scripts/test-composition.sh                    PASS
scripts/test-takt-skill.sh                     PASS
scripts/test-code-profile.sh                   PASS
scripts/test-worktree.sh                       PASS
scripts/test-child-runs.sh                     PASS
scripts/test-policies.sh                       PASS
scripts/test-child-fanout.sh                   PASS
scripts/test-script-artifacts.sh               PASS
scripts/test-mcp.sh                            PASS
scripts/test-external-executor.sh              PASS
scripts/test-deep-code-workflows.sh            PASS
scripts/test-authoring.sh                      PASS
scripts/test-daemon.sh                         PASS
scripts/test-dynamic-takt.sh                   PASS
scripts/test-block-packages.sh                 PASS
scripts/test-host-control.sh                   PASS
scripts/test-host-integrations-typescript.sh   PASS
scripts/test-autonomous-runs.sh                PASS
scripts/test-simple-reliable-router.sh         PASS
scripts/test-evidence-routing.sh               PASS
scripts/test-adapter-platform.sh               PASS
scripts/check-docs.sh                          PASS
```

Длинные объединённые цепочки contract targets несколько раз достигали внешнего timeout на переходе между suites. Оставшиеся suites после этого запускались отдельно и завершились PASS.

## Schema и CLI

```text
все schemas/*.json: Draft 2020-12 structural validation   PASS
takt version                                               0.1.41-alpha
takt adapter list/doctor                                   PASS
```

Новая внешняя схема `schemas/domain-adapter-protocol.schema.json` соответствует process transport `takt-domain-adapter/v1alpha1`. `config`, `workflow`, `run-state` и `EvidenceManifest` schemas обновлены вместе с Go contracts.

## Осознанные границы

- В поставку не входят production GitHub/GitLab/Jira/корпоративные credentials и provider implementations. SDK, transports, fake adapters и E2E являются переносимой основой для них.
- MCP adapter использует stdio и локальный trusted process; сетевой integration server не добавлен.
- Capability discovery доказывает заявленную операцию, но бизнес-права конкретного provider остаются ответственностью adapter.
- Exactly-once внешних side effects не заявляется. Reconcile предотвращает blind retry и требует внешней сверки факта.
- `sdk/agentadapter` проверяет protocol/session invariants, но не превращает неподтверждённый host в strict/tool-control capable adapter. Product-specific live/fixture checks остаются обязательными.
- Bundled Pi/OpenCode host integrations сохраняют `guarded` до live smoke на зафиксированных реальных версиях.
