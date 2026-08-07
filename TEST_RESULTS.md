# Takt v0.1.42-alpha — результаты проверок

Дата проверки: 2026-08-07.

## Состав среза

- Takt: `0.1.42-alpha`.
- `code` profile: `0.15.0` (формат профиля не менялся).
- `code-core` BlockPackage: `0.4.0` (содержимое не менялось).
- Takt authoring skill: `0.24.0`.
- Публичная agent MCP surface по-прежнему содержит пять `takt.task.*` tools.
- Новый основной срез: Portable Package Distribution поверх существующего `BlockPackage`.

## Portable Package Distribution P4

Проверено:

- установка `BlockPackage` из local и Git source без нового workflow/runtime формата;
- области `global|corporate|project` и разрешение блоков `project > corporate > global > builtin`;
- fail-closed объединение governance независимо от precedence реализации блока;
- project/corporate lock `<workspace>/.takt/takt.lock.json` и global lock `~/.takt/takt.lock.json`;
- фиксация version/source/ref/Git commit/SHA-256 и сведений о проверенной подписи;
- автоматическое подключение locked packages через `profile.Resolve` без ручного `block_packages`;
- fail-closed integrity/policy validation locked package до его попадания в Run;
- `takt package install|update|uninstall|list|sync|doctor|sign`;
- staged transactional install/update с rollback каталога при ошибке сохранения lock;
- зависимости пакетов и version constraints: exact, `>=`, `^`, `*`;
- запрет удаления пакета, который нужен установленному dependent package;
- package requirements к версии Takt и adapter capabilities;
- required adapter capability preflight до Run и preferred capability status для Router/Planner;
- local/Git source policy и Ed25519 signature policy для выбранных scopes;
- перенос полного package tree: workflows, commands, scripts, path skills и MCP config;
- рабочий пример `examples/portable-package/`;
- package distribution E2E с install → automatic catalog → corruption detection → sync repair → uninstall.

Сквозной новый контракт:

```text
scripts/test-package-distribution.sh          PASS
```

## Исправления по ревью v0.1.41

Подтверждены кодом и постоянными тестами:

1. `sdk/agentadapter` conformance kit теперь реально используется внутри репозитория:
   - `cmd/takt-fake-assistant` для `v1alpha2` использует публичные SDK validators;
   - process contract захватывает stdout реального fake wrapper и прогоняет его через `ValidateTranscript`.
2. Канонические fixtures добавлены в `sdk/agentadapter/testdata/v1alpha2/` и используются Go tests; документация указывает их как переносимый набор для реализаций на других языках.
3. В документации явно зафиксировано ограничение transcript-kit: он не видит OS process exit status. Согласованность OS exit code и `result.exit_code` проверяет host/process contract.
4. `takt adapter doctor` больше не является alias `describe`: doctor дополнительно проверяет configured operation/reconcile mappings; `list|describe|doctor` имеют CLI regression tests.
5. `sdk/agentadapter` получил публичные request/result/envelope types и validators, включая `attempt >= 1`, обязательные идентификаторы и ограничения policy.
6. Расширено branch coverage обоих публичных SDK: transcript/request/result/declaration/tool-control и domain operation/declaration/result/reconcile validation.
7. Reconcile preflight в документации ограничен фактической семантикой `side_effect.mode: reconcile` / явными reconcile requirements.
8. Добавлены прямые regression tests:
   - cancel активного adapter-node;
   - pause на границе adapter-node;
   - process transport `max_output_bytes` overflow;
   - `BOUNDARY_VIOLATION` через production DenyReason path;
   - evidence re-check `failed → passed` с assert итогового verdict.

Дополнительно race-прогон обнаружил и закрыл новый lifecycle-флак `takt-assistant/v1alpha2`: `StderrPipe` теперь дочитывается до `cmd.Wait()`, поэтому `os/exec` не закрывает pipe под активным reader-ом.

## Go quality gates

Подтверждены:

```text
gofmt -w cmd internal sdk                 PASS
go vet ./...                              PASS
go build ./...                            PASS
```

Обычные Go tests запущены по всем 39 пакетам отдельно с собственным timeout; все пакеты завершились PASS / `[no test files]`. Это эквивалентно покрытию `go test ./... -count=1`, но устойчивее в текущей песочнице, где длинный агрегированный процесс иногда не возвращает управление после уже завершившихся дочерних пакетов.

Race отдельно подтверждён для всего изменённого контура:

```text
internal/packagedist                     PASS
internal/profile                         PASS
internal/blockcatalog                    PASS
internal/control                         PASS
internal/runtime                         PASS
internal/domainadapter                   PASS
internal/assistant                       PASS
sdk/agentadapter                         PASS
sdk/domainadapter                        PASS
cmd/takt                                 PASS
```

`internal/runtime` и `internal/assistant` прогонялись отдельно с `-race` и завершились полным PASS, включая Pi/OpenCode contract cases. Один агрегированный `make check` дошёл до `go test -race ./...` после успешных fmt/vet/unit и был остановлен внешним лимитом команды через 1000 секунд без test failure. Поэтому релиз **не заявляет PASS одной агрегированной команды `go test -race ./...`**; заявляется PASS race всего изменённого контура отдельными прогонами.

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
scripts/test-package-distribution.sh            PASS
sdk/agentadapter conformance                    PASS
scripts/check-docs.sh                           PASS
```

Pi/OpenCode scripts включают собственные race-прогоны и завершились PASS. Некоторые длинные оболочки команд в этой среде возвращали внешний timeout, пока дочерний `go test -race` ещё работал; проверка process/log после завершения подтверждала PASS соответствующего suite. Для остальных контрактов использовались отдельные команды с собственным timeout.

## Schemas и поставка

Перед упаковкой:

```text
все schemas/*.json синтаксически валидны          PASS
schemas/package-lock.schema.json                  present
schemas/package-policy.schema.json                present
schemas/package-signature.schema.json             present
BlockPackage schema dependencies/requirements     present
VERSION                                             0.1.42-alpha
skill VERSION                                       0.24.0
```

`docs/56-portable-package-distribution-v0.1.42.md`, ADR-066/067, основная спецификация, roadmap, README, changelog и document map согласованы с реализацией; `scripts/check-docs.sh` — PASS.

## Осознанные границы

- Центральный package registry/server не добавлен: `v0.1.42` использует local и Git sources.
- Автоматическое скачивание dependency packages не выполняется; зависимость устанавливается явно и затем операция повторяется.
- Production GitHub/GitLab/Jira/корпоративные adapters не входят в ядро; они остаются отдельными реализациями поверх `sdk/domainadapter`.
- Public `sdk/agentadapter` теперь даёт типы, validators, fixtures и conformance harness, но product-specific live smoke для реальных Codex/Qwen/Oh My Pi wrappers остаётся задачей конкретного адаптера.
- Подпись Ed25519 опциональна и становится обязательной только для scopes, указанных в package policy.
- Multi-repo dynamic workflow остаётся следующим крупным продуктовым срезом.
