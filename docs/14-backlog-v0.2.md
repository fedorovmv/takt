# Backlog Takt v0.2

Статус обновлён после добавления идентичности benchmark и предметных метрик качества в `v0.1.14-alpha`.

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

### TAKT-010. Session resume — выполнено для process и Pi

**Цель:** сравнивать fresh и продолженную сессию.

**Результат:** сохранение Session ID; явное поле `resumed`; ошибка при неуспешном resume без тихого fallback.

**Приёмка:** fresh, resume success и resume failure реализованы для process protocol; подтверждены на Pi adapter; OpenCode получит тот же контракт при реализации.

## Завершено в v0.1.11–v0.1.14-alpha

### TAKT-011. Route DSL end-to-end — контрактный срез выполнен

**Цель:** заменить mock в основном сценарии и доказать управляющий контур.

**Результат:** Pi → validator → feedback → retry/resume → success → artifacts → approval.

**Приёмка:** сквозной CLI-тест требует двух попыток; вторая попытка использует сохранённый Session ID и диагностику первой проверки; success определяется только валидатором; Run завершается после отдельного `takt answer`.

### TAKT-011B. Производственная проверка Route DSL — выполняется

В `v0.1.12-alpha` реализован evaluation runner; в `v0.1.13-alpha` закрыты коллизии `case_id`, пересечение путей и потеря resume/feedback/diagnostics. В `v0.1.14-alpha` добавлены strategy/benchmark/workspace/validator fingerprints, assistant/version/requested/resolved model, `takt-validation/v1alpha1` и агрегаты success@1/final success/score/cost per valid.

Остаётся:

- выполнить baseline со штатным `route-tool` и реальным Pi на десяти обезличенных заданиях;
- расширить предметные checks валидатора семантикой требований;
- учитывать manual corrections результата;
- добавить сравнение нескольких report.json в CLI или внешний dashboard.

## Далее

### TAKT-012. Строгий template renderer

Неизвестная переменная вызывает ошибку; предусмотрены optional/default values.

### TAKT-013. Команда `takt cancel`

Идемпотентная отмена running/waiting Run и событие `run.cancelled`.

### TAKT-014. Capability contract

Типизированные capabilities, `requires` и проверка до запуска.

### TAKT-015. Нормализованные diagnostics

Общий формат `code/path/line/message`, deduplication и fingerprint ошибки.

### TAKT-016. Изоляция iteration state

Отдельная структура для истории всех итераций, а не только `LoopPrevious`.

### TAKT-017. Structured outputs

JSON output и JSON Schema validation.

### TAKT-018. Go workflow

Issue/fix → coding agent → `go test` → feedback → approval без изменения runtime.

### TAKT-019. Document workflow

Draft → approval comment → revise → artifact без изменения runtime.

### TAKT-020. Eval metrics — основной JSON-контур реализован

`takt eval run/report` собирает идентичность стратегии/benchmark/workspace/валидатора, assistant/version/requested/resolved model, status, attempts, duration, usage, approvals, diagnostics и предметное качество. Реализованы success@1, final success, average score, attempts/cost/time per valid. Остаются CLI-сравнение нескольких отчётов, экспорт таблиц и manual corrections.

## Вне v0.2

- parallel DAG;
- SQLite/Postgres;
- MCP server;
- Web UI;
- remote workers;
- untrusted/server mode;
- sandbox и многопользовательская авторизация.
