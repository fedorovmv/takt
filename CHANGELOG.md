# Changelog

## v0.1.2-alpha

- исправлена семантика `allow_failure`: разрешается только ненулевой exit code;
- добавлена классификация `exit/start/timed_out/cancelled/protocol/internal`;
- добавлены Node statuses `errored`, `timed_out`, `blocked`;
- scheduler продолжает DAG после failure и выполняет `all_done`;
- root DAG и `loop_group` используют один scheduler;
- `when` и `trigger_rule` работают внутри `loop_group`;
- добавлены node timeout, process output limit и `output_truncated`;
- на Unix cancellation завершает process group;
- добавлены fingerprints workflow/config/commands;
- `answer` проверяет определения до сохранения ответа;
- добавлены lock Run и команда `takt resume`;
- persistence использует обязательные revisions state/event и обнаруживает рассогласование;
- YAML parser сохраняет пустые строки и поддерживает chomp modes block scalar;
- CLI использует единый JSON success/error envelope;
- `command run` использует user command scope;
- добавлены contract tests отказов, persistence, YAML, adapter и CLI;
- зафиксирован trusted local scope текущей версии.

## v0.1.1-alpha

- добавлено целевое состояние Takt v0.2;
- добавлена спецификация runtime-семантики;
- добавлен целевой контракт Pi/OpenCode/process adapters;
- добавлены план реализации, backlog и инструкция для кодового агента;
- добавлены JSON Schemas текущего `takt/v1alpha1`;
- добавлена карта документов и источников истины;
- process adapter выставляет переменные `TAKT_*`;
- версия CLI обновлена до `v0.1.1-alpha`.

## v0.1.0-alpha

- базовый Go-runtime;
- Markdown-команды, модели и process/mock assistants;
- DAG, hooks, retries, loop_group и approval pause/resume;
- локальное состояние и журнал событий;
- примеры Route DSL и hook retry.
