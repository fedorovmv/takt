# Simple Reliable Router и нейтральные кодинг-агенты — v0.1.38-alpha

## Результат среза

`v0.1.38-alpha` добавляет единый пользовательский вход для задач и отделяет
его от внутренних host, worker и operator протоколов.

```text
takt task start <задача>
→ Task Router
→ existing workflow | simple-reliable | Dynamic Plan
→ preview
→ обычный Takt Workflow и Run
```

## Task Router

Профиль `code` публикует `workflows/task-route.yaml`. Роутер получает:

- задачу пользователя;
- детерминированные сигналы риска;
- каталог 19 готовых процессов;
- доверенный BlockPackage.

Результат соответствует `schemas/task-route.schema.json` и выбирает:

- `workflow` — готовый специализированный процесс;
- `template` — стабильный `simple-reliable`;
- `dynamic` — bounded Dynamic Takt.

Модель не может создать capability, workflow или block. Результат повторно
нормализуется и проверяется Go-кодом.

Если router не отвечает, возвращает невалидный JSON или недоступен, Takt не
блокирует обычную разработку. Он сохраняет route с сигналом
`router_fallback` и выбирает `simple-reliable + inspect_first`.

## Simple Reliable Development

Базовый процесс:

```text
investigate → implement → validate → review
```

Прогрессивные controls:

| Control | Добавляемая фаза или ограничение |
|---|---|
| `inspect_first` | checkpoint после исследования |
| `baseline` | baseline до изменений |
| `independent_tests` | отдельная test-design фаза |
| `enhanced_review` | adversarial verifier |
| `max_parallel` | верхняя граница параллельности |

Детерминированные сигналы могут только усиливать controls. Публичный API,
безопасность и миграции автоматически требуют baseline, независимых тестов и
усиленного ревью.

## Компактный Task API

CLI:

```text
takt task start
takt task status
takt task respond
takt task stop
takt task explain
```

Agent MCP surface публикует те же пять операций:

```text
takt.task.start
takt.task.status
takt.task.respond
takt.task.stop
takt.task.explain
```

`status` возвращает краткое состояние и признак `needs_input`. `explain`
показывает route, controls, фазы, Runs и артефакты.

## MCP surfaces

```text
takt mcp --surface agent       # default
takt mcp --surface host
takt mcp --surface worker
takt mcp --surface operator
takt mcp --surface all
```

Полная совместимая поверхность содержит 53 operations, но основная LLM видит
только пять task tools. Host-control и node lifecycle недоступны через agent
surface.

Daemon выбирает поверхность по `X-Takt-MCP-Surface`; без заголовка сохраняется
полная совместимая поверхность для старых клиентов daemon API.

## Нейтральный coding-agent

Встроенный профиль больше не закреплён за OpenCode. Workflow и Markdown-команды
используют логическое имя:

```yaml
assistant: coding-agent
```

Фактический исполнитель задаётся один раз:

```yaml
default_assistant: opencode
```

или внешним процессным адаптером:

```yaml
default_assistant: qwen
assistants:
  qwen:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [qwen-takt-adapter]
```

Прямые встроенные адаптеры Pi/OpenCode сохранены. Старый config без
`default_assistant`, содержащий `opencode`, продолжает работать. При одном
объявленном assistant он также выбирается автоматически. Неоднозначная
конфигурация отклоняется и требует явного default.

Codex, Oh My Pi и Qwen CLI не входят в поставку как готовые wrappers. Takt
предоставляет нейтральный protocol seam и пример конфигурации в
`examples/agent-session-adapters/`.

## Новые доверенные блоки

Пакет `code-core` теперь содержит девять блоков. Добавлены:

- `baseline` — read-only фиксация исходных проверок;
- `test-design` — независимое проектирование и добавление тестов.

Оба участвуют в транзитивном fingerprint пакета.

## Совместимость

- `takt mcp` теперь по умолчанию публикует безопасную agent surface;
- старым интеграциям, которым нужен полный набор, требуется
  `takt mcp --surface all`;
- daemon MCP без заголовка остаётся на `all`;
- существующие workflow по-прежнему выполняются одним scheduler/runtime;
- Dynamic Plan, host-control и external executor сохраняют прежние контракты;
- профиль `code` обновлён до `0.13.0`;
- Takt skill обновлён до `0.20.0`.

## Проверяемые инварианты

- route ссылается только на опубликованный workflow;
- template использует только trusted blocks;
- детерминированные protected-сигналы нельзя ослабить;
- router failure даёт inspect-first fallback;
- agent MCP содержит ровно пять tools;
- operator tool отклоняется на agent surface;
- `coding-agent` разрешается через один проверяемый default;
- все маршруты компилируются в стандартный Workflow и Run.

Полное архитектурное обоснование и дальнейший план находятся в
[`proposals/001-simple-reliable-agent-neutral-takt.md`](../../proposals/001-simple-reliable-agent-neutral-takt.md).
