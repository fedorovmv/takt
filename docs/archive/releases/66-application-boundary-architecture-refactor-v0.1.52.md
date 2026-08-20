# Application Boundary Architecture Refactor — v0.1.52-alpha

`v0.1.52-alpha` — архитектурный релиз без новых продуктовых возможностей. Его задача — остановить рост связности между CLI, MCP, daemon, control plane и runtime перед дальнейшим развитием Takt.

## Причина

К `v0.1.51` пользовательские функции работали, но композиция приложения стала слишком централизованной:

- `cmd/takt` содержал тысячи строк orchestration и напрямую собирал workflow/config/runtime;
- один `internal/control.Service` объединял Run, Dynamic Plan, Task, host, external worker и catalog use cases;
- MCP и daemon поддерживали собственные большие dispatch-switch и могли расходиться по defaults/validation;
- application/control код десятками создавал `store.FS` напрямую;
- `runtime.New` скрыто создавал concrete store/resolvers/adapters;
- крупные validator/action функции смешивали orchestration и реализацию конкретных действий.

Такой дизайн затруднял переиспользование, тестирование и безопасное добавление новых операций. Рефакторинг следует SOLID, DRY, KISS и YAGNI: границы становятся явными, но новый framework, DI-container, event bus, generic repository и plugin system не вводятся.

## Целевая направленность зависимостей

```text
cmd/takt
  |
  v
internal/cli                         transport parsing/output
  |
  v
internal/bootstrap                   production composition root
  |
  +--> internal/appapi               canonical local operation registry
  |
  +--> internal/application          use cases
           |
           +--> Run / Plan / Task / External / Host
           +--> Catalog / Authoring / Worktree / Command
           +--> Notification / Maintenance
           |
           v
       runtime / workflow / stores / adapters
```

MCP и daemon являются transport adapters той же application-модели:

```text
MCP ----+
        +--> appapi/application --> runtime
Daemon -+
```

## Что изменено

### 1. `cmd/takt` стал launcher

Production `cmd/takt` содержит только запуск `internal/cli`, печать ошибки и exit code. Architecture gate ограничивает его размер и набор imports.

CLI-команды перенесены в `internal/cli` и разделены по назначению: authoring, plan, admin/package, transport, evaluation, worktree, command, task, host, notifications и run operations.

CLI больше не создаёт `runtime.Runner`, `store.FS`, `notification.Dispatcher` и не вызывает evaluation/learning/package engines напрямую. Production `internal/cli` зависит только от application/bootstrap и transport clients; use-case semantics находятся за application boundary.

### 2. Монолитный `internal/control.Service` удалён

Control plane перенесён в `internal/application` и разделён на use-case services:

- `RunService` — lifecycle Run;
- `PlanService` — Dynamic Plan/replan/steering;
- `TaskService` — Task Source/Router entry point;
- `ExternalService` — external node/worker/tool lifecycle;
- `HostService` — host-control sessions/guards;
- `CatalogService` — workflow/block discovery;
- `AuthoringService` — workflow validation/preflight;
- `WorktreeService` — managed worktree operations;
- `CommandService` — durable execution Markdown commands;
- `NotificationService` — notification use cases;
- `MaintenanceService` — common background lifecycle;
- `EvaluationService` — evaluation/benchmark/report use cases через injected evaluation engine;
- `LearningService` — human-reviewed learning lifecycle;
- `CompatibilityService` — compatibility matrix/check operations;
- `AdapterService` — adapter discovery/doctor operations;
- `PackageService` — package install/update/remove/sync/doctor/sign operations.

Зависимости между use cases явные. Например, Task использует Run/Plan, Host — Plan, External — Run.

### 3. Один production composition root

`internal/bootstrap` собирает concrete зависимости:

- filesystem Run Store;
- command resolver;
- assistant resolver;
- domain adapter resolver;
- redactor;
- runtime Runner;
- application services;
- canonical API registry.

Application transports не должны самостоятельно повторять эту сборку.

### 4. Persistence инвертирован через consumer-owned port

Application использует `RunStore`, содержащий только операции, которые нужны Run/control use cases. Текущая реализация остаётся `store.FS` и по-прежнему хранит данные локально.

Это dependency inversion, а не подготовка абстрактной БД: database-backed store остаётся вне текущего scope.

### 5. Canonical operation registry вместо transport-specific business dispatch

`internal/appapi.Registry` содержит один набор именованных application operations и общий strict JSON decode/default contract.

Операции регистрируются в handler map по группам Task/Catalog/Host/Plan/Run/Notification. Daemon больше не имеет собственного business switch. MCP направляет общие операции в тот же registry; только MCP-specific worker/tool protocol и foreground `takt.execute` остаются transport-specific.

`run.start` сохраняет различие `detached` как явный параметр: отсутствие значения даёт daemon/API default, explicit `false` не теряется.

### 6. MCP получает узкие зависимости

Production MCP создаётся через `mcp.Dependencies`:

- canonical API;
- Plan service;
- External service;
- Maintenance service.

Он не получает весь application graph. Старый constructor сохранён только как repository compatibility wrapper для тестов/переходного периода.

### 7. Background lifecycle унифицирован

`MaintenanceService.Tick` выполняет один application-level lifecycle:

- advance Dynamic Plans;
- expire idle external executions;
- dispatch notifications.

Daemon и stdio MCP используют его одинаково вместо собственных частичных monitor loops.

### 8. Runtime получает явные зависимости

Добавлены:

```go
type Definition struct { ... }
type Dependencies struct {
    Commands   command.Resolver
    Store      store.Repository
    Assistants assistant.Resolver
    Adapters   domainadapter.Resolver
    Redactor   *redact.Redactor
}
```

Production создаёт Runner через `runtime.NewWithDependencies`. Внутренние зависимости `Runner` закрыты private-полями; application получает custom command resolver через `RunnerFactory` options вместо мутации готового Runner. Старый `runtime.New` остаётся compatibility helper внутри репозитория.

Scheduler, durable Run model и workflow semantics не изменены. Attempt lifecycle вынесен из scheduler в `attempt.go`: before/after hooks, timeout/cancel, retry disposition и successful commit разделены на небольшие функции без изменения durable semantics.

### 9. Закрытый action dispatch сохранён, реализация действий вынесена

Takt имеет конечный набор node actions, поэтому plugin framework не вводится. `Runner.execute` остаётся явным closed-world switch, а bash/script/approval/internal/adapter/assistant реализации вынесены в отдельные функции `actions.go`.

Это уменьшает ответственность scheduler-кода без искусственной абстракции.

### 10. Крупные validators разделены по ответственности

Монолитная `workflow.validateNodes` разложена на отдельные проверки action shape, composition, attempts, timing, assistant policy, sandbox, outputs, approvals, loops, dependencies и cycle detection. `config.Load` разделён на parsing и тематические validation helpers. Learning proposal validation разделена на identity/pattern/candidate/review+evaluation/status проверки. Error semantics сохранены regression tests.

### 11. Evaluation и tooling также проходят через application boundary

Evaluation, learning, compatibility, adapters и package distribution остаются самостоятельными engines/libraries, но transport не вызывает их напрямую. `EvaluationService` использует injected `EvaluationEngine`; task-matrix case execution получает application callback из bootstrap вместо создания нового application graph внутри evaluation package. Остальные tooling use cases также доступны через соответствующие application services.

Это сохраняет переиспользуемые библиотеки, но исключает второй application layer внутри CLI.

## Архитектурный gate

`internal/architecture` проверяют как минимум:

- отсутствие legacy `internal/control`;
- `cmd/takt` остаётся launcher и не разрастается;
- CLI не импортирует runtime/store/notification напрямую;
- application не зависит от transports/bootstrap/appapi;
- appapi не зависит от runtime/transports;
- MCP/daemon не обходят application через runtime/evaluation/notification;
- production CLI импортирует только разрешённые application/bootstrap/transport boundary packages;
- production application не создаёт concrete `store.FS`;
- runtime `Runner` не экспортирует изменяемые dependency fields.

Gate входит в `make check` и `scripts/verify.sh`. Рефакторинг дополнительно проверяется всем существующим набором repository contract/E2E scripts; transport/CLI совместимость не считается доказанной одной компиляцией.

## Что намеренно не сделано

В этом релизе не добавлены:

- новый node/plugin framework;
- DI framework/service locator;
- generic repository/ORM;
- event bus/CQRS;
- server/database/remote workers;
- новый workflow/API contract;
- новые P3-функции.

Evaluation, compatibility, package distribution и learning остаются самостоятельными локальными engines/libraries. Их orchestration проходит через application services; transport знает только request/response contract соответствующего use case. Это сохраняет библиотечную переиспользуемость без второго application layer.

## Совместимость

Рефакторинг не меняет:

- `takt/v1alpha1` workflow/config contracts;
- durable Run/state/event format;
- MCP tool names/surfaces;
- daemon public revision;
- Agent/Domain/Task Source protocols;
- evaluation/learning schemas;
- scheduler semantics.

Поэтому новая schema version не вводится.
