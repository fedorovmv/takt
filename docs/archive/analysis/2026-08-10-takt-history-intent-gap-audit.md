# Аудит истории, замысла и незакрытых gaps Takt

Дата среза: 2026-08-10

Ветка: `stabilize/live-host-conformance`

Проверенный commit: `5c390f9` (`feat: enforce deterministic plan to pr acceptance`)

Статус: **BLOCKED** для утверждения «Takt закончен», выпуска `v1beta1` или снятия feature freeze; пригоден для дальнейшей alpha-стабилизации.
Уверенность: высокая.

## 1. Объём и метод

Проверены:

- пять Markdown-экспортов истории ChatGPT из `takt_priv/history`: 49 383 строки, 169 prompt-блоков, период 18.07–09.08.2026;
- 81 документ Takt, включая текущие спецификации, roadmap/backlog, ADR, release history и рабочие планы;
- текущий Go-код, schemas, profile/workflow, SDK, reference adapters и Go/E2E tests;
- git history и переходы статусов backlog;
- фактические проверки на текущей ветке.

Источники оценивались в порядке: выполняемый тест/измерение → код и конфигурация → schema/ADR/current spec → current status → roadmap/backlog → release history → история чатов. Старое утверждение «сделано» не считалось доказательством без текущего кода или теста.

Сокращения ссылок на приватную историю:

- `H1` — `takt_priv/history/ChatGPT-Takt-1.md`;
- `H2` — `takt_priv/history/ChatGPT-Takt-2.md`;
- `H3` — `takt_priv/history/ChatGPT-Takt-3.md`;
- `H4` — `takt_priv/history/ChatGPT-Takt-4.md`;
- `H5` — `takt_priv/history/ChatGPT-Takt-5.md`.

## 2. Краткое заключение

Takt успешно превратился из эксперимента по генерации Route DSL в компактный локальный trusted workflow runtime. Реализованы почти все задуманные исполнительные механизмы, архитектурный refactor соответствует позднему замыслу, а stable/experimental/tooling границы проведены правильно.

Но исходная продуктовая гипотеза не доказана: нет реального обезличенного Route DSL corpus со штатным валидатором, production evaluation для всех трёх предметных классов и strict live host conformance. Дополнительно аудит обнаружил не отражённые в backlog дефекты scheduler, worktree/domain side effects, secrets/redaction, adapter protocol и durable lifecycle. Они не позволяют считать фактическое состояние эквивалентным заявлению «ядро реализовано» без оговорок.

## 3. Эволюция замысла

### 3.1. Исходная цель: стабильная Route DSL generation

Начальная задача — получать Camel-подобный YAML DSL из текстового ТЗ. Свободная генерация Qwen 3.5/3.6-27B была нестабильной, несмотря на документацию, примеры и validators (`H1:9-12`). Отдельный промежуточный AST пользователь отверг как слой, который не даёт прироста и добавляет сложность (`H1:258-262`).

Из раннего обсуждения сохранились правильные требования:

- terminal success определяет deterministic validator, а не текст модели;
- retry получает нормализованную ошибку и актуальное состояние, а не бесконечно растущую беседу;
- повторяющиеся ошибки и состояния должны иметь fingerprints;
- предметные инструменты документации, примеров, YAML, jq и Bloblang принадлежат отдельному DSL/tooling слою (`H1:11059-11072`);
- неоднозначное ТЗ требует HITL, а не выдумывания требований.

### 3.2. Workflow/experiment harness вместо очередного coding agent

После нескольких реализаций с разным числом агентов, контекстом и проверками сформулирован основной поворот: схема эксперимента должна стать конфигурацией, а не архитектурой программы (`H1:11023-11057`).

Приняты решения:

- новый Go-runtime, использующий Archon как behavioral reference, а не прямой порт;
- один workflow описывает deterministic и agent actions, loops, branches, approval и hooks;
- Pi/OpenCode/другой coding agent остаётся сменным исполнителем;
- роли не являются фиксированной сущностью ядра; они могут быть инструкциями, отдельными sessions или отсутствовать (`H1:11482-11505`);
- Takt не реализует собственный tool loop, LSP, файловые LLM-tools, память беседы или subagent framework.

Эта модель закреплена в `docs/01-project.md:17-53` и историческом
`docs/archive/analysis/02-initial-approach.md:23-62`.

### 3.3. Dynamic Takt и embedding в coding agents

К 06.08 цель расширилась: Takt должен не только выбирать готовый workflow, но и строить bounded task-specific plan, проверять budget/capabilities, исполнять его обычными governed Run, перепланировать и сохранять успешный процесс (`H3:522-549`).

Распределение ответственности:

- coding-agent host — UX, session и tool gate;
- LLM — решение и task-specific план;
- Takt — state, scheduler, policy, budgets, evidence и terminal transitions;
- corporate Git/Tracker/CI — provider-neutral adapters, не GitHub-specific core.

### 3.4. Простота и feature freeze

После разрастания control plane пользователь сформулировал принцип: **«Просто по умолчанию, строго по необходимости, сложность — внутри runtime»** (`H3:6945-6993`). Основной агент не должен видеть внутренние host/worker/operator операции; текущие пять `takt.task.*` tools соответствуют этому решению.

После `v0.1.51` введён feature freeze и потребован SOLID/DRY/KISS/YAGNI refactor (`H5:483-548`). Финальная формулировка цели: функции не удалять, но отделить stable core от experimental/tooling/extensions, заменить самописную инфраструктуру зрелыми библиотеками и довести пользовательский путь до стабильного состояния (`H5:2621-2641`).

## 4. Что реализовано в соответствии с замыслом

| Направление | Фактическое состояние |
|---|---|
| Универсальный Go runtime вместо собственного coding agent | Реализовано |
| DAG, `depends_on`, `when`, trigger rules, loops, retries, approval, hooks | Реализовано и покрыто unit/E2E tests |
| Durable state/events/revisions, resume, pause, cancel, abandon, recovery | Реализовано; есть дефекты отдельных error paths |
| Один scheduler для root/compiled composition | Реализовано; parallel path требует исправлений из раздела 6 |
| Structured input/output и deterministic validation | Реализовано |
| Node policy и governed child policy | Реализовано с fail-before-execution preflight |
| Process v1alpha1/v1alpha2 и fake conformance | Реализовано |
| Pi/OpenCode session adapters | Реализовано как bundled extensions; live host остаётся guarded |
| Dynamic Plan, replan, steering, promotion | Реализовано как experimental |
| Local daemon и autonomous run operations | Реализовано; lifecycle/attention имеют gaps |
| Agent/Domain/Task Source SDK | Реализовано, есть Qwen/GitHub reference implementations |
| BlockPackage, multi-repo, worktrees, artifacts | Реализовано alpha-срезами |
| Human-reviewed Learning Loop | Реализован bounded slice; auto-activation отсутствует намеренно |
| Thin transports, application services, single bootstrap | Реализовано в `v0.1.52–v0.1.57` |
| Stable/extensions/experimental/tooling boundaries | Реализовано и закреплено architecture tests |
| Компактная agent-facing MCP surface | Основной агент видит пять task tools; host/worker/operator скрыты |
| Deterministic `code:plan-to-pr` acceptance | Реализовано в `5c390f9`: scope, PR/review/summary gates и `safe_success/safe_stop` |

Подтверждающие текущие источники: `docs/05-implementation-status.md`, `docs/09-runtime-semantics.md`, `docs/10-assistant-adapter-spec.md`, `docs/04-architecture.md`, `internal/architecture`, `internal/runtime`, `tests/e2e`. Исторический release slice сохранён в `docs/archive/releases/72-architecture-contracts-v0.1.57.md`.

## 5. Явно незавершённые продуктовые направления

Текущий status перечисляет шесть основных gaps (`docs/05-implementation-status.md:224-233`):

1. Live Route DSL production evidence.
2. Обезличенная Go + Document production evaluation; Go production-shaped срез не заменяет production corpus.
3. Финальная `v1alpha1 → v1beta1` migration после evidence.
4. Live strict host conformance Pi/OpenCode: tool/completion и часть Pi boundaries не доказаны.
5. Live Qwen/GitHub reference adapter smoke с credentials.
6. `workflow graph/explain/scaffold`, расширенный `plan explain` и статический reject/revise.

Дополнительно:

- task-level benchmark измеряет правильность контура, но ещё не доказал качество Router/Dynamic на реальных задачах;
- corporate Git/Tracker/CI implementations не входят в репозиторий: готов extension contract, а не корпоративное внедрение;
- Dynamic validation использует assistant-provided `passed`; approved design откладывает project-owned deterministic evidence на второй stabilization stage (`docs/superpowers/specs/2026-08-10-deterministic-development-flow-acceptance-design.md:76-79`).

### 5.1. Главный незакрытый исходный результат

`examples/route-dsl-benchmark` содержит 25 regression/production-shaped synthetic cases. Сам документ явно говорит, что live качество требует штатного Route DSL validator и выбранного coding-agent/model adapter, а `production-shaped` не означает обезличенное production ТЗ (`examples/route-dsl-benchmark/README.md:1-10,52`).

Поэтому исходный вопрос «Takt действительно стабилизирует генерацию нашего DSL?» всё ещё не имеет доказательного ответа. Измерительный runtime готов; нет необходимого production evidence и полного предметного delivery pack.

Предметный pack должен жить вне core и включать:

- штатный semantic validator;
- реальные обезличенные ТЗ и expected outcomes;
- поиск документации и примеров;
- jq/Bloblang block validate/run/test/debug;
- фиксацию manual corrections и clarification quality;
- benchmark прямого, repair/simple-reliable и Dynamic подходов.

## 6. Реализовано не так, как заявлено

### P0 — блокеры release/stability claim

#### CORE-001. Parallel assistant nodes используют общий mutable RunState

`internal/runtime/parallel.go:39-74` запускает `r.execute(..., state, ...)` конкурентно. Assistant path мутирует `state.Nodes[node.ID].Policy` и вызывает `commit` (`internal/runtime/assistant_node.go:83-89`), а `commit` меняет revision/time и пишет Store (`internal/runtime/runner.go:1107-1138`).

Последствия: data race, конкурирующие revisions, потеря durable state. Существующий parallel regression покрывает bash, а не несколько assistant nodes.

Способ закрытия: regression с двумя assistant actions под `-race`; все state transitions/persistence сериализовать либо исключить state-mutating actions из parallel path.

#### CORE-002. Domain adapter получает control workspace вместо execution worktree

`internal/runtime/domain_adapter.go:79` формирует `InvokeRequest.Workspace: state.Workspace`; reconcile наследует тот же путь на строке 128. Process transport использует request workspace как cwd.

Последствия: SCM/Tracker/CI action при managed worktree или multi-repo может читать/изменять control checkout вместо candidate worktree. Это противоречит `docs/03-specification.md` и заявлению status об execution workspace.

Способ закрытия: передавать `state.ExecutionWorkspace`/`r.workspace`; добавить regression с managed worktree и Invoke/Reconcile.

### P1 — обязательные correctness/security fixes

#### CORE-003. Global hooks пропускаются в parallel wave

`parallelEligible` проверяет только `node.Hooks` (`internal/runtime/parallel.go:19-30`), а parallel path вызывает `execute` напрямую. Объединение global/local hooks выполняется только в `runNode` (`internal/runtime/runner.go:652-670`).

Последствие: workflow-level checks тихо не исполняются для независимых простых узлов.

#### CORE-004. Документированный idempotency template не работает

Спецификация разрешает:

```yaml
side_effect:
  mode: reconcile
  idempotency_key: ${run.id}:${node.id}
```

Но `resolveTemplatePath` не поддерживает `run.id` и `node.id` (`internal/runtime/template.go:84-115`). Domain adapter получает unresolved expression; external executor сохраняет буквальный template (`internal/runtime/assistant_node.go:135-140`), что создаёт одинаковый ключ в разных Run. Authoring preflight это поле не проверяет.

#### SEC-001. Pi/OpenCode не разрешают SecretRef

Generic process вызывает `redact.NewFromEnvironment().Resolve`, а `piEnvironment` и `openCodeEnvironment` только render-ят строки (`internal/extensions/assistants/pi/pi.go:395-408`, `internal/extensions/assistants/opencode/opencode.go:312-322`).

Последствия: исполнитель получает `secret://NAME` буквально, отсутствующий secret не блокирует Run; поведение providers расходится.

#### ADAPTER-001. Pi/OpenCode не публикуют промежуточные activity events

Контракт считает `Request.Emit` activity signal, сбрасывающий `idle_timeout` (`docs/10-assistant-adapter-spec.md:113-115`). Pi/OpenCode парсят provider events, но не вызывают `Emit`; runtime добавляет только synthetic session start и post-run message/usage/terminal.

Последствия: отсутствуют durable intermediate tool/diagnostic events; долгий активный agent может быть ошибочно остановлен по idle timeout.

#### OPS-001. Tool approval отсутствует в attention/notifications

External worker сохраняет `waiting_approval` (`internal/externalworker/service.go:845-853`), но application и notifications ищут `requested|waiting` (`internal/application/operations.go:203-210`, `internal/extensions/notification/notification.go:400-406`).

Последствие: реальный approval может не попасть в attention queue и notification delivery.

#### SEC-002. Evaluation report строится из live unredacted state

Runtime redacts clone перед Store commit, но оставляет live state исходным. Evaluation напрямую копирует live node output/stdout/stderr/error/feedback в report (`internal/tooling/evaluation/evaluation.go:587-713`) и JSON-marshals его без redactor.

Последствие: SecretRef-derived value может сохраниться в `report.json`, несмотря на общий persistence redaction invariant.

#### EVAL-001. Evaluation подавляет persistence errors

Infrastructure-error path игнорирует ошибку `writeReport` (`internal/tooling/evaluation/evaluation.go:311-315`), а runtime metrics молча прекращают расчёт при ошибке `ReadEvents`.

Последствие: persistence failure или отсутствующий true time-to-valid становится скрытой неполной метрикой.

#### PROTO-001. Process v1alpha2 слабее заявленного контракта

`finishV1Alpha2` помечает `Truncated`, но не возвращает protocol error, тогда как v1alpha1 делает это fail-closed. Stream decoder также принимает записи после terminal result, если это не второй `result`; public SDK conformance такие записи отклоняет.

Дополнительный drift: public SDK почти не валидирует raw event против declared `event_types`, а JSON Schema stream records слабее фактического Go decoder.

#### DURABLE-001. Cancel/abandon/recovery подавляют Store errors

Marker reads и child marker writes преобразуют ошибки в `false` или игнорируются (`internal/runtime/cancel.go:27-55,108-119,168-175`). Detached recovery/continuation также запускается с проигнорированной ошибкой (`internal/application/operations.go:860-862,903-905`).

Последствие: вызывающий код может считать Run отменённым или восстановленным, хотя durable transition не произошёл.

#### OPS-002. Daemon lifecycle не имеет надёжной stop/start boundary

CLI считает daemon остановленным после исчезновения health/socket, но lock освобождается позже при выходе `Serve`. Немедленный start может увидеть старый flock и завершиться, оставив workspace без socket. Recovery error логируется, после чего daemon всё равно публикует healthy endpoint.

### P2 — ограниченный долг

- `worktree.Remove` и `ForkRun` местами возвращают live `PublicView()` после commit вместо reload из Store;
- external worker принимает caller-supplied future `event.Time`, способный отложить idle timeout;
- нет rerun отдельного завершённого fan-out item;
- нет общей защиты от mutating siblings в одном shared workspace;
- `required_checks`, `branch_rules`, `change_request_template` BlockPackage остаются governance metadata без generic enforcement;
- stale lock после crash требует ручного удаления.

## 7. Что отсутствует или расходится в backlog

### 7.1. Обязательные новые задачи

Текущий `docs/14-backlog-v0.2.md` не содержит `CORE-001..004`, `SEC-001..002`, `ADAPTER-001`, `OPS-001..002`, `EVAL-001`, `PROTO-001` и `DURABLE-001`. До их появления status «ядро/runtime реализовано» следует читать как functional coverage, а не release-ready correctness.

### 7.2. Status и backlog расходятся

Из шести gaps `docs/05` backlog отражает:

- Route DSL evidence;
- Go + Document evidence;
- v1beta1 migration;
- strict Pi/OpenCode host;
- graph/explain/scaffold;
- static reject/revise.

Но live Qwen/GitHub reference adapter smoke отсутствует как отдельный backlog item, а roadmap говорит, что из external seams остался только host gate.

### 7.3. Deterministic acceptance tracking устарел

`5c390f9` реализовал и задокументировал `code:plan-to-pr` acceptance, однако:

- `docs/06-roadmap.md`, `docs/11-implementation-plan.md` и `docs/14-backlog-v0.2.md` не отражают этот срез;
- `docs/superpowers/plans/2026-08-10-deterministic-development-flow-acceptance.md` оставляет все шаги unchecked;
- known Dynamic validation self-attestation вынесена в design note, но не в backlog.

### 7.4. Оригинальный DSL delivery pack почти исчез из планирования

Backlog оставляет constrained generation, RAG examples, N-candidate и DSPy/GEPA условными benchmark strategies (`docs/14-backlog-v0.2.md:158-164`), но не фиксирует обязательный предметный validator/docs/examples/jq/Bloblang delivery layer. Это не core feature, однако без него исходная product goal не может быть проверена и внедрена.

## 8. Документационный drift

1. `docs/03-specification.md:26` говорит, что обязательный `${path}` fail-closed, а строка 715 — что неизвестный token сохраняется буквально и strict renderer остаётся задачей v0.2. Код реализует fail-closed.
2. `docs/09-runtime-semantics.md` сохраняет старую формулировку про Takt YAML subset, хотя YAML syntax делегирован `go.yaml.in/yaml/v3`.
3. `docs/13-evaluation-plan.md:162` называет task-level Dynamic benchmark будущим, а следующий раздел и status считают его реализованным.
4. Заголовки `docs/03` и `docs/13` остались на `v0.1.52`, `docs/11` и `docs/14` — на `v0.1.56`, при текущем `v0.1.57+`.
5. Удалённые manifest/check-docs gates всё ещё присутствуют в исторических рабочих планах без маркировки obsolete.
6. `README` сохраняет историческое число `internal/application` около 3,5k после v0.1.55, а status после v0.1.56 указывает около 2,2k; требуется датировать формулировку.
7. Functional implementation Dynamic/host/evaluation легко принять за stable guarantee; `experimental|supported-alpha|guarded` нужно показывать одинаково во всех current docs.

## 9. Что сознательно не является долгом

Без нового use case и threat model не следует возвращать в core:

- собственный coding-agent tool loop, LSP, file tools и conversation memory;
- Web UI, network server, database и object storage;
- remote workers и Slack/Telegram/message adapters;
- multi-user auth/RBAC и untrusted workflows;
- generic plugin framework;
- nested `loop_group`, полный JSON Schema и parallel retries/hooks без production evidence.

## 10. Приоритетный план закрытия

1. Остановить feature growth и закрыть `CORE-001..004` regression/race tests и минимальными fixes.
2. Закрыть `SEC-001..002`, `ADAPTER-001`, `PROTO-001` и `EVAL-001`.
3. Закрыть `DURABLE-001`, `OPS-001..002`; добиться стабильного полного release gate под нагрузкой.
4. Пересобрать `docs/05/06/11/13/14` как согласованный status/backlog и привести рабочие планы к фактическому состоянию.
5. Подготовить внешний Route DSL delivery pack и выполнить real corpus benchmark минимум с тремя repeats каждого case/strategy.
6. Выполнить Go/Document production evaluation и task-level Dynamic evaluation на реальных задачах.
7. Закрыть strict Pi/OpenCode host и live reference adapters.
8. Только после evidence и исправления core defects фиксировать `v1beta1`; затем брать graph/explain/scaffold и static reject/revise.

## 11. Проверка текущего среза

На `5c390f9`:

- `go vet ./...` — PASS;
- `tests/e2e`, включая `TestPlanToPRAcceptance`, — PASS в полном `go test ./...` запуске;
- `go test ./... -count=1` — FAIL из-за load-sensitive `internal/mcp.TestHostRunAndNotificationToolsThroughMCP`: detached plan не достиг stable boundary за 5 секунд;
- тот же MCP test отдельно прошёл 10/10 за 2,33–3,61 секунды, поэтому проблема проявляется под общей нагрузкой;
- свежий aggregate `go test -race ./...` после `5c390f9` не подтверждён, поскольку обычный полный suite остаётся красным.

После проверки в рабочем дереве появилась отдельная незакоммиченная правка `internal/mcp/server_test.go`, увеличивающая deadline с 5 до 15 секунд. Она не входит в этот audit commit и не считается доказательством устранения причины.

## 12. Условия снятия BLOCKED

Статус можно изменить на `CONDITIONAL`, когда:

- устранены `CORE-001`, `CORE-002`, `SEC-002` и `DURABLE-001` с regression/race evidence;
- полный `go test ./...` и `go test -race ./...` воспроизводимо зелёные;
- P1 defects получили явные backlog IDs, owners и критерии закрытия;
- текущие документы больше не противоречат реализованным contracts.

Статус `READY` для `v1beta1` требует дополнительно:

- real Route DSL, Go и Document production evidence;
- strict host/external seam conformance;
- подтверждённую field-by-field migration policy по фактически используемым contracts.
