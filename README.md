# Takt

Универсальный Go-runtime для воспроизводимых процессов с кодовыми агентами, одиночными вызовами моделей, детерминированными командами, циклами проверки и участием человека.

Проект вдохновлён моделью Archon, но не является портом его исходного кода. Takt реализует компактное подмножество наиболее полезных механизмов в Go и сохраняет строгую границу с Pi, OpenCode, Codex и другими кодовыми агентами.

## Область применения текущей версии

`v0.1.8-alpha` предназначена для **локального однопользовательского trusted runtime**. Workflow, config, Markdown-команды и рабочая директория считаются доверенными.

Серверный и многопользовательский запуск, а также выполнение конфигураций от недоверенных пользователей требуют sandbox, политики путей, изоляции сети, управления секретами и более сильной модели блокировок. Эти режимы пока не поддерживаются.

## Что уже работает

- конфигурация моделей и исполнителей;
- Markdown-команды с frontmatter;
- workflow в YAML или JSON;
- последовательный DAG с `depends_on`, `when` и `trigger_rule`;
- единая семантика корневого DAG и дочернего DAG `loop_group`;
- узлы `command`, `prompt`, `bash`, `approval`, `loop_group`;
- вложенные `loop_group` явно запрещены в `v1alpha1`;
- повтор узла после внешней проверки;
- переносимые hooks `before_node`, `after_node`, `before_complete`, `on_failure`;
- разделение ненулевого exit code, ошибки запуска, timeout и cancellation;
- `allow_failure`, разрешающий только ненулевой exit code;
- `all_done` после неуспешной зависимости;
- timeout всей попытки узла, включая portable hooks;
- timeout/cancellation родительского `loop_group` сохраняют `timed_out`/`cancelled`;
- общий thread-safe лимит stdout/stderr process assistant;
- approval с сохранением состояния и продолжением через `takt answer`;
- явное продолжение через `takt resume`;
- fingerprints workflow, config и Markdown-команд;
- блокировка Run при `answer` и `resume`;
- ревизии состояния и событий с проверкой согласованности;
- JSONL-журнал событий и файловые артефакты;
- адаптеры `mock`, универсальный `process` и специализированный `pi`;
- JSON-протокол `takt-assistant/v1alpha1` для внешних process assistants;
- fake-assistant contract suite: success, exit, start, timeout, cancel, concurrent output, malformed/strict protocol cases, fresh и resume;
- Pi RPC adapter и fake-Pi contract suite, включая model/thinking mapping, fresh/resume, timeout/cancel, output limit и отказ интерактивного extension UI;
- полное совпадение OS exit code и envelope `exit_code`, включая ноль;
- единый JSON envelope CLI для успеха и ошибок;
- строгий YAML subset с сохранением пустых строк в block scalar;
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

Повторное продолжение Run после временной ошибки CLI:

```bash
./bin/takt resume <run-id> --workspace examples/route-dsl
```

## С чего продолжать разработку

Семантика runtime, process-протокол и специализированный Pi RPC adapter стабилизированы контрактными тестами. Следующий вертикальный этап:

1. opt-in smoke test с установленным и авторизованным Pi;
2. Route DSL end-to-end: Pi → валидатор → feedback → retry/resume → approval;
3. OpenCode adapter только после проверки основного сценария либо при явной необходимости сравнения исполнителей.

Подробности:

- [Состояние реализации](docs/05-implementation-status.md)
- [Аудит и исправления v0.1.2](docs/16-audit-remediation-v0.1.2.md)
- [Дополнительная стабилизация v0.1.3](docs/17-audit-remediation-v0.1.3.md)
- [Классификация parent loop v0.1.4](docs/18-audit-remediation-v0.1.4.md)
- [Восстановление документации v0.1.5](docs/19-document-recovery-v0.1.5.md)
- [Fake-assistant contract suite v0.1.6](docs/20-fake-assistant-contract-v0.1.6.md)
- [Усиление protocol contract v0.1.7](docs/21-protocol-hardening-v0.1.7.md)
- [Pi RPC adapter v0.1.8](docs/22-pi-adapter-v0.1.8.md)
- [Backlog v0.2](docs/14-backlog-v0.2.md)

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
- [Аудит и исправления v0.1.2](docs/16-audit-remediation-v0.1.2.md)
- [Дополнительная стабилизация v0.1.3](docs/17-audit-remediation-v0.1.3.md)
- [Классификация parent loop v0.1.4](docs/18-audit-remediation-v0.1.4.md)
- [Восстановление документации v0.1.5](docs/19-document-recovery-v0.1.5.md)
- [Fake-assistant contract suite v0.1.6](docs/20-fake-assistant-contract-v0.1.6.md)
- [Усиление protocol contract v0.1.7](docs/21-protocol-hardening-v0.1.7.md)
- [Pi RPC adapter v0.1.8](docs/22-pi-adapter-v0.1.8.md)
- [Граница безопасности](SECURITY.md)
- [JSON Schemas](schemas/README.md)

## Важная граница

Takt управляет процессом снаружи. Внутренний цикл инструментов, работа с файлами, MCP, LSP, история сообщений и сжатие контекста остаются ответственностью Pi, OpenCode или другого кодового агента.
