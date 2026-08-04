# Спецификация адаптеров исполнителей

Статус: process-протокол и fake contract suite реализованы в `v0.1.7-alpha`; специализированный Pi RPC adapter реализован в `v0.1.8-alpha` и согласован с финальной RPC-семантикой Pi в `v0.1.9-alpha` и усилен по приоритету context/usage snapshots в `v0.1.11-alpha`. OpenCode adapter, capability discovery и потоковый EventSink остаются целевыми возможностями v0.2.

## 1. Назначение

Assistant adapter связывает Takt с готовым кодовым агентом или внешним CLI. Он не реализует агентный цикл, а нормализует запуск, модель, сессию, события и ошибки.

## 2. Базовый Go-контракт

Целевой интерфейс:

```go
type Adapter interface {
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

```go
type Capabilities struct {
    SessionResume    bool
    StructuredOutput bool
    ToolEvents       bool
    NativeHooks      []string
    MCP              bool
    Skills           bool
    Streaming        bool
    ModelOverride    bool
}
```

Config объявляет ожидаемые capabilities, а адаптер сообщает фактические. Takt должен отклонять запуск, если workflow требует неподдерживаемую возможность.

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
- retry с `session: fresh` очищает сохранённый ID.

### resume

- runtime передаёт сохранённый Session ID;
- отсутствие ID на первой попытке трактуется как fresh;
- если исполнитель не смог восстановить сессию, адаптер возвращает явный признак `resumed: false` либо ошибку согласно конфигурации;
- тихий переход на fresh запрещён по умолчанию.

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

Минимально:

```text
assistant.started
assistant.stdout
assistant.stderr
assistant.completed
assistant.failed
```

При поддержке исполнителем:

```text
assistant.session.started
assistant.session.resumed
assistant.tool.started
assistant.tool.completed
assistant.artifact.changed
assistant.usage
```

Потоковые события не должны быть обязательными для process adapter.

## 9. Pi adapter

Pi adapter реализован как `type: pi` и использует официальный subprocess RPC-режим:

```text
pi --mode rpc --provider <provider> --model <id> [--thinking ...] [--session ...]
```

Последовательность одной попытки:

1. `pi --version` проверяет доступность CLI и сохраняет версию в structured result;
2. запускается RPC-процесс в workspace узла;
3. `get_state` возвращает фактический Session ID и модель;
4. `prompt` принимает полное задание через JSONL stdin;
5. перед prompt снимается накопленная статистика `get_session_stats`;
6. adapter ждёт `agent_settled`; события `agent_end` учитываются как отдельные низкоуровневые запуски и могут иметь `willRetry: true`;
7. после settlement читаются `get_messages`, `get_last_assistant_text`, повторный `get_session_stats` и `get_state`;
8. usage вычисляется как дельта накопленной статистики до/после попытки; уменьшение значений и исчезновение ранее присутствовавшего usage являются protocol error;
9. закрытие stdin штатно завершает RPC-процесс.

Поддержано:

- выбор provider/model и thinking level;
- `fresh` и проверенный `resume` через `--session`;
- timeout/cancellation вместе с process group;
- приоритет `timed_out`/`cancelled` над одновременно обнаруженным output overflow;
- общий race-safe лимит stdout/stderr;
- Session ID, resolved model и per-attempt usage delta;
- дополнительные env и нерезервированные Pi flags;
- opt-in smoke test с реальным бинарником.

Интерактивный extension UI не проксируется в рамках попытки: запросы, требующие ответа, считаются protocol error. Fire-and-forget методы `notify`, `setStatus`, `setWidget`, `setTitle` и `set_editor_text` допускаются. Project-local Pi resources управляются явным `project_trust`.

`Request.Metadata` является optional. Workflow runtime пока не строит mapping из workflow/node metadata; adapter обязан транспортировать поле, когда вызывающая сторона его заполнила. Pi adapter делает это через `TAKT_METADATA_JSON`.

## 10. OpenCode adapter

После Pi либо вместо него:

- запуск через стабильный server/API предпочтительнее парсинга TUI;
- agent name и model задаются отдельно;
- base URL и authentication находятся в config/environment;
- session ID нормализуется в общий контракт;
- capabilities отражают фактические возможности используемой версии OpenCode.

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

Для CI используются `cmd/takt-fake-assistant`, `cmd/takt-fake-pi`, `scripts/test-fake-assistant.sh` и `scripts/test-pi-adapter.sh`. Реальный Pi smoke test включается только через `TAKT_PI_SMOKE=1` и не блокирует обычный unit test suite.
