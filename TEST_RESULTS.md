# Test results — v0.1.29-alpha

Дата проверки: 2026-08-05.

## Среда

- Linux x86_64, kernel `6.18.35`;
- Go `1.23.2`;
- GNU Bash `5.2.37`;
- штатные fake assistants для process, Pi и OpenCode contracts;
- Python 3, Node и Go использовались в unit-тестах script runtimes;
- реальный macOS runner в этой среде недоступен.

В CI сохраняется матрица `ubuntu-latest` / `macos-latest`. Фактический macOS PASS в этом локальном отчёте не заявляется.

## Script nodes и typed artifacts

| Контракт | Результат |
|---|---|
| runtime `command` | PASS |
| inline Python | PASS |
| inline Node | PASS |
| Go source через `go run` | PASS |
| raw stdout сохраняется при `output_format` | PASS |
| structured Output нормализуется | PASS |
| fingerprint script source | PASS |
| fingerprint declared dependency | PASS |
| изменение dependency блокирует resume | PASS |
| output artifact из stdout | PASS |
| file artifact из `output_path` | PASS |
| MIME, SHA-256, size и producer metadata | PASS |
| artifact source path confinement | PASS |
| шаблоны `nodes.<id>.artifacts.<type>.*` | PASS |
| child artifact доступен родителю | PASS |
| fan-out item сохраняет artifacts | PASS |
| CLI `takt artifacts` и фильтры | PASS |
| профиль `code` использует script и plan/PRD artifacts | PASS |

## Выполненные команды

```text
gofmt -w cmd internal                         PASS
go vet ./...                                  PASS
go test ./... -count=1                        PASS
go test -race ./... -count=1                  PASS
python3 -m json.tool schemas/*.json           PASS
scripts/test-fake-assistant.sh                PASS
scripts/test-pi-adapter.sh                    PASS
scripts/test-opencode-adapter.sh              PASS
scripts/test-route-dsl-e2e.sh                 PASS
scripts/test-route-dsl-eval.sh                PASS
scripts/test-composition.sh                    PASS
scripts/test-takt-skill.sh                    PASS
scripts/test-code-profile.sh                  PASS
scripts/test-worktree.sh                      PASS
scripts/test-child-runs.sh                    PASS
scripts/test-policies.sh                      PASS
scripts/test-child-fanout.sh                  PASS
scripts/test-script-artifacts.sh              PASS
scripts/check-docs.sh                         PASS
scripts/verify.sh                             PASS
example validation set                       PASS
```

`make check` в одном foreground-вызове был остановлен внешним лимитом инструмента на этапе Pi contracts после успешных unit/race и fake-assistant checks. Все оставшиеся команды были выполнены отдельно без изменения дерева и завершились успешно. После этого полный `scripts/verify.sh` был запущен как один процесс с журналом и завершился с кодом `0` и строкой `verification: PASS`.

## Что не проверялось

- реальные обращения к Pi, OpenCode и внешним моделям;
- реальные GitHub/Remotion операции профиля `code`;
- фактическая CI job на macOS;
- установка Python/Node dependencies — Takt их намеренно не выполняет;
- внешний object storage и secret redaction артефактов;
- системный OS sandbox — текущая filesystem/network policy остаётся assistant-enforced.

## Проверка поставляемого архива

Итоговый ZIP распакован в новый пустой каталог `/mnt/data/takt-final-clean-029/takt`. Непосредственно из распаковки прошли:

```text
sha256sum -c MANIFEST.sha256                  PASS
VERSION / profile VERSION / skill VERSION     PASS
scripts/verify.sh                             PASS
verification: PASS                            PASS
exit code                                     0
```

Полный чистый прогон включал unit/race, fake/Pi/OpenCode, Route DSL, composition, skill/profile, worktree, child-run, policy, fan-out, script/artifact, documentation и examples. Финальная фиксация этого отчёта не меняет исходный код, схемы, workflow или тестовые сценарии; после неё архив и манифест пересобираются и тот же чистый шлюз выполняется повторно.
