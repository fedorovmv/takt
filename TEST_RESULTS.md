# Результаты проверок — v0.1.39-alpha

Дата проверки: 2026-08-07

## Проверенный срез

- внутренний `RoleDefinition` для trusted worker-ролей;
- bounded `TaskBrief` без автоматического наследования transcript основной сессии;
- области `expected | allowed | protected | forbidden`;
- сверка `changed_files` с фактическим Git diff managed worktree;
- required/preferred checks и реакции `deny | repair | warn`;
- одна ограниченная automatic repair-итерация с повторной независимой проверкой;
- продолжение repair и последующих сегментов в том же execution workspace;
- исправления pause/recovery/retry/fork/summary из ревью `v0.1.37-alpha`;
- исправления notifications из ревью `v0.1.37-alpha`;
- исправления compact Task API и router/coding-agent resolution из ревью `v0.1.38-alpha`;
- совместимость Task Router, MCP surfaces, Dynamic Takt, host-control, external executor и профиля `code`.

## Go

```text
gofmt -l cmd internal                 PASS — пустой вывод
go vet ./...                          PASS
go test ./... -count=1                PASS
go build ./...                        PASS
```

Race detector выполнен группами, чтобы не запускать все instrumented test binaries одновременно. Все проверенные пакеты прошли, включая:

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
internal/rolecontract
internal/runtime
internal/store
internal/taskroute
internal/validation
internal/workflow
internal/yamlmini
```

## Контрактные наборы

```text
fake-assistant contract suite                 PASS
Pi adapter contract suite                     PASS
OpenCode adapter contract suite               PASS
Route DSL end-to-end                          PASS
Route DSL evaluation                          PASS
workflow composition                          PASS
Takt authoring skill                          PASS
code profile catalog                          PASS
git worktree contract                         PASS
governed child Run                            PASS
node capability policy                        PASS
governed child fan-out                        PASS
script and typed artifacts                    PASS
local MCP, 53 operations / agent surface 5    PASS
external node executor                        PASS
deep code workflows                           PASS
authoring contract                            PASS
daemon contract                               PASS ×4
dynamic Takt                                  PASS
trusted block packages, 9 built-in blocks     PASS
coding-agent host control                     PASS
Pi/OpenCode TypeScript contract               PASS
Autonomous Run Operations                     PASS
Simple Reliable Task Router + Role/Brief      PASS
documentation contract                        PASS
```

## Регрессии Autonomous Run Operations

Проверено:

- recovery не уничтожает операторский pause, включая durable состояние `pausing`;
- ошибочный `resume` не очищает pause marker до проверки статуса;
- parent, запаузенный в `waiting(child_run)`, остаётся paused после ответа ребёнку;
- pause перепроверяется перед каждым новым sequential node и каждой новой attempt;
- внешний node не публикуется claimable после запроса pause внутри той же волны;
- foreground `run recover` продолжает восстановленный Run вместо фоновой goroutine, завершающейся вместе с CLI;
- retry сохраняет историю `Executions` и artifacts предыдущих attempts;
- retry из `cancelled` начинает с первого незавершённого узла;
- recursive summary переносит окно между link child ID и публикацией child state;
- fork сохраняет source provenance и source fingerprint;
- persistence errors operator markers не маскируются как успешные операции.

## Регрессии notifications

Проверено:

- `question.required` имеет реального producer через approval с `capture_response`;
- repeat approval/question получает новый dedup key по kind/node/revision и не теряется;
- process/desktop sink ограничен timeout и не блокирует daemon monitor;
- dispatch сериализован межпроцессным lock;
- первый dispatch создаёт baseline без ретроспективного спама;
- устойчивый notification ID предотвращает повторную доставку после сбоя между persist и snapshot;
- inbox имеет ограниченный размер и сначала удаляет старые acknowledged items.

## Регрессии compact Task API и Router

Проверено:

- `task respond <plan-id> --action answer` находит waiting Run/node и доставляет ответ туда;
- `task stop <plan-id>` синхронно переводит активный Run и plan в `abandoned` без daemon;
- пустой `answer` отклоняется и не фабрикует `approved`;
- отсутствующий semantic router приводит к stable `simple-reliable` fallback;
- router fallback сохраняет диагностику и не поглощает cancellation контекста;
- детерминированные risk controls усиливаются Go-кодом даже когда router fixture возвращает их `false`;
- compact Task API покрыт Go unit tests;
- `coding-agent` имеет единый source of truth через `assistant.Factory.Resolve`;
- прямой `takt run` выполняет capability preflight;
- `task start` читает файл только через явный `--file`;
- term-based signals не принимают `author` за `auth` и `debug` за `bug`;
- пустой MCP surface означает `agent`, а `New()` явно сохраняет внутренний historical `all`;
- protocol docs различают single-result `v1alpha1` и event NDJSON `v1alpha2`.

## Регрессии Role / Brief / controls

Проверено:

- trusted block с role получает структурированный `TaskBrief`;
- read-only verifier получает реальную `sandbox.filesystem=read_only` policy;
- mutating role не заявляет неподдерживаемый path-level OS sandbox;
- любой block с capability `repository.write`, включая `test-design`, автоматически включает managed worktree;
- `forbidden` и `outside allowed` дают `deny`;
- `protected` и `unexpected` сохраняют диагностические warnings;
- preferred failure не блокирует Run;
- required `repair` запускает ровно одну automatic repair-итерацию;
- failed check повторяется после repair и сохраняет историю `[failed, passed]`;
- automatic repair продолжает тот же execution workspace и не создаёт новый чистый worktree;
- фактический Git diff сверяется с union заявленных `changed_files`;
- файл, изменённый в worktree, но отсутствующий в `changed_files`, блокирует принятие сегмента;
- повторный required failure после repair переводит plan в `waiting` с одним материальным вопросом.

## Проверка релизного ZIP

После формирования архива он распаковывается в чистый каталог. Проверяются:

```text
MANIFEST.sha256                         PASS
отсутствие собранного bin/ в архиве    PASS
go vet ./...                            PASS
go test ./... -count=1                  PASS
go build ./...                          PASS
Simple Reliable Router + Role/Brief     PASS
Autonomous Run Operations               PASS
host-control / TypeScript               PASS
MCP surfaces / Dynamic Takt             PASS
code profile / trusted blocks           PASS
external executor / deep workflows      PASS
documentation                           PASS
```

## Границы проверки

- Takt не зависит от Kiro CLI; Kiro CLI не использовался при тестировании.
- Готовые wrappers для Codex, Oh My Pi и Qwen CLI в архив не входят. Проверены общий adapter/config seam и process protocol `takt-assistant/v1alpha2`.
- Live smoke с авторизованными Codex, Oh My Pi, Qwen CLI, Pi и OpenCode не выполнялся.
- Bundled Pi/OpenCode host extensions остаются `guarded`, а не `strict`, до live contract tests на зафиксированных версиях хостов.
- Для mutating roles scope enforcement в этом срезе является post-action integrity gate: structured `changed_files` + фактический Git diff. Path-level OS sandbox для записи пока не реализован.
- Фактический macOS-runner в этой среде недоступен; path-sensitive unit tests и `EvalSymlinks` regressions прошли на Linux.
- Route DSL benchmark с реальными моделями не выполнялся; contract/evaluation fixtures прошли.

## Версии

```text
Takt             0.1.39-alpha
code profile     0.14.0
Takt skill       0.21.0
code-core        0.3.0
MCP all          53 operations
MCP agent        5 tools
```
