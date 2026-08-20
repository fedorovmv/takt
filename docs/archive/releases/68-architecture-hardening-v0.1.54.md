# Architecture Hardening — v0.1.54-alpha

`v0.1.54-alpha` продолжает feature freeze после ADR-085/086. Релиз не добавляет продуктовых возможностей и не меняет внешние Workflow/Config/Run/MCP/adapter contracts. Цель — довести application/runtime/test boundaries до фактического состояния, заявленного архитектурой.

## Что изменено

### Application dependencies

Общий `application.Context` удалён. Каждый application service хранит только свои private dependencies. Production infrastructure (`store.FS`, Dynamic Plan/Host stores, notification/learning/package backends, adapter factories) создаётся в `internal/bootstrap`, а application получает consumer-owned ports.

`RunService ↔ PlanService` больше не образуют цикл. Междоменный fork вынесен в отдельный `ForkService`; `RunService` знает только `PlanStore`, необходимый для согласования статуса owning plan. Architecture test строит граф зависимостей `*Service` и отклоняет циклы.

### Runtime composition

Production runtime имеет только `NewWithDependencies`. Default production constructor/factory удалён; child workflows наследуют уже собранные dependencies родительского Runner. Test-only convenience wiring находится в `internal/testsupport`/`*_test.go` и не образует второй production composition root.

Evaluation также получает execution factory через bootstrap и не создаёт `runtime`/`store.FS` самостоятельно.

### Cancellation and durable background work

`cmd/takt` создаёт signal-aware root context и передаёт его через CLI → application → runtime. Foreground operations, включая cancel/pause/abandon/promote, используют caller context.

Durable detached execution оформлено явно через `detachedContext`: cancellation transport-а не прерывает уже принятую durable операцию, но context values сохраняются. `context.Background()` внутри production application централизован в одном документированном helper только для recovery/reconciliation без живого request context.

### Coordination

Process-local `dynamicMu`/`hostMu` удалены. Dynamic Plan и Host Control используют durable store locks, которые одинаково работают для нескольких CLI/daemon/MCP процессов. Долгие foreground plan operations больше не держат глобальный process mutex.

### State-machine decomposition

Сложные orchestration paths разделены по фазам без введения plugin framework:

- task response разделён на plan/run/answer use cases;
- Dynamic Plan advance разделён на reconciliation, result capture, boundary handling и переход к следующему segment;
- child fan-out разделён на prepare, link/resume, batching, progress и join/finalize.

Closed-world node dispatch сохраняется: KISS/YAGNI важнее абстрактной extensibility без реального use case.

### Canonical operations

`internal/appapi` публикует canonical operation descriptors и MCP mapping использует тот же operation identity. Это убирает отдельную таблицу соответствия daemon/application ↔ MCP. Transport-specific title/schema/annotations остаются в MCP, потому что являются metadata транспорта, а не application contract.

### Test boundary

Black-box package distribution, reference adapters, host control и deep code workflow перенесены в Go E2E harness. `scripts/test-*.sh` теперь содержит только один TypeScript compiler smoke — единственную проверку, где внешняя языковая toolchain является предметом теста.

Go E2E subprocesses имеют bounded timeout по умолчанию. Shell smoke не содержит Python/grep assertions.

## Исполняемые архитектурные инварианты

`go test ./internal/architecture` проверяет:

- thin `cmd/takt` и transport import boundaries;
- отсутствие `internal/control`;
- отсутствие concrete infrastructure construction в application/evaluation;
- private runtime/application dependencies;
- отсутствие циклов между application services;
- отсутствие shared `application.Context` и process-global Dynamic/Host mutexes;
- единственный production runtime composition path;
- propagation caller context и единственный явный durable background helper;
- ровно один shell `test-*.sh` — TypeScript compiler smoke.

## Что намеренно не делалось

Не вводились DI container, service locator, generic repository, event bus, node plugin framework, отдельная БД или package-per-use-case иерархия. Application остаётся одним Go package, но сервисы имеют private narrow dependencies и acyclic graph. Физическое дробление package будет оправдано только при появлении независимой reusable boundary, а не ради уменьшения числа строк в каталоге.

## Совместимость

Внешний функциональный контракт этого релиза совпадает с `v0.1.53-alpha`. Изменения signatures относятся к internal application layer. Existing Go unit/component/E2E suites и published schemas остаются regression gate.
