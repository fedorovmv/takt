# Backlog Takt v0.2

Статус обновлён для `v0.1.26-alpha`: реализованы параллельные DAG-волны, `foreach.parallel`, проверяемый `output_format`, именованные workflow профиля, approval внутри цикла и каталог из 19 процессов с умным роутером.

## Завершено в v0.1.2–v0.1.4-alpha

### TAKT-001. Классификация execution errors — выполнено

- `exit`, `start`, `timed_out`, `cancelled`, `protocol`, `internal`;
- отдельные Node statuses;
- JSON error envelope;
- `allow_failure` только для exit code.

### TAKT-002. Failure propagation и `all_done` — выполнено

- Run не останавливается на первом failed node;
- `all_done` выполняется;
- недоступные ветви становятся `skipped` или `blocked`;
- итог Run вычисляется после DAG.

### TAKT-003. Единый scheduler root/loop — выполнено

- `when`, `trigger_rule`, hooks и attempts работают одинаково;
- добавлены contract tests.

### TAKT-004. Fingerprints и безопасный approval resume — выполнено

- SHA-256 workflow/config/commands;
- lock Run;
- проверка определений до потребления answer;
- команда `takt resume`.

### TAKT-005. Persistence revisions — выполнено

- обязательный `Store.Commit`;
- одинаковая revision state/event;
- обнаружение рассогласования;
- ошибки persistence не игнорируются.

### TAKT-006. YAML block scalar — выполнено

- сохранение пустых строк;
- `|`, `|-`, `|+`, `>`, `>-`, `>+`;
- строгий documented subset.

### TAKT-007. Timeout и output limit process adapter — выполнено частично

- timeout всей попытки, включая portable hooks;
- process output limit;
- thread-safe общий budget stdout/stderr;
- output truncation flag;
- Unix process-group cancellation;
- regression tests timeout/cancel по фазам hook.

Остаётся проверить grace period и platform-specific поведение на целевых ОС.

### TAKT-007A. Безопасная семантика loop state — выполнено для v1alpha1

- вложенные `loop_group` запрещены валидатором, схемой и runtime;
- runtime не перезаписывает существующий NodeState дочерним ID;
- `until` требует статус `completed`;
- namespace для вложенных циклов остаётся будущей задачей.

### TAKT-007B. Классификация parent loop timeout/cancel — выполнено

- attempt context проверяется до преобразования ошибок контейнера;
- timeout child сохраняется как `timed_out` у parent `loop_group`;
- внешняя cancellation сохраняется как `cancelled` у parent Node и Run;
- добавлены отдельные регрессии timeout и cancellation.

## Завершено в v0.1.6-alpha

### TAKT-008. Fake assistant protocol suite — выполнено

**Цель:** проверить нормализованный контракт до реального Pi/OpenCode.

**Результат:** тестовый бинарник поддерживает:

- success;
- exit N;
- start/invalid protocol;
- timeout;
- большой stdout/stderr;
- session ID;
- resume success;
- resume rejected;
- malformed structured result.

**Приёмка:** process adapter проходит contract cases success, exit, start, timeout, cancel, concurrent output, malformed result, fresh, resume и resume rejection. Suite включён в `scripts/verify.sh`; будущий specialized adapter обязан переиспользовать эти контракты.

## Завершено в v0.1.8–v0.1.11-alpha

### TAKT-009. Specialized Pi adapter — выполнено

**Цель:** проверить реальное выполнение агентного узла.

**Результат:** `type: pi` через официальный RPC mode, fake-Pi suite и opt-in real smoke.

**Приёмка:** provider/model/thinking, fresh/resume, timeout/cancel, output limit, приоритет context над совпавшим overflow, `agent_settled`, автоматический retry, полный deny-list зарезервированных флагов, fire-and-forget UI и строгая per-attempt usage delta покрыты тестами. Реальный smoke остаётся opt-in, так как требует установленного Pi, авторизации и модели.

### TAKT-010. Session resume — выполнено для process, Pi и OpenCode

**Цель:** сравнивать fresh и продолженную сессию.

**Результат:** сохранение Session ID; явное поле `resumed`; ошибка при неуспешном resume без тихого fallback.

**Приёмка:** fresh, resume success и resume failure реализованы для process protocol; подтверждены на Pi и OpenCode adapters.

## Завершено в v0.1.11–v0.1.16-alpha

### TAKT-011. Route DSL end-to-end — контрактный срез выполнен

**Цель:** заменить mock в основном сценарии и доказать управляющий контур.

**Результат:** Pi → validator → feedback → retry/resume → success → artifacts → approval.

**Приёмка:** сквозной CLI-тест требует двух попыток; вторая попытка использует сохранённый Session ID и диагностику первой проверки; success определяется только валидатором; Run завершается после отдельного `takt answer`.

### TAKT-011B. Производственная проверка Route DSL — выполняется

В `v0.1.12-alpha` реализован evaluation runner; в `v0.1.13-alpha` закрыты коллизии `case_id`, пересечение путей и потеря resume/feedback/diagnostics. В `v0.1.14-alpha` добавлены strategy/benchmark/workspace/validator fingerprints, assistant/version/requested/resolved model, `takt-validation/v1alpha1` и агрегаты success@1/final success/score/cost per valid. В `v0.1.15-alpha` добавлены per-attempt execution records, раздельная атрибуция usage, mixed identity и явные `0`/`null`. В `v0.1.16-alpha` validation envelope сохраняется независимо от exit code, а успех определяется только как `completed && valid=true`.

Остаётся:

- выполнить v0.1.45 matrix со штатным `route-tool`, configured coding-agent/model и отдельным реальным обезличенным corpus;
- расширить предметные checks валидатора семантикой требований;
- учитывать manual corrections результата;
- добавить сравнение нескольких report.json в CLI или внешний dashboard.

## Далее

### TAKT-012. Строгий template renderer

Неизвестная переменная вызывает ошибку; предусмотрены optional/default values.

### TAKT-014. Capability contract

Типизированные capabilities, `requires` и проверка до запуска.

### TAKT-015. Нормализованные diagnostics — реализовано в v0.1.44-alpha

Execution errors сохраняют `code/kind/op/message/fingerprint/retryable`; fingerprint нормализует workspace и volatile process numbers. `NodeState.path` даёт устойчивую структурную привязку узла. Более предметные `path/line` остаются данными конкретного validator/adapter, а не извлекаются эвристически из любого stderr.

### TAKT-016. Изоляция iteration state

Отдельная структура для истории всех итераций, а не только `LoopPrevious`.

### TAKT-017. Structured outputs

JSON output и JSON Schema validation.

### TAKT-018. Go workflow

Issue/fix → coding agent → `go test` → feedback → approval без изменения runtime.

### TAKT-019. Document workflow

Draft → approval comment → revise → artifact без изменения runtime.

### TAKT-020. Eval metrics — основной JSON-контур реализован

`takt eval run/report/benchmark/compare` собирает идентичность стратегии/benchmark/workspace/валидатора, per-attempt assistant/version/requested/resolved model, status, attempts, duration, usage, approvals, diagnostics и предметное качество. В v0.1.45 добавлены настоящий time-to-valid, matrix/repeat, парное CLI-сравнение, category breakdown, failed-execution cost, diagnostic fingerprints/stability и regression gates. Остаются экспорт табличных форматов, ручная разметка production corpus и task-level benchmark полного Dynamic Takt.

## Вне локального v0.2

- SQLite/Postgres;
- Web UI и server API;
- remote workers и message adapters;
- untrusted/multi-user mode;
- sandbox и многопользовательская авторизация.


### TAKT-009B. Specialized OpenCode adapter — выполнено

Реализован `type: opencode` через `opencode run --format json`: model/agent/variant mapping, fresh/resume, version, usage/cost, output limit, context priority, fake contract suite и opt-in smoke.


## Завершено в v0.1.24-alpha

### TAKT-020. Параллельные DAG-волны и foreach — выполнено

- независимые простые узлы выполняются конкурентно;
- persistence остаётся сериализованным;
- `foreach.parallel` собирает результаты в порядке входа;
- добавлены race и timing regressions.

### TAKT-021. Структурированный routing contract — выполнено

- `output_format` для `command` и `prompt`;
- проверка одного JSON-значения, типов, required, enum и additionalProperties;
- JSON-пути в шаблонах и `when`;
- protocol error при нарушении схемы.

### TAKT-022. Интерактивные циклы — выполнено

- approval разрешён внутри `loop_group`;
- resume продолжает активную итерацию;
- следующая итерация запрашивает новый ответ.

### TAKT-023. Каталог процессов code — выполнено

- 19 именованных workflow;
- умный роутер как root Run и выбранный процесс как governed child Run;
- `workflow list/describe`;
- reusable full/smart review blocks.

## Завершено в v0.1.21–v0.1.23-alpha

### TAKT-014. Пакеты профилей и композиция workflow — выполнено

- `takt init/validate/run <profile>`;
- встроенный профиль `code`, сохраняющий Markdown-план исходным документом;
- reusable `subworkflow` с inputs и публичным output;
- последовательный `foreach` по inline-списку и внешнему YAML/JSON-массиву;
- композиция внутри `loop_group`;
- публичная проекция Run и JSON-массив результатов итераций;
- локальные команды подключённого workflow входят в скомпилированное определение;
- fingerprints учитывают подключённые workflow и блокируют resume после их изменения;
- отдельный composition contract suite.

Остаются отдельные задачи параллельного `foreach`, групповых attempts/timeout/hooks и input adapters для OpenSpec, issue и других структурированных источников. Явный YAML/JSON-массив уже поддерживается через `items_from`; основной Markdown-режим сохраняется.


## Завершено в v0.1.25-alpha

### TAKT-023. Managed Git worktree isolation — выполнено

- workflow policy and CLI overrides;
- separate branch and execution workspace;
- router-aware activation at selected subworkflow gate;
- safe retain/remove lifecycle and management commands;
- persisted isolation state and resume semantics.

## Завершено в v0.1.26-alpha

### TAKT-024. Governed child Runs и cancellation tree — выполнено

- отдельные Run ID, state/events/artifacts/output/usage;
- parent/child links и history child attempts;
- `workflow` node с `input`, `output_node` и `isolation`;
- approval через root Run и каскадный resume parent chain;
- `takt children` и durable `takt cancel`;
- cancellation активного процесса и ожидающего дерева;
- новый child Run на retry;
- recursive fingerprints, recursion/depth checks;
- smart router и review blocks переведены на governed children;
- contract suite включён в `make check`.

## Завершено в v0.1.27–v0.1.29-alpha

### TAKT-025. Политики возможностей узлов — выполнено

Per-node tool allow/deny, skills, MCP, assistant-enforced sandbox policy, capability negotiation, persistence, fingerprints и child inheritance.

### TAKT-026. Dynamic governed fan-out — выполнено

Runtime list from structured output, child Run per item, max_parallel, resume, ordered aggregation, join policies and cancellation.

### TAKT-027. Script nodes и typed artifacts — выполнено

Runtime `command|python|node|go`, source/dependency fingerprints, structured output, `output_type`/MIME/SHA-256/producer metadata, CLI и parent/child propagation.

## Завершено в v0.1.30-alpha

### TAKT-028. Локальный MCP control plane — выполнено

- `takt mcp` по stdio без второго runtime и отдельной БД;
- legacy initialize и stateless server/discover;
- workflow discovery и полный Run lifecycle tools;
- detached start, revision events, children, typed artifact content;
- strict schemas, request cancellation, unit/lifecycle/process contracts;
- roadmap перенесён в архив `docs/44-local-mcp-control-plane-v0.1.30.md`.


## Следующий крупный срез

### TAKT-029. Агентные события и внешний исполнитель

- единая модель assistant/tool-call events в state/event log;
- внешний исполнитель одного узла без передачи ему orchestration semantics;
- связь событий с attempt, assistant session и usage;
- инструкции подключения MCP для основных coding-agent hosts;
- optional daemon рассматривается только как отдельное решение для переживания закрытия MCP-клиента.


## Завершено в v0.1.31-alpha

- конкурентно безопасное чтение file store и indexed event cursor;
- normalized assistant/tool events;
- durable external executor одного command/prompt узла через MCP claim/lease/token;
- устранение известных fan-out и static composition gaps.


## Завершено в v0.1.32-alpha

### TAKT-030. Controlled agent event protocol — выполнено

- session started/resumed;
- tool requested/allowed/denied/started/completed;
- blocking policy/approval и отмена отдельного tool call;
- artifact declaration с `call_id`;
- usage/diagnostic/terminal events;
- capability declaration и process protocol v1alpha2;
- MCP worker plane из 22 инструментов.

### TAKT-031. Deep core workflows — выполнено

- строгие JSON-входы для шести основных процессов;
- специализированные команды и обязательные checkpoint artifacts;
- domain error codes;
- Git decision trees;
- validation/recovery/revalidation;
- настоящий локальный Git repository, bare remote, fake `gh`;
- успешный и recovery E2E.

## Завершено в v0.1.33-alpha

### TAKT-032. Authoring preflight and strict renderer — выполнено

- path-aware `did you mean` для неизвестных полей;
- capability validation в `takt validate`;
- output/artifact reference diagnostics и incompatible-parameter hints;
- расширенный schema subset;
- `${path}`, `${path?}`, `${path:-default}`;
- `always_run` и `idle_timeout`.

### TAKT-033. Local filesystem daemon — выполнено

- `takt daemon start|status|stop|serve`;
- Unix socket, metadata, log и single-workspace lock;
- background Runs, event subscriptions и MCP proxy;
- несколько локальных clients и bounded control locking;
- external worker idle monitor без БД.

## Активные крупные направления после v0.1.33-alpha

1. Runtime security hardening, secret protection, retry backoff и fan-out early exit.
2. Реальный Route DSL benchmark.
3. Опциональное расширение daemon recovery после отдельного crash/restart design.
