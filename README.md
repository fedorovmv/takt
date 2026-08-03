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

## Где лежит основная документация

- [Описание проекта](docs/01-project.md)
- [Подход к решению](docs/02-approach.md)
- [Спецификация](docs/03-specification.md)
- [Архитектура](docs/04-architecture.md)
- [Состояние реализации](docs/05-implementation-status.md)
- [План развития](docs/06-roadmap.md)
- [Профиль совместимости с Archon](docs/07-archon-compatibility.md)

## Важная граница

Takt управляет процессом снаружи. Внутренний цикл инструментов, работа с файлами, MCP, LSP, история сообщений и сжатие контекста остаются ответственностью Pi/OpenCode/другого кодового агента.
