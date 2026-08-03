# Takt

Универсальный Go-runtime для воспроизводимых процессов с кодовыми агентами, одиночными вызовами моделей, детерминированными командами, циклами проверки и участием человека.

Проект вдохновлён моделью Archon, но не является портом его исходного кода. Цель — реализовать компактное подмножество наиболее полезных механизмов в Go и сохранить строгую границу с Pi, OpenCode, Codex и другими кодовыми агентами.

## Что уже работает

- конфигурация моделей и исполнителей;
- Markdown-команды с frontmatter;
- workflow в YAML или JSON;
- DAG с `depends_on`, `when` и базовыми `trigger_rule`;
- узлы `command`, `prompt`, `bash`, `approval`, `loop_group`;
- повтор узла после внешней проверки;
- переносимые хуки `before_node`, `after_node`, `before_complete`, `on_failure`;
- передача вывода проверки в следующую попытку;
- approval с сохранением состояния и продолжением через `takt answer`;
- JSONL-журнал событий и файловые артефакты;
- адаптеры `mock` и универсальный `process` для Pi/OpenCode и других CLI;
- машинный JSON-вывод CLI;
- только стандартная библиотека Go.

## Быстрый старт

```bash
make check

./bin/takt validate examples/route-dsl/workflow.yaml \
  --config examples/route-dsl/config.yaml

./bin/takt run examples/route-dsl/workflow.yaml \
  --config examples/route-dsl/config.yaml \
  --workspace examples/route-dsl \
  --input examples/route-dsl/specification.md
```

Демонстрационный процесс остановится на approval-узле и вернёт `run_id`. Продолжение:

```bash
./bin/takt answer <run-id> approve-result \
  --workspace examples/route-dsl \
  --value "Подтверждаю"
```

## С чего начать разработку

1. [Карта документов](docs/12-document-map.md)
2. [Целевое состояние v0.2](docs/08-target-v0.2.md)
3. [Текущее состояние реализации](docs/05-implementation-status.md)
4. [План реализации v0.2](docs/11-implementation-plan.md)
5. [Руководство разработчика](DEVELOPMENT.md)

## Документация

- [Описание проекта](docs/01-project.md)
- [Подход к решению](docs/02-approach.md)
- [Текущая спецификация `takt/v1alpha1`](docs/03-specification.md)
- [Архитектура текущей реализации](docs/04-architecture.md)
- [Состояние реализации](docs/05-implementation-status.md)
- [Общий план развития](docs/06-roadmap.md)
- [Профиль совместимости с Archon](docs/07-archon-compatibility.md)
- [Целевое состояние v0.2](docs/08-target-v0.2.md)
- [Семантика runtime v0.2](docs/09-runtime-semantics.md)
- [Контракт адаптеров](docs/10-assistant-adapter-spec.md)
- [План реализации v0.2](docs/11-implementation-plan.md)
- [Карта источников истины](docs/12-document-map.md)
- [План оценки стратегий](docs/13-evaluation-plan.md)
- [Backlog v0.2](docs/14-backlog-v0.2.md)
- [Стартовая инструкция для кодового агента](docs/15-coding-agent-start.md)
- [JSON Schemas](schemas/README.md)

## Важная граница

Takt управляет процессом снаружи. Внутренний цикл инструментов, работа с файлами, MCP, LSP, история сообщений и сжатие контекста остаются ответственностью Pi/OpenCode/другого кодового агента.
