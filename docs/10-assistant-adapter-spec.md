# Спецификация адаптеров исполнителей v0.2

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

Текущий `v1alpha1` поддерживает только `type`, `argv`, `env`, `capabilities`. Остальные поля — целевое расширение v0.2.

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

Текущий прототип выставляет `TAKT_*` и временно дублирует значения в устаревшие `HARNESS_*`. Новые адаптеры используют только `TAKT_*`; совместимость `HARNESS_*` удаляется при переходе к стабильной схеме.

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

Первый специализированный адаптер должен:

- определить доступную версию Pi;
- строить командную строку из структурированного Request;
- поддерживать print/JSON/RPC режим, выбранный после прототипирования;
- извлекать Session ID;
- различать отказ resume и новый запуск;
- передавать model provider/id без предположения о едином формате;
- поддерживать cancellation;
- иметь интеграционные тесты с fake binary и отдельные opt-in тесты с реальным Pi.

## 10. OpenCode adapter

После Pi либо вместо него:

- запуск через стабильный server/API предпочтительнее парсинга TUI;
- agent name и model задаются отдельно;
- base URL и authentication находятся в config/environment;
- session ID нормализуется в общий контракт;
- capabilities отражают фактические возможности используемой версии OpenCode.

## 11. Тестирование адаптера

Обязательные тесты:

1. prompt через stdin;
2. prompt в argv;
3. model params;
4. рабочий каталог;
5. env и секреты;
6. успешный exit;
7. ненулевой exit;
8. timeout;
9. cancellation;
10. output limit;
11. fresh session;
12. successful resume;
13. failed resume;
14. malformed structured output.

Для CI используется fake assistant binary. Реальные Pi/OpenCode тесты запускаются отдельно и не блокируют обычный unit test suite.
