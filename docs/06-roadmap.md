# План развития Takt

Актуальный план после `v0.1.64-alpha`. История реализованных срезов находится
в `05-implementation-status.md` и документах
`docs/archive/releases/`. Текущие architecture/evaluation contracts остаются
в `docs/71–73`.

## Текущая позиция

Takt уже закрывает основной локальный control plane и в `v0.1.52–v0.1.64` прошёл application, test, architecture-hardening, modularization и unified-evaluation стабилизацию: workflow/runtime, child Runs и fan-out, worktree/multi-repo, Dynamic Takt, host control, autonomous operations, evidence/failure routing, adapters/packages, local security и сравнительный evaluation.

Главный риск теперь — продолжать добавлять механизмы быстрее, чем появляется evidence их пользы. Поэтому ближайший порядок меняется с feature-driven на evidence-driven.

## P-1. Architecture quality — выполнено в v0.1.52 и hardened в v0.1.54

Перед продолжением feature work проведён feature-freeze refactor:

- thin `cmd/takt` и разделённый CLI transport;
- application services вместо `control.Service`;
- один production `bootstrap` composition root;
- canonical daemon/MCP operation registry;
- consumer-owned RunStore port;
- explicit runtime dependencies;
- decomposition node actions/workflow validation;
- executable architecture import gate.

Новые core-функции снова допускаются только через эти границы; architecture gate является обязательной частью `make check`.

## P-0.5. Test architecture — выполнено в v0.1.53 и hardened в v0.1.54

После production refactor product correctness принадлежит Go tests, black-box проверки живут в `tests/e2e`, subprocesses bounded. В `v0.1.54` shell ограничен единственным TypeScript compiler smoke. Architecture gate контролирует эту границу. Детали — `docs/archive/releases/67-go-native-test-architecture-v0.1.53.md`, `docs/archive/releases/68-architecture-hardening-v0.1.54.md`, ADR-086/087.

## P-0.25. Core modularization — выполнено в v0.1.55

Stable core отделён от `experimental`, `extensions` и `tooling` односторонними import boundaries. Dynamic Flow остаётся одним из экспериментальных способов интеграции с coding agents, evaluation/compatibility вынесены из runtime graph, Package/Block/Notification оформлены как extensions. Самописный YAML parser заменён upstream library, добавлен отдельный user-journey release gate. Детали — `docs/archive/releases/69-core-stabilization-modularization-v0.1.55.md`, ADR-088.

## P-0.1. Codebase hygiene — выполнено в v0.1.56

Commodity JSON Schema execution вынесен в upstream library, Pi/OpenCode отделены от stable assistant core, fake binaries — от product commands, внешний worker/tool lifecycle выделен в `internal/externalworker`, а оставшиеся стабильные orchestration hotspots разложены на фазы. После этого общий архитектурный refactor считается закрытым; дальнейшие изменения должны исходить из реальных user/live scenarios. Детали — `docs/archive/releases/70-codebase-hygiene-stabilization-v0.1.56.md`, ADR-089.

## P-0.05. Architecture contracts — выполнено в v0.1.57

Перед переходом к user stabilization закреплены три ограничения эволюции: workflow-language constitution, immutable extension registrations через единственный bootstrap и schema-first canonical operations для appapi/MCP/docs. Это не новый framework и не feature slice; правила защищают уже очищенные границы от повторного разрастания. Текущий контракт описан в `docs/04-architecture.md` и ADR-090.

## P0. User stabilization

До promotion новых контрактов приоритет имеет основной пользовательский путь stable core:

- сборка/установка и `init` на поддерживаемых ОС;
- `validate -> run -> status/events/artifacts`;
- approval/answer, failure/retry и reusable composition;
- понятные диагностики и backward compatibility существующих Workflow/Run контрактов;
- live coding-agent/adapter сценарии через stable APIs.

`make journeys` закрепляет минимальный black-box baseline. Новые проблемы из реального использования исправляются раньше расширения feature surface.

### Pi-first policy

Pi является наиболее отлаженным assistant path со статусом beta для текущего
развития flow. Ближайший release gate проверяет flow-функции на pinned Pi:
основной run lifecycle, approval/answer, retry/recovery, governed child/matrix
и deterministic evaluation. OpenCode, Qwen Code и другие assistants остаются
alpha/reference и не являются ближайшим release gate; их углубление начинается
только после Pi-first flow evidence и отдельного use case.

GitHub CI запускает `make check`, `make journeys` и deterministic
`make compatibility-contract` на `ubuntu-latest` и `macos-latest`; live model и
host conformance остаются отдельным credentialed gate.

## P0.5. Evidence для experimental/tooling

### 1. Live Route DSL benchmark on Pi

Первый внешний gate перед стабилизацией: реальные обезличенные задачи + штатный validator + реальные модели/агенты. `v0.1.45` уже предоставляет matrix/repeat/compare/gates; synthetic corpus остаётся только regression fixture.

### 2. Task-level Dynamic Takt benchmark — v0.1.46

`v0.1.46-alpha` добавляет воспроизводимую проверку полного пути:

```text
Task → Router → template/dynamic → checkpoint → replan → result
```

Benchmark отдельно измеряет route accuracy, terminal success, plan revisions, replanner runs, unexpected needs-input, router fallback и usage. Это позволяет отличать «задача как-то завершилась» от «Takt выбрал и адаптировал правильный процесс».

### 3. Go + Document production scenarios on Pi

Тем же evaluation-подходом доказать исходную универсальность Takt на Go-разработке и подготовке технического документа.

## P1. v0.2 Stabilization

Stabilization начат в `v0.1.47` и продолжен в `v0.1.48`, но `v1beta1` не замораживается до production evidence.

В `v0.1.47` уже выполнены:

- первичный audit внешних contracts (`stable-candidate | supported-alpha | deprecated | internal`);
- first-class bounded iteration history;
- решение оставить nested `loop_group` запрещённым в `v0.2`;
- draft migration policy `v1alpha1 → v1beta1`.

В `v0.1.48` выполнены:

- текущий structured contract заморожен как `takt-schema-subset/v1`, без обещания полного JSON Schema;
- добавлены `compatibility matrix|check` для session/host/domain seams;
- добавлен machine-readable field-by-field audit stable-candidate API;
- зафиксирована deprecation boundary process `v1alpha1` → целевой `v1alpha2` для новых wrappers.

Остаётся после production evidence:

- финальная migration `v1alpha1 → v1beta1` по фактически используемым полям;
- при необходимости migrator и cleanup deprecated compatibility fields;
- live host/external seam evidence, которое может уточнить supported-alpha части до freeze.

Цель v0.2 — не максимальное число функций, а небольшой набор доказанных стабильных контрактов.

## P2. External seams

P2 продолжен в `v0.1.50-alpha`. Уже реализованы:

- reference Qwen Code wrapper через `sdk/agentadapter`;
- reference GitHub SCM adapter через `sdk/domainadapter`;
- execution `workspace` как нейтральный domain-request context;
- conservative process-v1alpha2 capability negotiation без неявного `tool_control`;
- Structured Task Sources: внешний issue/tracker/PRD contract → normalized Task → существующий Router/Dynamic Takt;
- reference GitHub Issue source через public `sdk/tasksource`.

Остаётся основной внешний gate:

- live Pi host conformance на pinned version.

OpenCode/Qwen и прочие reference-adapter smoke остаются deferred до Pi-first
flow gate; их исторические fixtures не повышают stability status.

Дополнительные tracker/OpenSpec/PRD source adapters являются integration-level расширениями уже проверенного `takt-task-source/v1alpha1`, а не новой core-фичей.

Reference implementations живут поверх публичных SDK/контрактов; provider-specific логика не переносится в runtime.

## P3. Learning и UX

P3 открыт в `v0.1.51-alpha`. Реализован первый самостоятельный срез:

- Run history → repeated fingerprint → immutable candidate skill/block snapshot;
- human review с rationale;
- существующий matrix evaluation + regression gates;
- staged ready candidate без автоматической установки trusted package/skill.

Следующие P3-срезы:

- `workflow graph/explain/scaffold` и расширенный `plan explain`;
- bounded reject/revise для статических approvals.

Learning loop сохраняет trust boundary: принятие и evaluation обязательны, а активация staged candidate остаётся отдельным явным действием.

## Отложено

Server, Web UI, БД, remote workers, Slack/Telegram и multi-user authorization не входят в локальный v0.2. Они рассматриваются только при появлении нелокального сценария и отдельной threat model.
