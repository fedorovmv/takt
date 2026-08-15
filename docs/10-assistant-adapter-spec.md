# Спецификация адаптеров исполнителей

Статус: stable core содержит process-протокол v1alpha1/v1alpha2, capability
declaration, нормализованный EventSink и переиспользуемый conformance kit.
Pi RPC и OpenCode CLI поставляются как bundled extension adapters и покрыты
отдельными контрактными наборами; их live host-conformance остаётся guarded.
A1 runtime требует exact Session ID при resume и не подменяет failed resume
fresh-сессией.

Bundled adapters may return typed local metadata (`adapter` and an optional
`session_path`) alongside the normal Result. Pi exposes its stable session file;
OpenCode reports an unavailable path rather than inferring one from logs. Flow
evaluation copies exposed session files before cleanup with bounded size,
symlink/non-regular checks and shared redaction. `takt eval analyze` invokes a
dedicated read-only workflow and treats its JSON diagnosis as advisory evidence,
never as a replacement for deterministic validation.

## 1. Назначение

Session adapter связывает Takt с готовым кодинг-агентом или внешним CLI. Канонический контракт не зависит от Pi, OpenCode, Codex, Oh My Pi, Qwen CLI или другого продукта. Он не реализует агентный цикл, а нормализует запуск, модель, сессию, события и ошибки.

## 1.1. Логический исполнитель

Workflow профиля может использовать `provider: coding-agent`. Runtime разрешает
его через существующий Config binding, сохраняя один и тот же DAG при смене
хоста. Универсальный `process` реализует stable core contract; bundled
Pi/OpenCode extensions реализуют тот же `SessionAdapter`, но не входят в stable
core dependency graph.

Для сторонних кодинг-агентов используется `takt-assistant/v1alpha2`. Takt передаёт нормализованный Request с моделью, workspace, fresh/resume session, политикой и limits и получает поток событий и один terminal Result. Готовая обёртка конкретного CLI является отдельным интеграционным пакетом; ядро Takt не зависит от Kiro CLI и не эмулирует tool loop стороннего агента.

Адаптер может заявлять только фактически обеспеченные capabilities. Отсутствие блокирующего перехвата tool call означает отсутствие `tool_control`; наблюдательные события после исполнения не считаются enforcement.

### 1.2. Регистрация bundled extensions

Concrete bundled provider не регистрирует себя через package-global state или `init()`. Extension package возвращает декларативный `assistant.ProviderRegistration` с ID, display name, stability stage, factory и optional version probe. Production `assistant.Registry` собирается один раз в `internal/bootstrap`, проверяет дубликаты, копирует registrations и после construction доступен только для чтения. Stable assistant protocol поэтому не импортирует Pi/OpenCode, а runtime и tooling используют один и тот же явно собранный provider graph.

## 2. Базовый Go-контракт

Целевой интерфейс:

```go
type SessionAdapter interface {
    Describe(ctx context.Context) Capabilities
    Run(ctx context.Context, req Request, sink EventSink) (Result, error)
}
```

### Request

```go
type Request struct {
    RunID       string
    NodeID      string
    Attempt     int
    Prompt      string
    Workspace   string
    Model       ResolvedModel
    Session     SessionRequest
    NativeHooks json.RawMessage
    Environment map[string]string
    Metadata    map[string]string
    Limits      Limits
}
```

### ResolvedModel

```go
type ResolvedModel struct {
    Name     string
    Provider string
    ID       string
    Params   map[string]any
}
```

### SessionRequest

```go
type SessionRequest struct {
    Mode string // fresh | resume
    ID   string
}
```

### Limits

```go
type Limits struct {
    Timeout       time.Duration
    MaxOutputBytes int64
}
```

### Result

```go
type Result struct {
    Output       string
    Structured   json.RawMessage
    SessionID    string
    ExitCode     int
    Stdout       string
    Stderr       string
    ResolvedModel *ResolvedModel
    Usage        *Usage
}
```

Transport error возвращается через `error`. Ненулевой exit code сохраняется в `Result` и классифицируется runtime с учётом `allow_failure`.

В `takt-assistant/v1alpha1` OS exit code и `Result.ExitCode` описывают один результат и обязаны совпадать полностью, включая ноль. При расхождении adapter возвращает protocol error: envelope не переопределяет OS-завершение, а OS-код не отменяет строгую проверку envelope. Decoder также требует ровно один JSON result, допустимые version/type/status, обязательный и совместимый `exit_code`, неотрицательный usage и подтверждённый resume.

## 3. Capabilities

Adapter публикует список строковых capabilities:

- `tool_policy`;
- `skills`;
- `mcp`;
- `sandbox_filesystem`;
- `sandbox_network`;
- дополнительные adapter-specific names.

Runtime выводит обязательный набор из effective node policy и `requires`. `takt validate` выполняет этот preflight для локальных adapters; запуск повторяет проверку до процесса. Для `executor: external` worker подтверждает declaration при claim. Если capability отсутствует, выполнение отклоняется. Встроенные Pi/OpenCode не могут объявить через config зарезервированную возможность, которую adapter фактически не реализует. Универсальный `process` объявляет поддерживаемые гарантии явно, поскольку их исполняет внешний adapter.

`allowed_tools: []` и `skills: []` являются заданными пустыми allowlists, а не отсутствием политики. Эффективная политика передаётся в `Request.Policy`, process protocol и `TAKT_POLICY_JSON`; фактически применённая политика и capabilities сохраняются в состоянии узла.

## 3.1. Activity и idle timeout

Нормализованное событие через `Request.Emit` считается activity signal и сбрасывает `idle_timeout` AI-узла. Adapter обязан публиковать события только после фактического прогресса; искусственный heartbeat скрывает зависание и нарушает контракт. Общий `timeout` остаётся независимой верхней границей попытки.

Для внешнего executor activity сохраняется в `ExternalExecutionState.last_activity_at`, а expiry выполняет локальный daemon. Blocking tool approval приостанавливает idle expiry, потому что worker ожидает внешнее решение, а не завис.

## 4. Process transport

`process` остаётся универсальным низкоуровневым адаптером.

Конфигурация:

```yaml
assistants:
  pi:
    type: process
    argv:
      - pi
      - --print
      - --model
      - "{{model.id}}"
    stdin: prompt
    session:
      resume_arg: ["--session", "{{session.id}}"]
    result:
      format: text
    timeout: 20m
    max_output_bytes: 10485760
```

Текущий `v1alpha1` поддерживает `type`, `argv`, `env`, `capabilities`, `protocol` и `max_output_bytes`. При `protocol: takt-assistant/v1alpha1` request передаётся JSON через stdin, result читается как строгий JSON из stdout. Timeout задаётся на уровне Node.

## 5. Переменные process adapter

Поддерживаемые argv/env шаблоны:

```text
{{prompt}}
{{run.id}}
{{node.id}}
{{attempt}}
{{workspace}}
{{model.name}}
{{model.provider}}
{{model.id}}
{{model.params}}
{{session.mode}}
{{session.id}}
```

Переменные окружения целевого контракта:

```text
TAKT_RUN_ID
TAKT_NODE_ID
TAKT_ATTEMPT
TAKT_WORKSPACE
TAKT_MODEL_NAME
TAKT_MODEL_PROVIDER
TAKT_MODEL_ID
TAKT_MODEL_PARAMS_JSON
TAKT_SESSION_MODE
TAKT_SESSION_ID
TAKT_NATIVE_HOOKS_JSON
```

Текущая реализация выставляет все перечисленные `TAKT_*`. Устаревшие `HARNESS_*` удалены в `v0.1.2-alpha`.

## 6. Session policy

### fresh

- адаптер не использует предыдущий Session ID;
- Result может вернуть новый Session ID;
- retry с `attempts.retry_session: fresh` очищает сохранённый ID;
- `fresh_context: true` очищает ID между loop iterations.

### resume

- runtime передаёт сохранённый Session ID;
- отсутствие ID допустимо только для первой попытки, когда запрошен `resume` как
  default для ещё не созданной сессии;
- если исполнитель не смог восстановить сессию или вернул другой ID, попытка
  завершается `protocol`/`session_resume_failed`;
- тихий переход на fresh запрещён всегда, включая retry и `context: shared`;
- успешный resume обязан сохранить тот же Session ID в Result и durable
  `ExecutionState`.

### shared context и retry precedence

`context: shared` runtime разрешает только для assistant node с единственным
транзитивным upstream assistant ancestor того же provider/model и передаёт его
Session ID как `resume`. Если ancestor отсутствует, неоднозначен или resume не
подтверждён, node не запускается fresh.

Для повторов действуют правила: `attempts.retry_session: fresh` очищает ID,
`reuse` сохраняет прежний режим; hook `on_failure.session: resume` запрашивает
exact resume. Approval внутри loop не создаёт новую попытку и продолжает ту же
итерацию/session; следующая iteration наследует session только если
`fresh_context: false`.

## 6.1. Loop signal evidence

Assistant adapter возвращает полный normalized output и `output_truncated`; он
не объявляет успех loop сам. Runtime после terminal assistant result применяет
`until.signal` matcher (один `<promise>NAME</promise>` или последняя непустая
строка), сохраняет `matched_signal` либо `signal_diagnostic` (`signal_missing`
или `signal_ambiguous`) и отклоняет truncated source как protocol failure.
`until.requires` и `until_bash` остаются runtime/deterministic predicates:
adapter не подменяет их текстом агента и не добавляет скрытый validator.

## 7. Нормализация ошибок

Коды целевого уровня:

```text
adapter_not_found
capability_missing
process_start_failed
process_timeout
process_cancelled
process_exit_nonzero
output_limit_exceeded
session_resume_failed
invalid_structured_output
```

Ошибка содержит:

- code;
- message;
- assistant;
- model;
- run_id;
- node_id;
- attempt;
- exit_code при наличии;
- безопасный фрагмент stderr.

## 8. События адаптера

Канонический протокол `takt-agent-events/v2`:

```text
assistant.session.started
assistant.session.resumed
assistant.message
assistant.tool.requested
assistant.tool.allowed
assistant.tool.denied
assistant.tool.started
assistant.tool.completed
assistant.artifact.declared
assistant.usage
assistant.diagnostic
assistant.completed
assistant.failed
```

Adapter объявляет protocol, список capabilities, event types и флаги `session_events`, `tool_events`, `tool_control`, `artifact_events`, `usage_events`. `tool_control` разрешён только при pre-execution interception: Takt должен увидеть `tool.requested`, применить policy/approval и вернуть решение до запуска инструмента.

OpenCode и Pi поддерживают наблюдательные события и не заявляют `tool_control`. Внешний executor реализует durable blocking lifecycle. Process protocol `takt-assistant/v1alpha2` поддерживает bidirectional records `capabilities`, `event`, `tool.request`, `result` и ответ `tool.decision`.

Артефакт может содержать `call_id`, связывающий его с породившим tool call. Нормализованный поток не заменяет raw stdout/stderr.

## 9. Pi adapter

Pi принимает только существующие path skills: каждое значение проверяется через файловую систему до запуска. Именованный skill без локального пути для Pi не поддерживается. OpenCode поддерживает и path skills, и именованные skills; path skill внедряется в prompt, именованный ограничивается permissions.

Pi adapter реализован как `type: pi` и использует официальный subprocess RPC-режим:

```text
pi --mode rpc --provider <provider> --model <id> [--thinking ...] [--session ...]
```

Последовательность одной попытки:

1. `pi --version` проверяет доступность CLI и сохраняет версию в structured result;
2. запускается RPC-процесс в workspace узла;
3. `get_state` возвращает фактический Session ID и модель; при resume ID обязан
   совпасть с запрошенным;
4. `prompt` принимает полное задание через JSONL stdin;
5. перед prompt снимается накопленная статистика `get_session_stats`;
6. adapter ждёт `agent_settled`; события `agent_end` учитываются как отдельные низкоуровневые запуски и могут иметь `willRetry: true`;
7. после settlement читаются `get_messages`, `get_last_assistant_text`, повторный `get_session_stats` и `get_state`;
8. usage вычисляется как дельта накопленной статистики до/после попытки; уменьшение значений и исчезновение ранее присутствовавшего usage являются protocol error;
9. закрытие stdin штатно завершает RPC-процесс.

Последний assistant message со `stopReason: length` означает исчерпание
provider/model output limit и классифицируется как execution `exit`, даже если
Pi затем публикует `agent_settled` и RPC-процесс завершается с кодом 0. Session
ID и usage попытки сохраняются. Retry остаётся явной политикой workflow:
`attempts.retry_on: [exit]` вместе с `retry_session: reuse` продолжает ту же
сессию с feedback. `maxTokens` модели Pi, `contextWindow` и adapter
`max_output_bytes` являются тремя разными лимитами.

Поддержано:

- выбор provider/model и thinking level;
- `fresh` и проверенный `resume` через `--session`;
- отказ mismatched/missing resume без fresh fallback;
- timeout/cancellation вместе с process group;
- приоритет `timed_out`/`cancelled` над одновременно обнаруженным output overflow;
- общий race-safe лимит stdout/stderr;
- Session ID, версия Pi, фактический `responseModel` и per-attempt usage delta;
- дополнительные env и нерезервированные Pi flags;
- `--tools`/`--no-tools`, `--skill`/`--no-skills` и read-only tool restriction;
- opt-in smoke test с реальным бинарником.

Интерактивный extension UI не проксируется в рамках попытки: запросы, требующие ответа, считаются protocol error. Fire-and-forget методы `notify`, `setStatus`, `setWidget`, `setTitle` и `set_editor_text` допускаются. Project-local Pi resources управляются явным `project_trust`.

`Request.Metadata` является optional. Workflow runtime пока не строит mapping из workflow/node metadata; adapter обязан транспортировать поле, когда вызывающая сторона его заполнила. Pi adapter делает это через `TAKT_METADATA_JSON`.

Optional `message_end.message.usage.input` нормализуется как usage самого
`assistant.message` и может использоваться только как последнее измеренное
число input tokens model request для live progress. Оно не заменяет
authoritative per-attempt usage delta из `get_session_stats` после
`agent_settled`, не суммируется повторно и не считается maximum context window.
Если Pi не передал message usage, текущий context size остаётся неизвестным.

## 10. OpenCode adapter

OpenCode adapter реализован как `type: opencode` поверх официального неинтерактивного JSON-режима:

```text
opencode run --format json --dir <workspace> --model <provider>/<id> [--agent ...] [--variant ...] [--session ...]
```

Prompt передаётся через stdin. Stdout трактуется как NDJSON event stream, stderr сохраняется только как диагностика. Takt собирает итоговый текст из `text`, usage и cost — как сумму уникальных `step_finish`, а события `error` классифицирует как отказ агента даже при нулевом OS exit code.

При `--session` OpenCode должен вернуть тот же Session ID в event stream.
Отсутствующий или другой ID — protocol failure; новый fresh запуск не
подставляется автоматически.

Если parent context завершился, adapter сохраняет raw stdout/stderr и извлекает краткие сообщения из stderr и доступных `error` events. Итоговый execution kind остаётся `timed_out` или `cancelled`, а provider-диагностика добавляется к ошибке и logical output. Scheduler обязан сохранять такую специализированную context-ошибку, а не заменять её общим сообщением `node attempt`.

### 10.1. Provider availability result

`takt-assistant/v1alpha2` может вернуть failed result с
`failure_kind: provider_unavailable`, non-empty `session.id` и optional
non-negative `retry_after_ms`. Adapter выдаёт этот kind только по явному
transient evidence: HTTP `429`, `502`, `503` или `504`; explicitly
rate-limited/overloaded/temporarily-unavailable provider error; connection
reset/refused, explicit `connection error`, temporary DNS или equivalent
transport error без наблюдаемого request-side effect. Unknown errors, other
`4xx`, protocol/tool failures and assistant decisions are not this kind. Parent
timeout/cancellation wins.

Это terminal adapter result после собственных retries provider: Pi/OpenCode
internal retries не являются Takt `SessionAdapter.Run` calls. Scheduler может
вызвать adapter ровно три раза на workflow attempt, всегда с тем же Session ID;
`retry_after_ms` задаёт прямой delay, capped at `60s`. Adapter не выполняет
этот retry loop и не может silently resume as fresh.

Поддержано:

- выбор model alias на уровне workflow, команды или узла;
- `agent` и model `variant`; строковый `reasoning_effort` используется как fallback variant;
- `fresh` и проверенный `resume` через `--session`;
- exact Session ID check для resumed attempts без fresh fallback;
- version probe, timeout/cancellation, общий stdout/stderr limit;
- provider retry/connection diagnostics при timeout/cancellation без изменения execution kind;
- per-attempt usage и cost;
- permission/MCP policy через `OPENCODE_CONFIG_CONTENT`, explicit empty tool/skill allowlists и prompt injection для path skills;
- opt-in smoke test с реальным OpenCode CLI.

JSON stream текущего CLI не гарантирует отдельное событие о фактическом provider-side routing. Поэтому `resolved_model` равен явно переданному `provider/id`, если event stream не предоставил другую модель; источник фиксируется в structured metadata. `auto_approve: true` передаёт `--auto` и допускается только для доверенного workspace. Takt не парсит TUI и не реализует внутренний tool loop OpenCode.

## 11. Тестирование адаптера

Обязательные тесты:

1. prompt и рабочий каталог;
2. provider/model/thinking mapping;
3. env, optional metadata и native hooks transport;
4. успешное выполнение;
5. ненулевой OS exit;
6. ошибка запуска;
7. timeout;
8. cancellation;
9. общий concurrent stdout/stderr output limit;
10. malformed RPC/result;
11. fresh session без передачи старого ID;
12. successful resume того же Session ID;
13. failed или mismatched resume;
14. отказ prompt preflight;
15. agent-level failure;
16. неподдерживаемый интерактивный extension UI;
17. runtime `fresh → retry → resume`;
18. ожидание `agent_settled` после одного или нескольких `agent_end`;
19. запрет всех session/mode CLI-флагов, которыми adapter управляет самостоятельно;
20. per-attempt delta для fresh и resume;
21. fire-and-forget `set_editor_text`;
22. opt-in smoke test с реальным бинарником.

Для CI используются `internal/testsupport/cmd/takt-fake-assistant`, `internal/testsupport/cmd/takt-fake-pi`, `internal/testsupport/cmd/takt-fake-opencode` и Go contract tests. Реальный Pi smoke test включается только через `TAKT_PI_SMOKE=1` и не блокирует обычный unit test suite.


## 12. Conformance kit для сторонних coding-agent adapters

Пакет `sdk/agentadapter` является стабильной тестовой границей для обёрток Codex, Oh My Pi, Qwen CLI и других coding agents по `takt-assistant/v1alpha2`. Он не содержит логики конкретного продукта и не запускает agent loop.

`ValidateTranscript` принимает NDJSON transcript и проверяет protocol version, declaration capabilities, уникальность tool `call_id`, наличие `tool_control` при блокирующих tool requests, ровно один terminal result, согласованность status/exit code и сохранение Session ID при обязательном resume. Внешняя обёртка может использовать пакет в собственных Go contract tests. Канонические NDJSON fixtures находятся в `sdk/agentadapter/testdata/v1alpha2/`; адаптер на другом языке может прогонять эти же файлы своим validator-ом.

`internal/testsupport/cmd/takt-fake-assistant` использует public v1alpha2 request/result validators при работе как process wrapper, а его contract test повторно проверяет captured stdout через `ValidateTranscript`. Bundled Pi/OpenCode extension adapters не являются v1alpha2 process wrappers и поэтому не должны искусственно прогоняться через этот kit.

Conformance kit видит только протокольный transcript stdout и поэтому **не может** доказать соответствие OS process exit status полю `result.exit_code`; это отдельно проверяет process-host contract test. Conformance kit дополняет, а не заменяет product-specific smoke test: возможность `resume`, pre-execution tool control или read-only режима считается поддержанной только после фактической проверки хоста. Пример и матрица возможностей находятся в `examples/agent-session-adapters/README.md`.


## Compatibility matrix v0.2

Начиная с `v0.1.48`, поддерживаемые границы публикуются через:

```bash
takt compatibility matrix
takt compatibility check --config .takt/config.yaml
takt compatibility check --config .takt/config.yaml --live
```

Session adapter compatibility не равна host-control compatibility. Live smoke с Pi `0.83.0` (`aihub/Qwen/Qwen3.6-27B`) и OpenCode `1.18.14` (`aihub-sbt/Qwen/Qwen3.6-27B`) подтвердил fresh/exact resume обоих adapters. Для host-control подтверждены Pi extension load/command interception и OpenCode plugin load/command/input/recovery; Pi input/tool/recovery/completion и OpenCode tool/completion остаются непроверенными. Поэтому bundled integrations сохраняют `guarded`, а `strict_allowed` остаётся `false`. `takt-assistant/v1alpha1` сохраняется для чтения старых wrappers и помечен deprecated для новых интеграций; целевой process protocol — `v1alpha2`.

OpenCode `1.18.14` загружает plugin entrypoint как `Plugin(input) -> Promise<Hooks>` с hooks `chat.message` и `tool.execute.before`; TypeScript contract smoke проверяет assignability и runtime blocking bundled entrypoint. Headless host сохраняет намеренное прерывание hook как общий `UnknownError` в NDJSON, поэтому plugin до abort пишет точный Takt diagnostic в stderr. Для headless interception prompt передаётся через stdin; initial positional message может быть отправлен host до завершения загрузки external plugin и не является поддерживаемой guarded-границей.

## 14. Reference Qwen Code wrapper — v0.1.49

Поставляемый `cmd/qwen-takt-adapter` является первой внешней reference implementation public `sdk/agentadapter`. Он запускает Qwen Code headless CLI в `stream-json`, поддерживает fresh/exact resume, model selection, timeout и usage, но намеренно не заявляет `tool_control`, selected skills, MCP projection или sandbox capabilities.

Для `type: process` с `protocol: takt-assistant/v1alpha2` версия протокола больше не повышает capabilities автоматически. `capabilities` в Config являются preflight-ожиданием, а первая stream-запись `capabilities` обязана подтвердить их. Tool request допустим только при `tool_control: true` в фактической declaration.

Пример:

```yaml
assistants:
  qwen-reference:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [qwen-takt-adapter]
    capabilities: [agent_events_v2, session_events, usage_events]
```

Подробности и ограничения: `docs/63-reference-external-adapters-v0.1.49.md`.


## v0.1.50: terminal failure kind

`takt-assistant/v1alpha2` failed result may carry `failure_kind: exit|timed_out|cancelled`. It is authoritative for failures reported by a wrapper itself (for example Qwen budget exit 55); Takt's own context deadline/cancellation still takes precedence. Transport version never implies `tool_control`.
