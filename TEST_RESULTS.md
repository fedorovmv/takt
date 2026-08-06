# Takt v0.1.40-alpha — результаты проверок

Дата проверки: 2026-08-07.

## Состав среза

- Takt: `0.1.40-alpha`.
- `code` profile: `0.15.0`.
- `code-core` BlockPackage: `0.4.0`.
- Takt authoring skill: `0.22.0`.
- MCP: 54 операции суммарно; agent surface — 5, host — 7, worker — 13, operator — 29.
- Новый worker tool: `takt.node.reconcile`.

## Основные изменения, проверенные в этом релизе

- внутренний `EvidenceManifest` с baseline, check-to-evidence mapping и candidate content SHA-256;
- stale verdict invalidation после изменения candidate;
- exact normalized classification известных baseline failures без лишнего automatic repair;
- `parked` Dynamic Plan с failure code, owner, `safe_next_action` и attention projection;
- `executor: external` + `side_effect.mode: reconcile`, запрещающий blind retry после неизвестного исхода;
- reconciliation outcomes `unknown|not_applied|applied`, причём `applied` требует receipt и проходит обычный submit/finalization path;
- постоянные regressions для замечаний ревью v0.1.39;
- стабилизация старых OpenCode timeout contract tests под полной race-нагрузкой.

## Go quality gates

```text
gofmt -w cmd internal                 PASS
go vet ./...                          PASS
go test ./... -count=1                PASS
go build ./...                        PASS
go test -race ./... -count=1          PASS
```

`go test -race ./...` дважды запускался отдельно после того, как длинная объединённая shell-команда достигала внешнего timeout. Оба отдельных полных race-прогона завершились PASS. В частности, прежние flaky tests `TestOpenCodeRunPreservesContextPriorityWithRealOverflow` и `TestOpenCodeTimeoutPreservesProviderDiagnostics` прошли в общем race-прогоне после увеличения их тестового deadline.

При первой проверке чистой распаковки проявилась отдельная lifecycle-гонка самого теста `TestHostRunAndNotificationToolsThroughMCP`: detached `host.confirm` продолжал писать state после завершения unit-test без daemon monitor. Тест исправлен так, чтобы явно прогонять `AdvanceDynamicPlans` до устойчивой границы; после исправления проблемный тест прошёл 20/20, затем полный unit/race suite снова прошёл.

## Новый P2 contract

```text
scripts/test-evidence-routing.sh              PASS
```

Он постоянно проверяет:

- baseline-only failure classification;
- обе terminal-ветки bounded automatic repair;
- candidate SHA изменение от tracked/untracked content;
- parking и Task/attention projection;
- external side-effect reconciliation;
- side-effect workflow validation;
- lexeme matching `auth != author`, `bug != debug`.

Дополнительные Go-регрессии покрывают:

- pause re-check перед новой retry-attempt;
- настоящий `Waiting.Kind=question` для `capture_response`;
- ошибки `ClearPause`, `ClearCancel`, release advance lock;
- transient recursive summary для linked-but-not-published child;
- plan-fork fingerprint;
- notification dispatch lock, bounded inbox и desktop timeout;
- `task start --file` semantics;
- default MCP surface для пустого значения;
- direct `takt run` capability preflight до создания Run;
- persisted `RouterError` и propagation `context.Canceled`.

## Сквозные контракты

```text
scripts/test-simple-reliable-router.sh         PASS
scripts/test-autonomous-runs.sh                PASS
scripts/test-host-control.sh                   PASS
scripts/test-host-integrations-typescript.sh   PASS
scripts/test-mcp.sh                            PASS
scripts/test-daemon.sh                         PASS ×4
scripts/test-dynamic-takt.sh                   PASS
scripts/test-block-packages.sh                 PASS
scripts/test-external-executor.sh              PASS
scripts/test-deep-code-workflows.sh            PASS
scripts/test-code-profile.sh                   PASS
scripts/test-composition.sh                    PASS
scripts/test-worktree.sh                       PASS
scripts/test-child-runs.sh                     PASS
scripts/test-child-fanout.sh                   PASS
scripts/test-policies.sh                       PASS
scripts/test-script-artifacts.sh               PASS
scripts/test-authoring.sh                      PASS
scripts/test-fake-assistant.sh                 PASS
scripts/test-pi-adapter.sh                     PASS
scripts/test-opencode-adapter.sh               PASS
scripts/test-route-dsl-e2e.sh                  PASS
scripts/test-route-dsl-eval.sh                 PASS
scripts/test-takt-skill.sh                     PASS
scripts/check-docs.sh                          PASS
```

Один длинный агрегированный запуск группы contract scripts достиг внешнего timeout во время OpenCode suite после успешного прохождения предыдущих тестов. OpenCode suite и оставшиеся contracts затем запускались отдельно и завершились PASS.

## Проверенная семантика MCP

- default `takt mcp` = agent surface;
- agent surface: только `takt.task.start|status|respond|stop|explain`;
- worker surface включает `takt.node.reconcile`;
- `tools/call` проверяет surface, а не только `tools/list`;
- полная surface содержит 54 операции.

## Ограничения, которые не следует трактовать как реализованные гарантии

- Candidate SHA является content SHA-256 рабочей Git-дельты и untracked files, а не Git commit SHA и не supply-chain attestation.
- Baseline matching детерминированный и точный после нормализации; семантически похожие, но текстово разные failures считаются новыми.
- `parked` является состоянием Dynamic Plan; Run-level pause/abandon сохраняют отдельную существующую семантику.
- External reconciliation не знает GitHub/GitLab/tracker/CI самостоятельно. Adapter обязан проверить внешний факт и предоставить receipt/result.
- Exactly-once внешних side effects не заявляется.
- Path-level OS sandbox для mutating coding-agent workers ещё не реализован; Takt применяет managed worktree, adapter policy и post-action Git-diff scope gate.
- Bundled Pi/OpenCode host integrations остаются `guarded` до live smoke на зафиксированных реальных версиях.
