# Стартовая инструкция для кодинг-агента

## Контекст

Takt — локальный Go-runtime, который выбирает и исполняет проверяемые процессы поверх готовых кодинг-агентов. Он не реализует собственный LLM tool loop и не зависит от Kiro CLI. Исполнителем может быть Pi, OpenCode, Codex, Oh My Pi, Qwen CLI или другой CLI через совместимый `SessionAdapter`.

## Перед работой

Прочитай:

1. корневой `AGENTS.md`;
2. `skills/takt/SKILL.md`;
3. `docs/archive/releases/52-simple-reliable-agent-neutral-router-v0.1.38.md`;
4. `docs/proposals/001-simple-reliable-agent-neutral-takt.md`;
5. `docs/03-specification.md`;
6. `docs/09-runtime-semantics.md`;
7. `docs/10-assistant-adapter-spec.md`;
8. `docs/12-document-map.md`.

Проверь исходное состояние:

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/takt
./scripts/verify.sh
```

## Граница ответственности

- coding-agent host исполняет LLM turn, инструменты и физический sandbox;
- host bridge перехватывает `/takt`, показывает preview/status/result и создаёт worker-сессии;
- Takt выбирает workflow, параметры, roles/skills, политики, бюджеты и terminal transition;
- все маршруты компилируются в один Workflow и один runtime;
- основная LLM получает только agent MCP surface из пяти `takt.task.*` tools;
- host, worker и operator используют отдельные surfaces.

## Подключение исполнителя

Встроенный профиль использует логическое имя `coding-agent`. Выбери конкретный адаптер в config:

```yaml
default_assistant: opencode
```

Для стороннего CLI:

```yaml
default_assistant: codex
assistants:
  codex:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [codex-takt-adapter]
```

Пример является контрактом интеграции, а не заявлением о поставке `codex-takt-adapter`. Не допускай тихого fallback с resume на fresh и не приписывай адаптеру capabilities, которых он фактически не обеспечивает.

## Ближайшее развитие

Следующий продуктовый срез после v0.1.38:

1. внутренний Role Contract и Brief Compiler без нового пользовательского YAML;
2. реакции проверок `deny | repair | warn`;
3. выборочное усиление baseline, независимых тестов и verifier;
4. conformance gates и router/authoring evals;
5. предложения новых skills по повторяющимся Run, только через pending approval.

## Ограничения

- не добавляй второй scheduler или отдельную машину состояний;
- не выноси выбор workflow и roles в prompt основной модели;
- не добавляй новый публичный MCP-tool, когда операция относится к host/worker/operator surface;
- не превращай каждую проверку в блокировку: устранимая техническая проблема должна идти в repair;
- не скрывай невозможность resume, sandbox или tool interception;
- не считай текст агента доказательством успеха;
- не расширяй runtime до недоверенных пользователей без threat model и OS sandbox.

## Definition of Done

- код отформатирован;
- unit и contract tests проходят;
- `go test -race ./...` и `go vet ./...` проходят;
- сквозные примеры работают;
- документация отражает фактическое состояние;
- пользовательский путь остаётся простым;
- изменение сохраняет нейтральную границу с внешним coding agent.
