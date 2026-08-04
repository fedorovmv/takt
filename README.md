# Takt

Универсальный Go-runtime для воспроизводимых процессов с кодовыми агентами, одиночными вызовами моделей, детерминированными командами, циклами проверки и участием человека.

Проект вдохновлён моделью Archon, но не является портом его исходного кода. Takt реализует компактное подмножество наиболее полезных механизмов в Go и сохраняет строгую границу с Pi, OpenCode, Codex и другими кодовыми агентами.

## Область применения текущей версии

`v0.1.16-alpha` предназначена для **локального однопользовательского trusted runtime**. Workflow, config, Markdown-команды и рабочая директория считаются доверенными.

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
- Pi RPC adapter и fake-Pi contract suite, включая model/thinking mapping, fresh/resume, ожидание `agent_settled`, автоматический retry, per-attempt usage delta, timeout/cancel, output limit и границу extension UI;
- полное совпадение OS exit code и envelope `exit_code`, включая ноль;
- единый JSON envelope CLI для успеха и ошибок;
- строгий YAML subset с сохранением пустых строк в block scalar;
- aggregate usage по узлам и отдельные execution records по каждой фактической попытке;
- `takt eval run/report` для воспроизводимой оценки каталогов заданий с fingerprints стратегии, benchmark, workspace и валидатора, версией assistant, requested/resolved model и предметными метриками качества;
- атрибуция tokens/cost по execution identity; смена assistant, его версии или resolved model между retry помечается как mixed;
- измеренные нулевые показатели сохраняются как `0`, а недоступные средние значения — как `null`;
- validation envelope сохраняется при любом terminal status quality-node; успех требует `completed && valid=true`;
- строгий контракт результата валидатора `takt-validation/v1alpha1`;
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

Прогон набора Route DSL заданий:

```bash
./bin/takt eval run examples/route-dsl-e2e/workflow.yaml \
  --config examples/route-dsl-e2e/config.yaml \
  --cases examples/route-dsl-eval/cases \
  --workspace-template examples/route-dsl-e2e \
  --output .takt/evals/qwen-resume \
  --answer approved \
  --strategy-id qwen-route-feedback-v1 \
  --benchmark-id route-dsl-real-10-v1 \
  --quality-node full-validation \
  --generation-node implement \
  --validator-id route-tool \
  --validator-version 1.0 \
  --validator-path route-tool \
  --repeat 3 \
  --replace \
  --json
```

## С чего продолжать разработку

Семантика runtime, process-протокол и специализированный Pi RPC adapter стабилизированы контрактными тестами. Воспроизводимый Route DSL end-to-end добавлен в `examples/route-dsl-e2e` и проверяется в `make check`.

Evaluation runner фиксирует идентичность стратегии, набора заданий, workspace и валидатора, а также execution identity каждой попытки. Следующий вертикальный этап — запустить `examples/route-dsl-benchmark` со штатным Route DSL validator и реальными обезличенными заданиями, получить baseline и сравнить модели или стратегии на неизменных fingerprints. OpenCode adapter нужен после этого сравнения либо при явной необходимости сопоставить исполнителей.

Подробности:

- [Состояние реализации](docs/05-implementation-status.md)
- [Аудит и исправления v0.1.2](docs/16-audit-remediation-v0.1.2.md)
- [Дополнительная стабилизация v0.1.3](docs/17-audit-remediation-v0.1.3.md)
- [Классификация parent loop v0.1.4](docs/18-audit-remediation-v0.1.4.md)
- [Восстановление документации v0.1.5](docs/19-document-recovery-v0.1.5.md)
- [Fake-assistant contract suite v0.1.6](docs/20-fake-assistant-contract-v0.1.6.md)
- [Усиление protocol contract v0.1.7](docs/21-protocol-hardening-v0.1.7.md)
- [Pi RPC adapter v0.1.8](docs/22-pi-adapter-v0.1.8.md)
- [Согласование Pi RPC-контракта v0.1.9](docs/23-pi-rpc-alignment-v0.1.9.md)
- [Усиление context/usage Pi v0.1.10](docs/24-pi-context-usage-hardening-v0.1.10.md)
- [Route DSL end-to-end v0.1.11](docs/25-route-dsl-e2e-v0.1.11.md)
- [Evaluation runner v0.1.12](docs/26-evaluation-runner-v0.1.12.md)
- [Изоляция и диагностика evaluation v0.1.13](docs/27-evaluation-isolation-report-v0.1.13.md)
- [Идентичность benchmark и качество v0.1.14](docs/28-benchmark-identity-quality-v0.1.14.md)
- [Семантика метрик и execution identity v0.1.15](docs/29-benchmark-metric-semantics-v0.1.15.md)
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
- [Согласование Pi RPC-контракта v0.1.9](docs/23-pi-rpc-alignment-v0.1.9.md)
- [Усиление context/usage Pi v0.1.10](docs/24-pi-context-usage-hardening-v0.1.10.md)
- [Route DSL end-to-end v0.1.11](docs/25-route-dsl-e2e-v0.1.11.md)
- [Evaluation runner v0.1.12](docs/26-evaluation-runner-v0.1.12.md)
- [Изоляция и диагностика evaluation v0.1.13](docs/27-evaluation-isolation-report-v0.1.13.md)
- [Идентичность benchmark и качество v0.1.14](docs/28-benchmark-identity-quality-v0.1.14.md)
- [Семантика метрик и execution identity v0.1.15](docs/29-benchmark-metric-semantics-v0.1.15.md)
- [Граница безопасности](SECURITY.md)
- [JSON Schemas](schemas/README.md)

## Важная граница

Takt управляет процессом снаружи. Внутренний цикл инструментов, работа с файлами, MCP, LSP, история сообщений и сжатие контекста остаются ответственностью Pi, OpenCode или другого кодового агента.
