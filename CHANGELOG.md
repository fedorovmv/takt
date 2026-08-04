# Changelog

## v0.1.9-alpha

- Pi adapter завершает попытку только после `agent_settled`, а не первого `agent_end`;
- fake Pi моделирует automatic retry и проверяет, что Takt не возвращает частичный результат;
- расширен deny-list session/mode CLI-флагов Pi, включая короткие aliases;
- fire-and-forget `set_editor_text` допускается без ответа;
- usage Pi вычисляется как дельта накопленной статистики до/после prompt;
- добавлены регрессии для fresh/resume usage delta и уменьшения cumulative stats;
- добавлен ADR-021 и отчёт `docs/23-pi-rpc-alignment-v0.1.9.md`.

## v0.1.8-alpha

- добавлен специализированный `type: pi` через официальный `pi --mode rpc`;
- реализованы provider/model/thinking mapping, version probe и project trust;
- реализованы проверенные `fresh` и `resume` по фактическому Session ID;
- нормализованы итоговый текст, usage, resolved model, stdout/stderr и structured metadata;
- добавлены timeout/cancel, общий race-safe output limit и process-group termination;
- добавлены `cmd/takt-fake-pi`, Pi contract suite, runtime retry/resume test и opt-in real smoke;
- Pi-specific Config и JSON Schema синхронизированы;
- закрыты P2 документации: нумерация adapter tests, актуальная runtime version и optional metadata policy;
- добавлен ADR-020 и отчёт `docs/22-pi-adapter-v0.1.8.md`.

## v0.1.7-alpha

- OS exit code и `result.exit_code` в `takt-assistant/v1alpha1` обязаны совпадать всегда, включая ноль;
- добавлены отрицательные contract cases для версии, type, неизвестных полей/status, отсутствующего/null `exit_code`, несовместимых status/exit, двух JSON-значений и OS/envelope mismatch;
- decoder проверяет неотрицательные `usage.input_tokens`, `usage.output_tokens` и `usage.cost`;
- fake assistant отклоняет второй JSON request envelope;
- contract suite проверяет передачу `metadata` и `native_hooks`;
- `config.schema.json` запрещает `protocol` для `type: mock`, как runtime validator;
- добавлен отчёт `docs/21-protocol-hardening-v0.1.7.md`.

## v0.1.6-alpha

- реализован JSON-протокол `takt-assistant/v1alpha1` для process assistant;
- добавлен `cmd/takt-fake-assistant`;
- добавлены contract cases success, exit, start, timeout, cancel, concurrent output, malformed result, fresh, resume, resume rejection и output limit;
- runtime передаёт Run ID, Node ID и номер попытки в adapter;
- session resume проверен сквозным retry-тестом runtime;
- fake-assistant suite включён в `scripts/verify.sh`;
- обновлены JSON Schema, спецификации, backlog и документация.

## v0.1.5-alpha

- восстановлены полные редакции документов `v0.1.2–v0.1.3`, случайно перезаписанные при сборке `v0.1.4`;
- поверх восстановленной документации перенесена семантика parent-loop timeout/cancel из `v0.1.4`;
- восстановлены ADR-008–ADR-016, актуальные runtime specification, adapter contract, document map и coding-agent guide;
- добавлен отчёт `docs/19-document-recovery-v0.1.5.md`;
- кодовая семантика относительно `v0.1.4-alpha` не изменена.

## v0.1.4-alpha

- timeout и cancellation родительского `loop_group` сохраняют классификацию `timed_out`/`cancelled`;
- ошибка истёкшего attempt context имеет приоритет над производной ошибкой контейнера, включая `loop_group exhausted`;
- код ошибки Run для внешней отмены и deadline больше не записывается как `internal`;
- добавлены регрессии timeout и cancellation родительского `loop_group`;
- документация и результаты проверок обновлены перед fake-assistant contract suite.

## v0.1.3-alpha

- общий лимит stdout/stderr process assistant стал thread-safe;
- добавлен race-регрессионный тест одновременного stdout/stderr;
- `node.timeout` теперь ограничивает всю попытку: `before_node`, действие, `on_failure`, `after_node`, `before_complete`;
- timeout и cancellation внутри hooks сохраняют статусы `timed_out` и `cancelled`;
- вложенные `loop_group` явно запрещены в `v1alpha1` валидатором, JSON Schema и runtime-защитой;
- runtime предотвращает перезапись существующего состояния дочерним узлом цикла;
- `until` считается выполненным только для child node со статусом `completed`;
- добавлены регрессии для hook timeout/cancel, nested loops и skipped until-node;
- документация и схемы синхронизированы с фактической семантикой.

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
