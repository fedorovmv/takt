# Состояние реализации

## Реализовано

### Форматы и загрузка

- workflow/config в YAML и JSON;
- строгий decode неизвестных полей;
- документированный YAML subset с block scalar `|`, `|-`, `|+`, `>`, `>-`, `>+`;
- JSON Schemas для config, workflow, state, events, Markdown-команд и assistant protocol;
- проверка ссылок, ID и циклов DAG;
- явный запрет вложенных `loop_group` в `v1alpha1`.

### Runtime

- последовательный DAG с `depends_on`, `when` и `trigger_rule`;
- общая scheduler-семантика root DAG и дочернего DAG `loop_group`;
- `command`, `prompt`, `bash`, `approval`, `loop_group`, `subworkflow`, последовательный `foreach`;
- `subworkflow` и `foreach` внутри `loop_group` без отдельного scheduler;
- inline `foreach.items` и внешний `foreach.items_from.path` с fingerprint содержимого;
- упорядоченный JSON-массив результатов `foreach`;
- публичная проекция Run без развёрнутых `__`-ID при сохранении полного состояния для resume;
- defaults `assistant`, `model`, `session` на композиционном контейнере;
- portable hooks и retry с feedback;
- `all_done` после failure-like состояния зависимости;
- разделение `exit`, `start`, `timed_out`, `cancelled`, `protocol`, `internal`;
- `allow_failure`, разрешающий только ненулевой exit code;
- timeout всей попытки, включая portable hooks;
- сохранение timeout/cancel на уровне родительского `loop_group`;
- `until` только по child node со статусом `completed`;
- pause/resume approval;
- fingerprints workflow/config/commands, включая подключённые subworkflow и локальные команды;
- блокировка Run и revision consistency state/events;
- JSONL-журнал событий и файловые артефакты.

### Assistants и protocol

- `mock`, универсальный `process`, специализированные `pi` и `opencode` assistants;
- текстовый process mode;
- строгий JSON process mode `takt-assistant/v1alpha1`;
- общий race-safe лимит stdout/stderr;
- timeout, cancellation и завершение process group на Unix;
- строгая проверка одного request/result envelope;
- полное совпадение OS exit code и envelope `exit_code`;
- проверка version/type/status, usage, session resume и неизвестных полей;
- fake-assistant binary и отрицательный contract suite;
- сквозной `fresh → retry → resume` через process protocol и Pi adapter;
- Pi RPC: version probe с сохранением версии, provider/model/thinking mapping, verified Session ID, ожидание `agent_settled`, per-attempt usage delta, строгая последовательность usage snapshots и фактический `responseModel`;
- приоритет timeout/cancellation над совпавшим output overflow в Pi adapter;
- fake-Pi contract suite и opt-in smoke test с реальным CLI;
- интеграционные overflow+timeout/cancel проверки через `Pi.Run` и `OpenCode.Run` с корректными parent contexts и сохранение `OutputTruncated` в `NodeState`;
- OpenCode `run --format json`: version probe, model/agent/variant mapping, verified resume, NDJSON events, per-step usage, error events, сохранение provider diagnostics при timeout/cancellation и opt-in smoke;
- воспроизводимый Route DSL end-to-end с двумя попытками, feedback, resume, обязательной проверкой, артефактами и approval;
- накопление usage по всем агентным попыткам в `NodeState`;
- отдельные execution records для каждой попытки с assistant/version/requested/resolved model и usage;
- `takt eval run/report`, предварительная проверка уникальности `case_id`, запрет пересечения template/output, fingerprints стратегии/benchmark/workspace/валидатора, assistant/version/requested/resolved model и JSON-отчёт с предметными метриками качества;
- раздельная атрибуция usage по execution identity и явная отметка mixed identity;
- явные нулевые quality-метрики и `null` для недоступных средних значений;
- сохранение validation envelope при любом terminal status quality-node и success gate `completed && valid=true`;
- раздельное хранение stdout/stderr bash-узла и декодирование quality envelope только из stdout;
- строгие схемы `takt-validation/v1alpha1` и `takt-evaluation/v1alpha1`;
- стартовый Route DSL eval-набор из десяти синтетических заданий.
- переносимый authoring skill `skills/takt/` для создания config, workflow, Markdown-команд и проверяемых профилей;
- проверяемый стартовый профиль с inline prompt, моделями на узлах, retry/feedback и approval.

### CLI

- `validate`, `run`, `answer`, `resume`, `status`, `command run`;
- единый JSON success/error envelope;
- единая область поиска проектных и пользовательских Markdown-команд.

## Осознанно упрощено

- готовые DAG-узлы и `foreach` выполняются последовательно;
- параллельный `foreach` требует отдельного расширения scheduler;
- `attempts`, `timeout`, hooks, `native_hooks` и `allow_failure` композиционной группы задаются в дочернем workflow;
- состояние хранится локально в файлах;
- язык выражений ограничен;
- nested loops запрещены вместо path-based namespace;
- process protocol не передаёт потоковые tool events;
- native hooks передаются adapter, но не исполняются ядром.

## Текущая граница безопасности

Текущая версия — локальный однопользовательский trusted runtime. Workflow, config, Markdown-команды, бинарники assistants и workspace считаются доверенными.

Для server/untrusted scope нужны sandbox, контроль путей и сети, политика секретов, усиленные блокировки и отдельная threat model.

## Не реализовано

- подключение штатного Route DSL validator и реального обезличенного набора заданий;
- capability negotiation по фактическим возможностям adapter;
- MCP-интерфейс Takt;
- параллельный scheduler;
- server/Web UI;
- path-based state namespace для вложенных циклов;
- динамические input adapters для OpenSpec, issue, JSON/YAML и других источников;
- миграции стабильной схемы.

## Ближайший целевой срез

Пакеты профилей, reusable `subworkflow`, внешний массив `items_from` и последовательный `foreach` реализованы без обязательного task AST. Следующие runtime-срезы — параллельный `foreach`, групповые execution policies и расширяемые input adapters для источников сложнее явного YAML/JSON-массива. Markdown-профиль `code` продолжает работать с исходным документом напрямую.

Evaluation runner, изоляция путей, strategy/benchmark/workspace/validator fingerprints, per-attempt execution identity и предметные метрики качества реализованы. Следующий срез — запустить `examples/route-dsl-benchmark` со штатным `route-tool`, получить baseline на десяти реальных обезличенных заданиях и затем сравнить модели или стратегии на неизменных fingerprints. Настоящий time-to-valid потребует временной отметки успешного quality-node; текущий показатель времени является амортизированной end-to-end длительностью benchmark. Нормализованные diagnostics и учёт ручных исправлений остаются следующими расширениями. Pi и OpenCode доступны для практических workflow; следующий предметный шаг остаётся подключением штатного `route-tool`.
