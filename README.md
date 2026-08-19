# Takt

Универсальный Go-runtime для воспроизводимых процессов с кодовыми агентами, одиночными вызовами моделей, детерминированными командами, циклами проверки и участием человека.

Проект вдохновлён моделью Archon, но не является портом его исходного кода. Takt реализует компактное подмножество наиболее полезных механизмов в Go и сохраняет строгую границу с Pi, OpenCode, Codex и другими кодовыми агентами.

## Область применения текущей версии

`v0.1.62-alpha` предназначена для **локального однопользовательского trusted runtime**. Workflow, config, Markdown-команды и рабочая директория считаются доверенными.

Локальный `takt daemon` поддерживает фоновые Run и несколько клиентов одного пользователя через Unix socket. Сетевой и многопользовательский запуск, а также выполнение конфигураций от недоверенных пользователей требуют sandbox, политики путей, изоляции сети, управления секретами и distributed locking. Эти режимы не поддерживаются.

## Текущая точка развития

К `v0.1.62-alpha` Takt завершил архитектурный feature freeze и перешёл от накопления возможностей к стабилизации пользовательского контура. Stable core отделён от extensions, experimental и tooling; Dynamic Flow остаётся доступным, но больше не определяет стабильность основного Run/runtime API. Ближайшая цель — **проверять основные пользовательские сценарии на реальных задачах, исправлять найденные дефекты и постепенно продвигать доказанные experimental contracts в stable surface**.

Приоритеты:

1. application/test/architecture/modular/codebase-hygiene boundaries — выполнены в `v0.1.52–v0.1.57` и закреплены автоматическими gates;
2. стабильный пользовательский путь `build → init → validate → run → inspect/recover`: Linux/macOS smoke, понятные ошибки, документация и backward compatibility;
3. реальные coding-agent/adapter сценарии через stable Run/runtime API; live conformance там, где сейчас есть только guarded/reference integrations;
4. evidence на реальном обезличенном corpus для experimental Dynamic Flow и tooling evaluation до promotion их контрактов в stable;
5. `v1beta1` contract convergence — только после пользовательского и production evidence, без нового feature growth ради самого roadmap.

Актуальный план: [`docs/06-roadmap.md`](docs/06-roadmap.md), backlog: [`docs/14-backlog-v0.2.md`](docs/14-backlog-v0.2.md).

## Что уже работает

- конфигурация моделей и исполнителей;
- Markdown-команды с frontmatter;
- workflow в YAML или JSON;
- DAG с параллельным выполнением независимых узлов, `depends_on`, `when`, `trigger_rule` и cleanup-семантикой `always_run`;
- единая семантика корневого DAG и дочернего DAG `loop_group`;
- узлы `command`, `prompt`, `bash`, `script`, `adapter`, `approval`, `loop_group`, `subworkflow`, `foreach`, `workflow`;
- reusable `subworkflow` компилируется в тот же DAG, а `workflow` запускает отдельный governed child Run;
- последовательный и параллельный `foreach` для inline-списков и внешних YAML/JSON-массивов без преобразования Markdown в task AST;
- `subworkflow` и `foreach` внутри `loop_group`;
- JSON-массив результатов всех итераций `foreach`;
- публичное состояние Run без внутренних развёрнутых ID;
- durable `loop_iterations[]` со всеми завершёнными итерациями и совместимым `loop_previous`; `max_iterations` ограничен 64;
- вложенные `loop_group` явно остаются запрещёнными в целевом `v0.2` (текущий `v1alpha1` их отклоняет);
- повтор узла после внешней проверки;
- переносимые hooks `before_node`, `after_node`, `before_complete`, `on_failure`;
- разделение ненулевого exit code, ошибки запуска, timeout и cancellation;
- `allow_failure`, разрешающий только ненулевой exit code;
- `all_done` после неуспешной зависимости;
- timeout всей попытки узла, включая portable hooks, и activity-based `idle_timeout` для AI-узлов;
- timeout/cancellation родительского `loop_group` сохраняют `timed_out`/`cancelled`;
- общий thread-safe лимит stdout/stderr process assistant;
- approval с сохранением состояния и продолжением через `takt answer`, включая повторные решения внутри `loop_group`;
- явное продолжение через `takt resume`;
- fingerprints workflow, config и Markdown-команд;
- блокировка Run при `answer` и `resume`;
- ревизии состояния и событий с проверкой согласованности;
- JSONL-журнал событий и файловые артефакты;
- stable assistant core: `mock` и универсальный `process`; bundled Pi/OpenCode implementations подключаются как extensions;
- JSON-протокол `takt-assistant/v1alpha1` и потоковый `takt-assistant/v1alpha2` для внешних process assistants;
- fake-assistant contract suite: success, exit, start, timeout, cancel, concurrent output, malformed/strict protocol cases, fresh и resume;
- Pi RPC adapter и fake-Pi contract suite, включая model/thinking mapping, fresh/resume, ожидание `agent_settled`, автоматический retry, per-attempt usage delta, timeout/cancel, output limit и границу extension UI;
- OpenCode CLI adapter через `opencode run --format json`, с model/agent/variant mapping, проверенным resume, per-step usage, сохранением provider diagnostics при timeout/cancellation и contract suite;
- полное совпадение OS exit code и envelope `exit_code`, включая ноль;
- единый JSON envelope CLI для успеха и ошибок;
- YAML через поддерживаемый `go.yaml.in/yaml/v3` с Takt-specific strict JSON-field adapter, path-aware `did you mean` и authoring diagnostics;
- расширенный проверяемый `output_format`, статическая диагностика output/artifact references и строгие `$path`, `$path?`, `$path:-default`;
- именованные workflow профиля, `workflow list/describe` и селектор `profile:name`;
- профиль `code` 0.19.3 с 19 процессами разработки, Task Router, user-owned Git scope и deterministic acceptance gates для `plan-to-pr`, outcome-gated `feature-development` с одной repair/revalidation веткой, внутренними Role/TaskBrief contracts и одиннадцатью встроенными доверенными блоками;
- управляемые Git worktree: политика workflow, отдельная ветка, безопасное удержание/очистка и `takt worktree list/remove/prune`;
- parent/child lifecycle с отдельными state/events/artifacts/usage, `takt children`, каскадным `takt cancel` и approval через корневой Run;
- динамический fan-out governed child Runs из структурированного output: устойчивые child ID, `max_parallel`, resume, ordered aggregation и join policies;
- script runtime `command|python|node|go|validation` с fingerprints исходника и зависимостей;
- типизированные артефакты с MIME, SHA-256, producer metadata, CLI `takt artifacts` и передачей parent/child/fan-out;
- role-based локальный MCP control plane: основная LLM по умолчанию видит пять `takt.task.*`, а полная совместимая поверхность содержит 54 операции workflow/Run/host/worker/operator;
- Simple Reliable Task Router: выбор `workflow|template|dynamic`, прогрессивные baseline/independent-tests/enhanced-review controls и inspect-first fallback;
- Evidence/baseline/failure routing: `EvidenceManifest`, candidate content SHA-256, stale verdict invalidation, baseline failure fingerprints, `parked` с safe next action и external `side_effect: reconcile`;
- Adapter Platform: нейтральные SCM/tracker/CI операции через `adapter`-узлы, process/MCP transports, capability discovery, durable idempotency/receipt/reconcile, public Agent/Domain SDK и reference Qwen Code/GitHub SCM adapters;
- Portable Package Distribution: local/Git установка `BlockPackage`, global/corporate/project scopes, lock с commit/checksum, dependencies/adapter requirements, package policy/signatures и автоматическое подключение locked packages;
- Dynamic Takt: решение `existing|planned`, ограниченный `WorkflowPlan`, доверенные `BlockPackage`, компиляция в обычные governed child Run, preview/confirmation, полные бюджеты, checkpoint-replanning, steering, plan revisions и продвижение completed-плана в workflow проекта;
- Coding Agent Host Control: Go-ядро поддерживает strict host contract, а bundled Pi/OpenCode extensions работают в честном `guarded`-режиме с fail-closed cache до live smoke на зафиксированных версиях хоста;
- Autonomous Run Operations: реестр и attention queue, safe pause/resume, retry/fork/abandon, PID-based recovery, уведомления и агрегированный result summary;
- локальный `takt daemon` на Unix socket и файловом Store: background Runs, event subscriptions, MCP proxy, idle enforcement внешних workers и несколько клиентов без БД;
- event protocol v2: session lifecycle, tool request/allow/deny/start/complete, отдельная отмена tool call, artifact declaration с `call_id`, usage/diagnostic/terminal events и capability declaration;
- aggregate usage по узлам и отдельные execution records по каждой фактической попытке;
- `takt eval run/report/benchmark/compare` для воспроизводимой оценки и попарного сравнения стратегий: matrix/repeat/gates, fingerprints, true time-to-valid, failed-execution cost, diagnostic stability и category breakdown;
- `takt eval task-benchmark` для полного `Task Router → template/dynamic → checkpoint → replan → result`: route accuracy, plan revisions, replanner runs, pairwise outcomes и task-level gates;
- `takt eval flow` для изолированных production-shaped cases с durable repeat evidence и читаемым `SCOPE | EVENT | DETAILS` trace в stderr; heartbeat показывает последний измеренный model-request context или `unknown`; `takt eval flow init code:feature-development --output evals/feature` создаёт только suite/example skeleton, после чего добавь config, validator и initial workspace;
- `make eval-smoke|eval-feature-smoke|eval-feature|eval-review|eval-architect` запускает готовые live Pi-проверки без ручного набора аргументов; `eval-feature-smoke` проверяет `implement-basic`, а `eval-feature` — все feature cases; progress и путь `report.json` видны в trace, требуется локальный `examples/flow-evaluation/mini-du/config.yaml`;
- `make eval-status RUN=<eval-dir>` читает атомарный `progress.json` работающего или завершённого flow eval и показывает elapsed time, фазовые тайминги, наблюдаемые LLM wait/stream/total/tool durations, процент завершённых cases/nodes, quality valid rate, input/output/total tokens и текущий измеренный model context; `make eval-stats RUN=<eval-dir>` печатает итоговую статистику, а до первого report checkpoint — partial live stats из `progress.json` с `complete=false`; `make eval-inspect RUN=<eval-dir> [CASE=...] [REPEAT=...]` во время работы читает live progress, а после checkpoint показывает validator/runtime cause, незавершённые узлы и доступные evidence; `make eval-compare A=<eval-dir> B=<eval-dir>` выдаёт A/B scorecard с явными `BETTER|WORSE|SAME`, correctness/reliability/efficiency, моделями, ресурсами и переходами cases; ни одна из этих команд не запускает workflow или модели;
- `takt eval analyze <saved-evaluation-dir> [--language en|ru]` запускает read-only advisory расследование сохранённого прогона через dedicated `takt_analyze`; timestamped redacted analysis reports добавляют к immutable deterministic verdict причинный механизм, failure point и prevention с проверяемыми citations, сохраняют язык, session evidence и bounded raw output для protocol errors; `make eval-analyze RUN=... EVAL_ANALYSIS_LANGUAGE=ru` — короткий запуск этой команды на русском;
- `eval-feature|eval-review|eval-architect` fail-closed после 5 минут без assistant progress; для калибровки используйте `EVAL_IDLE_TIMEOUT=10m make eval-feature`;
- атрибуция tokens/cost по execution identity; смена assistant, его версии или resolved model между retry помечается как mixed;
- общие `model_presets` Config для произвольных aliases в `takt run`, `takt command run` и eval; Make shortcuts принимают `EVAL_PRESET` и generic `MODEL_<ALIAS>` overrides;
- измеренные нулевые показатели сохраняются как `0`, а недоступные средние значения — как `null`;
- validation envelope сохраняется при любом terminal status quality-node; успех требует `completed && valid=true`;
- строгий контракт результата валидатора `takt-validation/v1alpha1`;
- commodity parsing/validation делегированы поддерживаемым Go-библиотекам: `go.yaml.in/yaml/v3` и `github.com/santhosh-tekuri/jsonschema/v6`; Takt сохраняет только собственные strict/subset contracts.

## Outcome-gated Production Flow Evaluation v0.1.62

`v0.1.62-alpha` сохраняет воспроизводимые production-flow suites, внешний
статус и трассировку активного flow, сохранение исходников и Git evidence,
человекоочитаемые stats/compare/inspect и отдельный read-only LLM-анализ причин.
`code:feature-development` теперь принимает только строгий review verdict:
`PASS` продолжает flow, `REPAIR` разрешает ровно одну repair и независимую
revalidation, а `BLOCKED` останавливает доставку. После принятия review
требуются PR/URL/summary artifacts; evaluation fixture проверяет хотя бы один
успешный `pr create`. Mini-du validator 3 добавляет два сценария и переводит
новые прогоны в generation v2; hook retry session принимает только `fresh` или
`resume` вместе с `action: retry`. Provider outages получают сохраняемые
same-session повторы Takt, а скрытые повторы Pi в изолированной оценке отключаются. Нативный блок
`assistants.<name>.settings` управляет создаваемым `.pi/settings.json` без
переименования параметров Pi. Контракт и команды описаны в
[`docs/13-evaluation-plan.md`](docs/13-evaluation-plan.md).

## Architecture Contracts v0.1.57

`v0.1.57-alpha` закрепляет три правила эволюции без новых функций: **«YAML координирует. Код вычисляет. Агент принимает решения.»**, immutable provider registrations с единственной production-сборкой в `bootstrap`, и schema-first `OperationDescriptor` как единый источник appapi/MCP/docs контрактов. `when` не расширяется по одному оператору; extensions не используют global registration state; generated canonical operation docs проверяются на drift. Решение: [`docs/72-architecture-contracts-v0.1.57.md`](docs/72-architecture-contracts-v0.1.57.md), ADR-090.

## Codebase Hygiene & Stabilization v0.1.56

`v0.1.56-alpha` завершает общий архитектурный cleanup перед реальными пользовательскими прогонами. `takt-schema-subset/v1` остаётся ограниченным контрактом Takt, но фактическую Draft 2020-12 validation выполняет upstream `jsonschema/v6`; Pi/OpenCode implementation находятся в extensions, fake binaries — только в testsupport, а external worker/tool lifecycle вынесен из общего application package в `internal/externalworker`. Стабильные process v1alpha2/script/child-run paths разложены на явные фазы. CLI и MCP теперь прямо показывают experimental статус Dynamic Flow/Host Control. Решение: [`docs/70-codebase-hygiene-stabilization-v0.1.56.md`](docs/70-codebase-hygiene-stabilization-v0.1.56.md), ADR-089.

## Core Stabilization & Modularization v0.1.55

`v0.1.55-alpha` не удаляет функциональность, а разделяет её по стабильности: stable application/runtime/workflow/config/profile/store не зависят от `experimental`, `extensions` или `tooling`; Dynamic Flow/Host/Learning находятся в experimental, Package/Block/Notification — в extensions, evaluation/compatibility — в tooling. `internal/application` уменьшен примерно с 6,8 тыс. до 3,5 тыс. production-строк. Самописный YAML parser удалён в пользу `go.yaml.in/yaml/v3`; `make journeys` проверяет основной пользовательский путь через реальный CLI. Решение: [`docs/69-core-stabilization-modularization-v0.1.55.md`](docs/69-core-stabilization-modularization-v0.1.55.md), ADR-088.

## Architecture hardening v0.1.54

`v0.1.54-alpha` продолжает feature freeze: shared application Context и Run↔Plan cycle удалены, concrete wiring сосредоточен в bootstrap, runtime/evaluation не имеют hidden composition path, signal-aware context проходит до runtime, durable plan/host coordination использует store locks. Architecture gate проверяет acyclic private service dependencies и явный background lifetime. Shell test layer сокращён до одного TypeScript compiler smoke; остальные process/package assertions — bounded Go E2E. Решение: [`docs/68-architecture-hardening-v0.1.54.md`](docs/68-architecture-hardening-v0.1.54.md), ADR-087.

## Go-native test architecture v0.1.53

`v0.1.53-alpha` не добавлял продуктовых функций. Основной contract contour — стандартный `go test ./...`; black-box CLI/daemon/MCP/evaluation сценарии живут в `tests/e2e` и используют общий Go harness. Исторически в `v0.1.53` оставалось пять shell smoke tests; `v0.1.54` перенёс четыре из них в Go и оставил только TypeScript compiler smoke. Решение: [`docs/67-go-native-test-architecture-v0.1.53.md`](docs/67-go-native-test-architecture-v0.1.53.md), ADR-086.

## Application boundary refactor v0.1.52

`v0.1.52-alpha` не добавляет продуктовых функций. `cmd/takt` стал тонким launcher, stateful use cases перенесены в `internal/application`, concrete wiring — в `internal/bootstrap`, а daemon/MCP используют общий `internal/appapi` registry. Filesystem persistence подключается к application через `RunStore`; runtime создаётся через явные `Definition + Dependencies`.

Архитектурные границы проверяет `go test ./internal/architecture`; решение application boundary зафиксировано в [`docs/66-application-boundary-architecture-refactor-v0.1.52.md`](docs/66-application-boundary-architecture-refactor-v0.1.52.md), ADR-085. Тестовый контур переведён на Go в [`docs/67-go-native-test-architecture-v0.1.53.md`](docs/67-go-native-test-architecture-v0.1.53.md), ADR-086.

## Human-reviewed Skill/Block Learning Loop v0.1.51

Повторяющийся durable diagnostic fingerprint или успешный workflow fingerprint из нескольких Run можно превратить в proposal:

```bash
takt learn scan --workspace . --min-runs 2
takt learn propose --pattern diagnostic:sha256:... --kind skill \
  --name validation-recovery --benefit "reduce repeated validation failures"
takt learn review learn-... --decision accept --reason "reusable and scoped"
takt learn evaluate learn-... --report ./evaluation/benchmark.json
takt learn stage learn-...
```

Proposal фиксирует supporting Run IDs, immutable candidate SHA-256, human review и matrix evaluation provenance. `stage` создаёт только `.takt/learning/ready/<proposal-id>` и не устанавливает package/skill автоматически. Контракт: [`docs/65-human-reviewed-learning-loop-v0.1.51.md`](docs/65-human-reviewed-learning-loop-v0.1.51.md).

## Structured Task Sources v0.1.50

Внешняя задача может поступать не только как свободный `goal`, но и через trusted source adapter:

```yaml
task_sources:
  github:
    transport: process
    argv: [takt-github-task-source]
    env:
      GH_TOKEN: secret://GH_TOKEN
    timeout: 30s
```

```bash
takt task start --workspace . --profile code \
  --source github --source-ref acme/service#42
```

Source adapter нормализует issue/tracker/PRD/OpenSpec в Task с immutable `source.revision`, после чего запускается тот же Router/Dynamic Takt. Reference GitHub Issue adapter: `cmd/takt-github-task-source`, публичный SDK: `sdk/tasksource`. Подробности: [`docs/64-structured-task-sources-v0.1.50.md`](docs/64-structured-task-sources-v0.1.50.md).

## Reference adapters v0.1.49

Поставка содержит два provider-specific reference binaries поверх публичных SDK:

```bash
go build ./cmd/qwen-takt-adapter
go build ./cmd/takt-github-scm-adapter
```

`qwen-takt-adapter` использует Qwen Code headless `stream-json` и намеренно имеет узкий capability surface: session/message/usage без `tool_control`, selected skills/MCP или sandbox guarantee. `takt-github-scm-adapter` реализует neutral SCM operations через authenticated `gh` и reconcile marker. Примеры: [`examples/reference-adapters`](examples/reference-adapters), детали: [`docs/63-reference-external-adapters-v0.1.49.md`](docs/63-reference-external-adapters-v0.1.49.md).

## Быстрый старт

Сборка пользовательского CLI (Go сам загрузит зафиксированную YAML-зависимость из `go.mod`):

```bash
mkdir -p bin
go build -o bin/takt ./cmd/takt
```

Создание локального проекта:

```bash
./bin/takt init code --dir ./takt-demo --json
```

Минимальный workflow `takt-demo/workflow.yaml`:

```yaml
name: hello
nodes:
  - id: hello
    bash: printf 'hello from Takt'
    output_type: result
    output_mime: text/plain
```

Проверка и запуск:

```bash
./bin/takt validate ./takt-demo/workflow.yaml \
  --config ./takt-demo/.takt/config.yaml \
  --workspace ./takt-demo

./bin/takt run ./takt-demo/workflow.yaml \
  --config ./takt-demo/.takt/config.yaml \
  --workspace ./takt-demo \
  --json
```

Команда `run` вернёт `id`. Состояние, события и артефакты можно посмотреть без знания внутреннего формата `.takt`:

```bash
./bin/takt status <run-id> --workspace ./takt-demo --json
./bin/takt events <run-id> --workspace ./takt-demo --json
./bin/takt artifacts <run-id> --workspace ./takt-demo --json
```

Approval/retry/subworkflow проходят теми же Run APIs и входят в отдельный
`make journeys` gate. Для ежедневной проверки Takt используется быстрый
`make check` (компиляция всех Go-пакетов и короткие контракты, обычно менее
минуты); полный обычный/race прогон запускается через `make check-full`.

Experimental Dynamic Flow и tooling evaluation остаются доступными, но не являются обязательной частью этого stable quick start.

## Совместимость и граница structured JSON

`input.schema` и `output_format` используют версионированный `takt-schema-subset/v1`, а не полный JSON Schema. Точный список поддерживаемых keywords доступен машинно:

```bash
takt compatibility schema
```

Для внешних executors и интеграций compatibility разделена на session adapter, coding-agent host integration и domain adapter:

```bash
takt compatibility matrix
takt compatibility fields
takt compatibility check --config .takt/config.yaml
takt compatibility check --config .takt/config.yaml --live
takt compatibility check --config .takt/config.yaml --strict
```

`--live` выполняет доступный version/Describe probe, но не считается live host conformance. Bundled Pi/OpenCode host integrations остаются `guarded`, пока конкретная версия хоста не прошла отдельный conformance smoke.

## Локальное управление через MCP и daemon

Одноразовый stdio MCP:

```bash
takt mcp --surface agent --workspace . --config .takt/config.yaml
```

Фоновый локальный процесс:

```bash
takt daemon start --workspace .
takt run workflow.yaml --workspace . --daemon --json
takt events <run-id> --workspace . --daemon --follow
takt mcp --surface agent --workspace . --daemon
takt daemon stop --workspace .
```

По умолчанию `takt mcp` публикует безопасную agent surface из пяти `takt.task.*`. `--surface host|worker|operator|all` открывает отдельный протокол соответствующего потребителя; полная совместимая поверхность содержит 54 операции. `run.start` по умолчанию возвращает durable `run_id` после принятия запуска; состояние и поток событий читаются отдельными вызовами по revision cursor. Поддерживаются legacy initialization до `2025-11-25` и stateless discovery `2026-07-28`.

Прямой MCP и daemon используют тот же файловый store, locks, fingerprints, governed children и worktree lifecycle, что CLI. Daemon слушает только Unix socket текущего пользователя, переживает закрытие клиента, восстанавливает durable Run после потери локального executor PID и не является сетевым или многопользовательским сервером. Подробности: [Локальный MCP control plane v0.1.30](docs/44-local-mcp-control-plane-v0.1.30.md), [внешний executor v0.1.31](docs/45-agent-events-external-executor-v0.1.31.md) и [управляемые события и глубокие workflow v0.1.32](docs/46-controlled-agent-events-deep-workflows-v0.1.32.md), а также [authoring/daemon v0.1.33](docs/47-authoring-local-daemon-v0.1.33.md).

## Простой запуск задачи

```bash
takt task start "Исправь дефект и добавь регрессионную проверку" --workspace .
takt task respond <plan-id> --action go --workspace .
takt task status <plan-id> --workspace .
takt task explain <plan-id> --workspace .
```

Task Router выбирает готовый процесс, стабильный `simple-reliable` или bounded Dynamic Plan. Обычный шаблон выполняет исследование, изменение, проверки и независимое ревью; baseline, независимые тесты и усиленная проверка добавляются только по сигналам риска. Ошибка semantic router приводит к inspect-first fallback, а не к остановке задачи. Профиль обращается к логическому `coding-agent`; конкретный Pi, OpenCode, Codex, Oh My Pi, Qwen CLI или другой адаптер выбирается через `default_assistant`. Подробности: [v0.1.38](docs/52-simple-reliable-agent-neutral-router-v0.1.38.md), [v0.1.39](docs/53-role-brief-controls-v0.1.39.md), [v0.1.40](docs/54-evidence-baseline-failure-routing-v0.1.40.md) и [proposal](docs/proposals/001-simple-reliable-agent-neutral-takt.md).

## Dynamic Takt из основной сессии кодинг-агента

```bash
takt plan "Проверь совместимость MCP-инструментов" --workspace .
takt execute <plan-id> --workspace . --confirm
# Для фонового исполнения:
takt execute <plan-id> --workspace . --confirm --daemon
takt plan get <plan-id> --workspace .
takt steer <plan-id> "Сложные расхождения вынеси в отчёт" --workspace .
takt plan promote <plan-id> --name audit-mcp-compatibility --workspace .
```

`takt plan` выбирает готовый процесс либо создаёт ограниченный task-specific `WorkflowPlan` из явно подключённых доверенных пакетов блоков. План компилируется в обычный Takt Workflow: отдельные worker-сессии выбранного `coding-agent` выполняют governed child Run, а основная сессия кодинг-агента показывает preview, наблюдает события и передаёт approval/steering. Перепланирование происходит только в явных checkpoint и создаёт новую revision незавершённой части. MCP-инструменты: `takt.plan`, `takt.plan.get`, `takt.execute`, `takt.run.steer`, `takt.plan.promote`. Подробности: [Dynamic Takt v0.1.34](docs/48-dynamic-takt-v0.1.34.md).

## Автономные Run

```bash
takt runs --active --workspace . --daemon
takt attention --workspace . --daemon
takt run pause <run-id> --workspace . --daemon
takt run resume <run-id> --workspace . --daemon
takt run retry <run-id> --node validate --workspace . --daemon
takt run summary <run-id> --workspace . --daemon
takt notify list --unread --workspace .
```

Pause действует на безопасной границе: Takt не запускает новые узлы и новые партии fan-out, текущие попытки завершают границу узла, после чего root и child Runs переходят в `paused`. Daemon startup выполняет PID-based recovery потерянных локальных исполнителей; это новая attempt, а не продолжение того же OS/provider-процесса. Уведомления записываются в durable inbox и при необходимости доставляются через desktop или доверенный process sink. Подробности: [Autonomous Run Operations v0.1.37](docs/51-autonomous-run-operations-v0.1.37.md).

## Профиль code: 19 процессов, умный роутер и глубокие workflow

```bash
takt init code
takt workflow list code
takt workflow describe code:piv-loop
takt run code --input "Исправь issue #123 и создай PR"
takt run code:comprehensive-pr-review --input "Проверь текущий PR"
```

Запуск `code` без суффикса выполняет schema-validated router node в корневом Run и запускает выбранный процесс как отдельный governed child Run. Каталог включает assist, GitHub issue/PR процессы, PIV, PRD, Ralph, архитектурный анализ, безопасный рефакторинг, adversarial development, Remotion и разрешение конфликтов. Шесть основных процессов принимают строгий JSON input и требуют типизированные checkpoints, evidence, Git preparation, validation/recovery и предметные error codes. Подробности: [Каталог процессов v0.1.24](docs/38-archon-workflow-catalog-v0.1.24.md) и [углубление процессов v0.1.32](docs/46-controlled-agent-events-deep-workflows-v0.1.32.md).

## Композиция workflow

```yaml
name: composition
nodes:
  - id: implementation
    subworkflow:
      path: workflows/implementation.yaml
      inputs:
        plan: $ARGUMENTS

  - id: checks
    depends_on: [implementation]
    foreach:
      as: check
      items: [lint, test]
      subworkflow:
        path: workflows/check.yaml
        inputs:
          name: $INPUTS.check
```

`subworkflow` и `foreach` разворачиваются до запуска в обычный DAG, включая дочерний DAG `loop_group`. Публичные ID `implementation` и `checks` остаются доступными для зависимостей и шаблонов, а внутренние ID скрыты из CLI-состояния. `foreach` принимает inline `items` или `items_from.path`, поддерживает `parallel: true` и возвращает JSON-массив результатов в порядке элементов; Markdown-планы Takt не преобразует во внутренний список задач.

Рабочий пример: [`examples/composition/`](examples/composition/). Изменяющие процессы профиля `code` запускаются в управляемом Git worktree; состояние и артефакты остаются в исходном checkout.

Для отдельного жизненного цикла используется `workflow`:

```yaml
- id: feature
  workflow:
    path: workflows/feature-development.yaml
    input: $ARGUMENTS
    output_node: summary
```

Ребёнок получает собственный Run ID, state/events/artifacts и usage. Управление деревом:

```bash
takt children <run-id>
takt status <child-run-id>
takt cancel <run-id> --reason "остановлено пользователем"
```

Approval внутри ребёнка можно подтвердить через ID корневого Run и публичный ID `workflow`-узла. Подробности: [Governed child Runs v0.1.26](docs/40-governed-child-runs-v0.1.26.md).

Динамический fan-out управляемых детей задаётся внутри `workflow`:

```yaml
- id: reviews
  depends_on: [classify]
  workflow:
    path: workflows/review.yaml
    input: "Perspective: $FANOUT.item"
    isolation: inherit
    fan_out:
      items_from: $classify.output.reviewers
      as: reviewer
      max_parallel: 5
      join: all_success
```

Каждый элемент получает отдельный Run ID. Завершённые дети переиспользуются при resume, результат агрегируется в исходном порядке, а изменение массива блокирует небезопасное продолжение. Подробности: [Динамический fan-out v0.1.28](docs/42-governed-child-fanout-v0.1.28.md).

## Script-узлы и типизированные артефакты

```yaml
- id: build-index
  script:
    runtime: command
    path: tools/build-index
    dependencies: [schemas/index.schema.json]
  output_format:
    type: object
    properties:
      files:
        type: array
        items: {type: string}
    required: [files]
  output_type: plan-index
  output_mime: application/json
```

Поддерживаются `command`, `python`, `node` и `go`, file/inline source, args, env и working directory. Исходник и dependencies входят в fingerprint. `output_type` сохраняет Output либо `output_path` как снимок с checksum и producer metadata. Downstream-узлы используют `$build-index.artifacts.plan-index.path`.

```bash
takt artifacts <run-id>
takt artifacts <run-id> --type plan --recursive
```

Артефакты governed children поднимаются родителю без потери producer Run. Подробности: [Script-узлы и типизированные артефакты v0.1.29](docs/43-script-nodes-typed-artifacts-v0.1.29.md).


## Доверенные пакеты блоков

Профиль подключает пакеты явно через `block_packages`. Пакет содержит проверенные workflow-блоки и корпоративные ограничения: обязательные проверки, правила веток, шаблон change request, разрешённые интеграции, security policy и максимальные бюджеты.

```bash
takt block validate examples/corporate-block-package/package.yaml
takt block list --profile code --workspace .
takt block describe adversarial-verify --profile code --workspace .
```

Dynamic Takt сохраняет fingerprint каталога при preview. Изменение пакета или workflow блока блокирует execute/replan/promote и требует нового плана. Корпоративная политика сужает встроенные блоки; `map` использует только точно объявленный массив результата. Полный пример: [`examples/corporate-block-package/`](examples/corporate-block-package/).

## Скилл для настройки Takt

Каталог [`skills/takt/`](skills/takt/) содержит переносимый скилл для кодовых агентов. Он помогает:

- собирать `.takt/config.yaml`, workflow и Markdown-команды;
- выбирать assistant и model на уровне defaults, команды или узла;
- проектировать retry/feedback, hooks, approval, `loop_group`, `subworkflow`, `foreach` и governed `workflow`;
- использовать inline `prompt` и внешние команды;
- проверять профиль через `takt validate` и диагностировать ошибки;
- начинать с проверенного шаблона `skills/takt/assets/validated-agent-profile/`.

Основной файл скилла: [`skills/takt/SKILL.md`](skills/takt/SKILL.md).

## С чего продолжать разработку

Семантика runtime, process-протокол и специализированный Pi RPC adapter стабилизированы контрактными тестами. Воспроизводимый Route DSL end-to-end добавлен в `examples/route-dsl-e2e` и проверяется в полном контуре `make check-full`; быстрый `make check` оставляет E2E за пределами ежедневного шлюза.

### Переносимые пакеты

```bash
takt package install ./my-package --scope project
takt package doctor
takt package sync
```

Local/Git `BlockPackage` фиксируется lock-файлом и автоматически подключается к профилю. См. `examples/portable-package/` и `docs/56-portable-package-distribution-v0.1.42.md`.

Пакеты профилей, reusable `subworkflow`, параллельный DAG и оба режима `foreach` реализованы. Профиль `code` 0.19.3 содержит 19 процессов разработки, умный роутер с отдельным child Run и deterministic `plan-to-pr` gates по validation, Git scope, PR, review и summary, а `feature-development` — строгий outcome gate с одной repair/revalidation веткой. Интерактивные PIV/PRD-циклы возобновляют активную итерацию после approval, а структурированные классификаторы проверяются через `output_format`. Per-node политики инструментов, skills, MCP и assistant-enforced sandbox реализованы с проверкой возможностей adapter до запуска. Динамический fan-out дочерних Run реализован и используется smart/comprehensive review. Script-узлы и типизированные артефакты используются для review perspectives, планов и PRD. Локальная интеграция Takt через MCP реализована в v0.1.30-alpha; v0.1.31-alpha добавляет durable `executor: external`, v0.1.32-alpha завершает управляемый tool lifecycle и углубляет шесть основных workflow, v0.1.33-alpha добавляет строгий authoring preflight и локальный daemon, v0.1.34-alpha — Dynamic Takt и coding-agent flow, v0.1.35-alpha — доверенные корпоративные блоки и исправления бюджетов/исполнения, v0.1.36-alpha — host-control core и guarded Pi/OpenCode integrations, v0.1.37-alpha — автономные Run, attention, pause/recovery и уведомления, v0.1.38-alpha — нейтральный coding-agent, Task Router, simple-reliable template и role-based MCP surfaces, v0.1.39-alpha — Role Contract, bounded TaskBrief, required/preferred checks, deny/repair/warn и bounded automatic repair с исправлениями автономного control plane, v0.1.40-alpha — EvidenceManifest, baseline-aware failure classification, parking и reconciliation неизвестных external side effects, v0.1.41-alpha — нейтральную Adapter Platform для coding-agent, SCM, tracker и CI, v0.1.42-alpha — переносимую доставку пакетов с lock, dependency/capability preflight и source/signature policy, v0.1.43-alpha — multi-repo Dynamic Workflows с repository catalog, изолированными repo child Runs, per-repo evidence, neutral SCM publication и integration verification, v0.1.44-alpha — durable retry/backoff, diagnostic fingerprints, fan-out early termination, SecretRef/redaction, локальный OS sandbox для bash/script и NodePath, а v0.1.45-alpha — сравнительный Route DSL benchmark с matrix/repeat/compare/gates, true time-to-valid и стабильностью diagnostics. v0.1.46-alpha добавляет task-level Dynamic Takt benchmark и закрывает общий redaction-контракт external/control persistence, а v0.1.47-alpha начинает стабилизацию v0.2: first-class iteration history, bounded loop state, contract audit и draft migration policy к v1beta1, а v0.1.48-alpha фиксирует `takt-schema-subset/v1`, machine-readable field audit и adapter/host/domain compatibility matrix; v0.1.49-alpha впервые доказывает внешние seams реальными reference implementations `qwen-takt-adapter` и `takt-github-scm-adapter`, включая exact resume и reconcile неизвестного SCM side effect; v0.1.50-alpha добавляет Structured Task Sources: нормализованный внешний Task с immutable revision до Router/Dynamic Takt и reference GitHub Issue source; v0.1.51-alpha — human-reviewed Skill/Block Learning Loop с immutable candidate snapshot, обязательным review/evaluation и staging без автоматической установки, v0.1.52-alpha — application/bootstrap refactor, thin transports и architecture regression gate, v0.1.53-alpha — Go-native test architecture, а v0.1.54-alpha — architecture hardening private/acyclic application dependencies, explicit execution lifetime и единственный TypeScript shell smoke, а v0.1.55-alpha — modularization stable/experimental/extensions/tooling, upstream YAML и user-journey gate; релизы v0.1.52–v0.1.56 не добавляют продуктовых функций. Web UI, БД и удалённый многопользовательский server остаются proposal-направлением.

Evaluation runner фиксирует идентичность стратегии, корпуса, workspace и валидатора, execution identity каждой попытки, true time-to-valid и diagnostic fingerprints. `examples/route-dsl-benchmark` содержит 25 размеченных regression/production-shaped synthetic cases и matrix для сравнения стратегий. Live Go benchmark подтвердил Pi/Qwen 3.6 repair `14/15 → 15/15` и OpenCode/Qwen3-Coder-Next direct `15/15`, repair `13/15 → 15/15`; два OpenCode `GOFMT_FAILED` восстановлены exact resume. Сохранённый OpenCode/Qwen 3.6 direct/proxy evidence существенно хуже и не используется как текущий default. Предметный следующий этап — тот же measurement contract на реальном обезличенном corpus, когда он доступен. OpenCode adapter реализован и может использоваться вместо Pi на уровне defaults, Markdown-команды или отдельного узла.

Подробности:

- [Состояние реализации](docs/05-implementation-status.md)
- [Аудит и исправления v0.1.2](docs/16-audit-remediation-v0.1.2.md)
- [Дополнительная стабилизация v0.1.3](docs/17-audit-remediation-v0.1.3.md)
- [Классификация parent loop v0.1.4](docs/18-audit-remediation-v0.1.4.md)
- [Восстановление документации v0.1.5](docs/19-document-recovery-v0.1.5.md)
- [Fake-assistant contract suite v0.1.6](docs/20-fake-assistant-contract-v0.1.6.md)
- [Усиление protocol contract v0.1.7](docs/21-protocol-hardening-v0.1.7.md)
- [Pi RPC adapter v0.1.8](docs/22-pi-adapter-v0.1.8.md)
- [Согласование Pi RPC-контракта v0.1.9](docs/23-pi-rpc-alignment-v0.1.9.md)
- [Усиление context/usage Pi v0.1.10](docs/24-pi-context-usage-hardening-v0.1.10.md)
- [OpenCode adapter v0.1.19](docs/33-opencode-adapter-v0.1.19.md)
- [Диагностика provider-сбоев OpenCode v0.1.20](docs/34-opencode-provider-diagnostics-v0.1.20.md)
- [Route DSL end-to-end v0.1.11](docs/25-route-dsl-e2e-v0.1.11.md)
- [Evaluation runner v0.1.12](docs/26-evaluation-runner-v0.1.12.md)
- [Изоляция и диагностика evaluation v0.1.13](docs/27-evaluation-isolation-report-v0.1.13.md)
- [Идентичность benchmark и качество v0.1.14](docs/28-benchmark-identity-quality-v0.1.14.md)
- [Семантика метрик и execution identity v0.1.15](docs/29-benchmark-metric-semantics-v0.1.15.md)
- [Семантика validation envelope v0.1.16](docs/30-quality-envelope-semantics-v0.1.16.md)
- [Разделение stdout/stderr quality-node v0.1.17](docs/31-quality-stdout-separation-v0.1.17.md)
- [Скилл настройки Takt v0.1.18](docs/32-takt-authoring-skill-v0.1.18.md)
- [Композиция workflow v0.1.22](docs/36-workflow-composition-v0.1.22.md)
- [Усиление композиции v0.1.23](docs/37-composition-hardening-v0.1.23.md)
- [Каталог процессов и умный роутер v0.1.24](docs/38-archon-workflow-catalog-v0.1.24.md)
- [Git worktree isolation v0.1.25](docs/39-git-worktree-isolation-v0.1.25.md)
- [Governed child Runs v0.1.26](docs/40-governed-child-runs-v0.1.26.md)
- [Политики возможностей узлов v0.1.27](docs/41-node-capability-policies-v0.1.27.md)
- [Динамический fan-out governed child Runs v0.1.28](docs/42-governed-child-fanout-v0.1.28.md)
- [Script-узлы и типизированные артефакты v0.1.29](docs/43-script-nodes-typed-artifacts-v0.1.29.md)
- [Локальный MCP control plane v0.1.30](docs/44-local-mcp-control-plane-v0.1.30.md)
- [Доверенные пакеты блоков v0.1.35](docs/49-trusted-block-packages-v0.1.35.md)
- [Coding Agent Host Control v0.1.36](docs/50-coding-agent-host-control-v0.1.36.md)
- [Evidence, baseline и failure routing v0.1.40](docs/54-evidence-baseline-failure-routing-v0.1.40.md)
- [Role Contract, Brief Compiler и управляемые проверки v0.1.39](docs/53-role-brief-controls-v0.1.39.md)
- [Simple Reliable Router и нейтральные кодинг-агенты v0.1.38](docs/52-simple-reliable-agent-neutral-router-v0.1.38.md)
- [Proposal: простой и надёжный agent-neutral Takt](docs/proposals/001-simple-reliable-agent-neutral-takt.md)
- [Autonomous Run Operations v0.1.37](docs/51-autonomous-run-operations-v0.1.37.md)
- [Backlog v0.2](docs/14-backlog-v0.2.md)

## Документация

- [Краткие правила для кодовых агентов](AGENTS.md)
- [Скилл для настройки и использования Takt](skills/takt/SKILL.md)
- [Описание проекта](docs/01-project.md)
- [Подход к решению](docs/02-approach.md)
- [Текущая спецификация `takt/v1alpha1`](docs/03-specification.md)
- [Архитектура текущей реализации](docs/04-architecture.md)
- [Состояние реализации](docs/05-implementation-status.md)
- [Общий план развития](docs/06-roadmap.md)
- [Профиль совместимости с Archon](docs/07-archon-compatibility.md)
- [Целевое состояние v0.2](docs/08-target-v0.2.md)
- [Семантика runtime v0.2](docs/09-runtime-semantics.md)
- [Контракт адаптеров](docs/10-assistant-adapter-spec.md)
- [План реализации v0.2](docs/11-implementation-plan.md)
- [Карта источников истины](docs/12-document-map.md)
- [План оценки стратегий](docs/13-evaluation-plan.md)
- [Backlog v0.2](docs/14-backlog-v0.2.md)
- [Стартовая инструкция для кодового агента](docs/15-coding-agent-start.md)
- [Аудит и исправления v0.1.2](docs/16-audit-remediation-v0.1.2.md)
- [Дополнительная стабилизация v0.1.3](docs/17-audit-remediation-v0.1.3.md)
- [Классификация parent loop v0.1.4](docs/18-audit-remediation-v0.1.4.md)
- [Восстановление документации v0.1.5](docs/19-document-recovery-v0.1.5.md)
- [Fake-assistant contract suite v0.1.6](docs/20-fake-assistant-contract-v0.1.6.md)
- [Усиление protocol contract v0.1.7](docs/21-protocol-hardening-v0.1.7.md)
- [Pi RPC adapter v0.1.8](docs/22-pi-adapter-v0.1.8.md)
- [Согласование Pi RPC-контракта v0.1.9](docs/23-pi-rpc-alignment-v0.1.9.md)
- [Усиление context/usage Pi v0.1.10](docs/24-pi-context-usage-hardening-v0.1.10.md)
- [Route DSL end-to-end v0.1.11](docs/25-route-dsl-e2e-v0.1.11.md)
- [Evaluation runner v0.1.12](docs/26-evaluation-runner-v0.1.12.md)
- [Изоляция и диагностика evaluation v0.1.13](docs/27-evaluation-isolation-report-v0.1.13.md)
- [Идентичность benchmark и качество v0.1.14](docs/28-benchmark-identity-quality-v0.1.14.md)
- [Семантика метрик и execution identity v0.1.15](docs/29-benchmark-metric-semantics-v0.1.15.md)
- [Семантика validation envelope v0.1.16](docs/30-quality-envelope-semantics-v0.1.16.md)
- [Разделение stdout/stderr quality-node v0.1.17](docs/31-quality-stdout-separation-v0.1.17.md)
- [Скилл настройки Takt v0.1.18](docs/32-takt-authoring-skill-v0.1.18.md)
- [Композиция workflow v0.1.22](docs/36-workflow-composition-v0.1.22.md)
- [Усиление композиции v0.1.23](docs/37-composition-hardening-v0.1.23.md)
- [Каталог процессов и умный роутер v0.1.24](docs/38-archon-workflow-catalog-v0.1.24.md)
- [Git worktree isolation v0.1.25](docs/39-git-worktree-isolation-v0.1.25.md)
- [Governed child Runs v0.1.26](docs/40-governed-child-runs-v0.1.26.md)
- [Политики возможностей узлов v0.1.27](docs/41-node-capability-policies-v0.1.27.md)
- [Динамический fan-out governed child Runs v0.1.28](docs/42-governed-child-fanout-v0.1.28.md)
- [Script-узлы и типизированные артефакты v0.1.29](docs/43-script-nodes-typed-artifacts-v0.1.29.md)
- [Локальный MCP control plane v0.1.30](docs/44-local-mcp-control-plane-v0.1.30.md)
- [Граница безопасности](SECURITY.md)
- [JSON Schemas](schemas/README.md)

## Политики возможностей узла

AI-узлы поддерживают `allowed_tools`, `denied_tools`, `skills`, `mcp`, `sandbox` и `requires`. Явный `allowed_tools: []` означает запуск без инструментов. Adapter обязан объявить требуемые возможности до запуска; неподдерживаемая гарантия завершает узел до вызова модели. Политика сохраняется в состоянии, входит в fingerprint вместе с файлами MCP/skills и наследуется governed child Run как верхняя граница.

```yaml
- id: classify
  command: classify-change
  allowed_tools: []
  skills: []

- id: review
  command: review-code
  denied_tools: [edit, write]
  mcp: mcp/repository.json
  sandbox:
    filesystem: read_only
  requires: [tool_policy, mcp]
```

Filesystem/network policy текущей версии является assistant-enforced contract, а не OS sandbox. `process` получает policy через протокол и `TAKT_POLICY_JSON`; Pi/OpenCode применяют только реально поддерживаемые встроенные capabilities.

## Важная граница

Takt управляет процессом снаружи. Внутренний цикл инструментов, работа с файлами, MCP, LSP, история сообщений и сжатие контекста остаются ответственностью Pi, OpenCode или другого кодового агента.

## Готовый режим разработки по Markdown-плану

```bash
takt init code
takt validate code --workspace . --json
takt run code --workspace . --input docs/plan.md --json
```

Профиль `code` устанавливается в `.takt/profiles/code/`. Markdown-файл остаётся исходным планом; Takt передаёт агенту путь и содержимое без обязательного преобразования в отдельную структуру задач.
