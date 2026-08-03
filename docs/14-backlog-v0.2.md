# Backlog Takt v0.2

Статус обновлён после реализации fake-assistant protocol suite в `v0.1.6-alpha`.

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

## Следующий этап

### TAKT-009. Specialized Pi или OpenCode adapter

**Цель:** проверить реальное выполнение агентного узла.

**Результат:** один adapter по `10-assistant-adapter-spec.md`.

**Приёмка:** fake suite и opt-in smoke test с реальным бинарником.

### TAKT-010. Session resume — базовый process-контракт выполнен

**Цель:** сравнивать fresh и продолженную сессию.

**Результат:** сохранение Session ID; явное поле `resumed`; ошибка при неуспешном resume без тихого fallback.

**Приёмка:** fresh, resume success и resume failure реализованы для process protocol; остаётся подтвердить их на выбранном Pi/OpenCode adapter.

### TAKT-011. Route DSL end-to-end

**Цель:** заменить mock в основном примере.

**Результат:** agent → validator → feedback → retry → success → approval.

**Приёмка:** минимум один тест требует двух попыток; success определяется только валидатором.

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

### TAKT-020. Eval metrics

Сравнение fresh/resume, моделей и workflow-стратегий на фиксированном наборе задач.

## Вне v0.2

- parallel DAG;
- SQLite/Postgres;
- MCP server;
- Web UI;
- remote workers;
- untrusted/server mode;
- sandbox и многопользовательская авторизация.
