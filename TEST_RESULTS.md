# Результаты проверок — v0.1.38-alpha

Дата проверки: 2026-08-07

## Проверенный срез

- Task Router: `workflow | template | dynamic`;
- стабильный шаблон `simple-reliable` и progressive controls;
- inspect-first fallback при недоступном semantic router;
- логический `coding-agent` и `default_assistant`;
- нейтральный process contract `takt-assistant/v1alpha2`;
- компактный Task API;
- MCP surfaces `agent | host | worker | operator | all`;
- новые trusted blocks `baseline` и `test-design`;
- совместимость Dynamic Takt, host-control, external executor и Autonomous Run Operations.

## Go

```text
gofmt -l cmd internal                 PASS — пустой вывод
go vet ./...                          PASS
go test ./... -count=1                PASS
go build ./cmd/takt                   PASS
go build fake assistants              PASS
```

Race detector выполнен по пакетам отдельными запусками, чтобы малый release-runner не запускал все instrumented test binaries одновременно. Все Go-пакеты прошли, включая:

```text
cmd/takt
internal/assistant
internal/authoring
internal/blockcatalog
internal/command
internal/config
internal/control
internal/daemon
internal/definition
internal/dynamicplan
internal/evaluation
internal/execution
internal/gitworktree
internal/hostcontrol
internal/mcp
internal/notification
internal/profile
internal/runtime
internal/store
internal/taskroute
internal/validation
internal/workflow
internal/yamlmini
```

`go test -race ./...` и полный `scripts/verify.sh` одним процессом в этой среде не заявляются: длинный объединённый запуск достигал внешнего лимита команды. Их составляющие выполнены отдельно и прошли.

## Контрактные наборы

```text
fake-assistant contract suite                 PASS
Pi adapter contract suite                     PASS
OpenCode adapter contract suite               PASS
Route DSL end-to-end                           PASS
Route DSL evaluation                           PASS
workflow composition                           PASS
Takt authoring skill                           PASS
code profile catalog                           PASS
git worktree contract                          PASS
governed child Run                             PASS
node capability policy                         PASS
governed child fan-out                         PASS
script and typed artifacts                     PASS
local MCP, 53 operations / agent surface 5     PASS
external node executor                         PASS
deep code workflows                            PASS
authoring contract                             PASS
daemon contract                                PASS
dynamic Takt                                   PASS
trusted block packages, 9 built-in blocks      PASS
coding-agent host control                      PASS
Pi/OpenCode TypeScript contract                PASS
Autonomous Run Operations                      PASS
Simple Reliable Task Router                    PASS
documentation contract                         PASS
```

## Новые регрессии

Проверено:

- ошибка semantic router создаёт durable `router_fallback`, а не блокирует задачу;
- protected-сигналы монотонно включают baseline, independent tests и enhanced review;
- route не может сослаться на неизвестный workflow или недоверенный block;
- `coding-agent` разрешается через `default_assistant`, legacy OpenCode fallback или единственный assistant;
- неоднозначная конфигурация без default отклоняется;
- прямой `takt run`, `takt validate` и daemon используют одинаковую проверку логического assistant;
- agent MCP публикует ровно пять `takt.task.*` и отклоняет operator/host/worker tools;
- `--surface all` сохраняет полный совместимый набор из 53 операций;
- built-in `code-core` содержит девять блоков, включая `baseline` и `test-design`;
- профиль `code` устанавливается в версии `0.13.0` и содержит Task Router;
- внешний executor по-прежнему проходит полный tool request/decision/start/complete lifecycle.

## Проверка релизного ZIP

Архив распакован в чистый каталог. Проверены:

```text
MANIFEST.sha256, 441 source files       PASS
отсутствие собранного bin/ в архиве     PASS
go vet ./...                            PASS
go test ./... -count=1                  PASS
сборка Takt и fake adapters             PASS
Simple Reliable Router                  PASS
MCP surfaces                            PASS
Dynamic Takt                            PASS
code profile / trusted blocks           PASS
external executor                       PASS
daemon / host-control / TypeScript      PASS
Autonomous Run Operations               PASS
deep code workflows                     PASS
documentation                           PASS
```

## Границы проверки

- Takt не зависит от Kiro CLI; Kiro CLI не использовался при тестировании.
- Готовые wrappers для Codex, Oh My Pi и Qwen CLI в архив не входят. Проверены общий `SessionAdapter`, schema/config и process protocol seam `takt-assistant/v1alpha2`.
- Live smoke с авторизованными Codex, Oh My Pi, Qwen CLI, Pi и OpenCode не выполнялся.
- Bundled Pi/OpenCode host extensions остаются `guarded`, а не `strict`, до live contract tests на зафиксированных версиях хостов.
- Фактический macOS-runner в этой среде недоступен; path-sensitive unit tests и `EvalSymlinks` regressions прошли на Linux.
- Route DSL benchmark с реальными моделями не выполнялся; contract/evaluation fixtures прошли.

## Версии

```text
Takt             0.1.38-alpha
code profile     0.13.0
Takt skill       0.20.0
code-core        0.2.0
MCP all          53 operations
MCP agent        5 tools
```
