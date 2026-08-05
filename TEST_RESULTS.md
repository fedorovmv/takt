# Test results — v0.1.28-alpha

Дата проверки: 2026-08-05.

## Среда

- Linux x86_64;
- Go toolchain из текущего окружения;
- штатные fake assistants для process, Pi и OpenCode contracts;
- реальный macOS runner в этой среде недоступен.

В CI сохранена матрица `ubuntu-latest` / `macos-latest`. Исправленный `scripts/test-worktree.sh` не использует `readarray`/`mapfile` и синтаксически совместим с Bash 3.2. Фактический macOS PASS должен подтверждаться соответствующей CI job; в этом отчёте он не заявляется.

## Исправления замечаний

| Проверка | Результат |
|---|---|
| `scripts/test-worktree.sh` без `readarray` | PASS |
| OpenCode `read_only` запрещает явно разрешённый `write` | PASS |
| рекурсивный merge `OPENCODE_CONFIG_CONTENT` | PASS |
| документация path/named skills Pi и OpenCode | PASS |

## Dynamic governed child fan-out

| Контракт | Результат |
|---|---|
| массив из структурированного upstream output | PASS |
| отдельный child Run на элемент | PASS |
| устойчивые IDs и fingerprint списка | PASS |
| ordered aggregation | PASS |
| ограничение `max_parallel` | PASS |
| resume только waiting/nonterminal детей | PASS |
| защита от изменения массива при resume | PASS |
| retry создаёт новую группу детей | PASS |
| `all_success`, `all_done`, `one_success` | PASS |
| пустой список отклоняется по умолчанию | PASS |
| `allow_empty: true` возвращает `[]` | PASS |
| множественный approval требует выбрать child Run | PASS |
| выборочная и каскадная отмена | PASS |
| metadata fan-out в `takt children` | PASS |
| профиль `code` использует runtime fan-out | PASS |

## Выполненные команды

```text
gofmt -w cmd internal                         PASS
go vet ./...                                  PASS
go test ./... -count=1                        PASS
go test -race ./... -count=1                  PASS
bash -n scripts/*.sh                          PASS
scripts/test-fake-assistant.sh                PASS
scripts/test-pi-adapter.sh                    PASS
scripts/test-opencode-adapter.sh              PASS
scripts/test-route-dsl-e2e.sh                 PASS
scripts/test-route-dsl-eval.sh                PASS
scripts/test-composition.sh                   PASS
scripts/test-takt-skill.sh                    PASS
scripts/test-code-profile.sh                  PASS
scripts/test-worktree.sh                      PASS
scripts/test-child-runs.sh                    PASS
scripts/test-policies.sh                      PASS
scripts/test-child-fanout.sh                  PASS
scripts/check-docs.sh                         PASS
example validation set                       PASS
```

Один монолитный вызов `scripts/verify.sh` был остановлен внешним ограничением длительности после успешных unit/race и fake-assistant checks. Все оставшиеся неизменённые команды из скрипта выполнены по группам в том же дереве и завершились успешно. Это ограничение инструмента запуска, а не зафиксированный failure проекта.

## Что не проверялось

- реальные обращения к Pi, OpenCode и внешним моделям;
- реальные GitHub/Remotion операции профиля `code`;
- фактическая CI job на macOS;
- системный OS sandbox — текущая filesystem/network policy остаётся assistant-enforced.

## Проверка поставляемого архива

Итоговый ZIP был распакован в новый пустой каталог. Из распаковки прошли:

```text
sha256sum -c MANIFEST.sha256              PASS
go test ./... -count=1                    PASS
go test -race ./... -count=1              PASS
fake/Pi/OpenCode contract suites          PASS
Route DSL contracts                       PASS
composition/profile/skill contracts       PASS
worktree/child-run/policy/fan-out suites  PASS
documentation and example validation      PASS
```

Проверка выполнялась до финального обновления только этого отчёта; после обновления отчёта архив и манифест пересобраны, а контрольные суммы проверены повторно. Исходный код и тестовые сценарии при финальной пересборке не менялись.
