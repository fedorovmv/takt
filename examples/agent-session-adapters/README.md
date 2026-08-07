# Нейтральные адаптеры кодинг-агентов

Takt не зависит от Kiro CLI или другого конкретного хоста. Встроенный профиль
`code` использует логическое имя `coding-agent`, а `.takt/config.yaml` выбирает
фактический адаптер через `default_assistant`.

Для Pi и OpenCode есть встроенные адаптеры. Codex, Oh My Pi, Qwen CLI и другие
кодинг-агенты подключаются процессной обёрткой по
`takt-assistant/v1alpha2`. Обёртка должна:

1. прочитать начальную JSON-запись `request`;
2. объявить реальные возможности;
3. создать или продолжить изолированную сессию кодинг-агента;
4. передавать нормализованные сообщения, usage и tool lifecycle;
5. ожидать `tool.decision`, когда заявлен `tool_control`;
6. вернуть ровно одну terminal-запись `result`.

Файл `config.yaml` показывает несколько альтернативных адаптеров. Имена
`codex-takt-adapter`, `ohmypi-takt-adapter` и `qwen-takt-adapter` являются
примерами внешних исполняемых файлов, а не частью поставки Takt.

Для одного проекта выбирается один основной исполнитель:

```yaml
default_assistant: qwen
```

Workflow и команды профиля при этом не меняются.

## Conformance kit v0.1.41

Takt предоставляет product-neutral проверку `takt-assistant/v1alpha2` в Go-пакете `sdk/agentadapter`. Новый wrapper для Codex, Oh My Pi, Qwen CLI или другого coding agent сначала записывает NDJSON transcript своего contract fixture, затем проверяет общие protocol/session invariants:

```go
report, err := agentadapter.ValidateTranscript(reader, agentadapter.Options{
    RequireDeclaration: true,
    RequiredCapabilities: []string{"structured_output"},
    RequestedSessionID: "session-123",
})
```

Conformance kit проверяет declaration, terminal result, exit/status, tool-control consistency и resume identity. Он не объявляет capability за адаптер: `tool_control`, read-only enforcement и strict completion gate должны подтверждаться отдельным fixture/live test конкретного хоста.
