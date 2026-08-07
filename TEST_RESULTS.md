# Takt v0.1.43-alpha — результаты проверок

Дата проверки: 2026-08-07.

## Состав релиза

- Takt: `0.1.43-alpha`.
- `code` profile: `0.16.0`.
- `code-core` BlockPackage: `0.5.0`.
- Takt authoring skill: `0.25.0`.
- Публичная agent MCP surface сохраняет пять `takt.task.*` tools.
- Основной срез: Multi-repo Dynamic Workflows поверх существующего `WorkflowPlan -> Workflow -> scheduler -> governed child Run`.

## Multi-repo Dynamic Workflows

Проверено:

- `.takt/workspace.yaml` с `takt/v1alpha1 Workspace`, явными repository IDs, paths и `depends_on`;
- bounded auto-discovery root Git repository либо immediate child repositories;
- обязательные `apiVersion`/`kind` для явного manifest;
- repository ID contract `[a-z][a-z0-9-]{0,62}` и нормализация auto-discovery IDs;
- запрет lexical path escape и escape через symlink после `EvalSymlinks`;
- проверка Git roots, уникальности IDs и ацикличности repository dependency graph;
- durable workspace-catalog fingerprint по repositories/dependencies/HEAD;
- доставка repository catalog и adapter preflight в semantic Router, Planner и Replanner;
- `WorkflowPlan.phase.repository` и `publish_change`;
- проверка, что repository dependency graph отражён phase dependencies;
- один mutating owner phase на repository в первом multi-repo контракте;
- компиляция repository phase в обычный governed child Run, без второго scheduler/runtime;
- отдельный managed worktree на изменяемый repository;
- сохранение child run ID/control workspace/execution workspace/branch/base commit;
- cross-repository TaskBrief context с результатами и явными workspace/branch/base references;
- per-repository candidate SHA-256 и `EvidenceManifest`;
- plan-level candidate SHA из repository candidate fingerprints;
- multi-repo changed-files integrity gate по реальному Git diff с namespace `<repo-id>/<path>`;
- `publish_change` через нейтральный `scm/change.create` adapter с существующими idempotency/reconcile semantics;
- deterministic merge order из dependency graph;
- read-only `integration-verify` после repository phases;
- `PendingPhases`/checkpoint replanning только незавершённого хвоста с сохранением `repository_executions`;
- удержание repository worktrees до завершения parent plan для интеграционной проверки и evidence.

Сквозной release-контракт создаёт три настоящих локальных Git repository `api -> client -> service`, строит semantic multi-repo plan, выполняет три изолированных repository child Runs, публикует три fake neutral SCM changes, запускает integration verification, проверяет per-repository и общий evidence и подтверждает, что базовые checkout не изменились:

```text
scripts/test-multi-repo.sh                 PASS
```

## Исправления по ревью v0.1.42

Закрыты и закреплены постоянными тестами:

1. Security-negative package tests:
   - source вне `allowed_sources`;
   - tampered content после подписи;
   - untrusted signing key;
   - missing signature при `required`;
   - несовместимый `requirements.takt`;
   - local source drift при `sync`.
2. Local source allowlist использует реальную path boundary, а не строковый prefix: `/opt/takt/packages` не разрешает `/opt/takt/packages-evil`.
3. `examples/portable-package/` устанавливается и реально выполняется из locked copy; E2E проверяет bundled command/script/skill/MCP resolution.
4. Lock-write injection добавлен; rollback установленного каталога при ошибке сохранения lock покрыт прямым тестом.
5. Caret-version semantics уточнена для `0.x`: `^0.1.42` не принимает `0.2.0`; `^0.0.3` не принимает `0.0.4`.
6. Package dependency resolution учитывает scope; неоднозначное имя без scope fail-closed.
7. `takt adapter doctor` возвращает non-zero exit при `status: error`.
8. Process-adapter error paths дочитывают stderr до `Wait()`, исключая тот же pipe lifecycle race на ошибочных ветках.
9. Добавлены regression tests для:
   - adapter preflight в Router/Planner payloads;
   - duplicate conformance `call_id`;
   - update, ломающего установленный dependent package;
   - Git sync точно на locked commit;
   - package CLI lifecycle через `packageCmd`.

Дополнительно multi-repo preflight ужесточён: manifest repository path проверяется после symlink resolution до запуска child Run, чтобы security boundary совпадала между plan/preflight и runtime.

## Go quality gates

Финальный working tree после всех изменений:

```text
gofmt -w cmd internal sdk                 PASS
go vet ./...                              PASS
go test ./... -count=1                    PASS (40 packages)
go build ./...                            PASS
schemas/*.json parse                      PASS (20 schemas)
```

Race подтверждён для всего изменённого и критичного контура:

```text
internal/workspacecatalog                 PASS
internal/dynamicplan                      PASS
internal/control                          PASS
internal/runtime                          PASS
internal/packagedist                      PASS
internal/assistant                        PASS
cmd/takt                                  PASS
sdk/agentadapter                          PASS
sdk/domainadapter                         PASS
```

На более раннем полном regression pass остальные `internal/...` пакеты также завершились под `-race` без FAIL. Единый агрегированный `go test -race ./...`/`make check` в этой песочнице периодически не возвращает управление после уже завершившихся пакетов и был остановлен внешним лимитом команды без test failure. Поэтому релиз не заявляет PASS одной агрегированной race-команды; изменённые и критичные пакеты проверены отдельными race-прогонами.

## Сквозные контракты

В ходе regression gate завершились PASS:

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
scripts/test-simple-reliable-router.sh          PASS
scripts/test-evidence-routing.sh               PASS
scripts/test-adapter-platform.sh               PASS
scripts/test-package-distribution.sh            PASS
scripts/test-multi-repo.sh                     PASS
scripts/check-docs.sh                          PASS
```

После финального workspace security-hardening повторно прошли наиболее затронутые контракты:

```text
scripts/test-multi-repo.sh                     PASS
scripts/test-package-distribution.sh            PASS
scripts/test-adapter-platform.sh               PASS
scripts/test-dynamic-takt.sh                   PASS
scripts/test-code-profile.sh                   PASS
scripts/test-block-packages.sh                 PASS
scripts/test-evidence-routing.sh               PASS
scripts/test-opencode-adapter.sh               PASS
scripts/check-docs.sh                          PASS
```

## Известная граница v0.1.43

Первый multi-repo срез намеренно ограничен локально доступными Git repositories в одном workspace. Он не добавляет distributed workers, автоматическое merge change requests, удалённый mass checkout или отдельный dependency resolver. Один repository пока имеет не более одного mutating owner phase; это сохраняет однозначное владение candidate worktree и evidence.
