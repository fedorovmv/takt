# Ключевые архитектурные решения

## ADR-001. Новый Go-runtime вместо порта Archon

Archon используется как поведенческая спецификация. Исходный TypeScript runtime не переносится построчно.

## ADR-002. Кодовый агент остаётся внешним исполнителем

Takt не реализует tool-calling, файловые инструменты, MCP, LSP и память агента.

## ADR-003. Модель отделена от исполнителя

Workflow выбирает логическую модель и assistant независимо. Один assistant может запускаться с разными моделями.

## ADR-004. Command — Markdown-инструкция

Термин `command` означает переиспользуемый prompt-файл, а детерминированная команда называется `bash`.

## ADR-005. Hooks делятся на portable и native

Portable hooks исполняются runtime. Native hooks передаются конкретному adapter и не интерпретируются ядром.

## ADR-006. Approval — сохраняемое состояние

Ожидание человека не блокирует stdin. Run сохраняется и продолжается отдельной командой.

## ADR-007. Локальное файловое хранилище в прототипе

`state.json` и `events.jsonl` позволяют проверить модель без сервера и базы данных. Production-store будет заменяемым.

## ADR-008. Текущий scope — локальный trusted runtime

До отдельной threat model Takt не принимает workflow и команды от недоверенных пользователей и не используется как многопользовательский сервер.

## ADR-009. Failure узла не останавливает DAG немедленно

Node failure является terminal-результатом узла. Scheduler продолжает доступные ветви, включая `all_done`, и вычисляет итог Run после завершения графа.

## ADR-010. `allow_failure` разрешает только exit code

Ошибки запуска, timeout, cancellation, protocol и internal errors не могут быть скрыты через `allow_failure`.

## ADR-011. Root DAG и loop DAG используют одну семантику

`depends_on`, `when`, `trigger_rule`, hooks, attempts и ошибки реализуются общим scheduler.

## ADR-012. Persistence использует revision consistency

State и event одного перехода получают одинаковую revision. Рассогласование считается ошибкой хранилища, а не восстанавливается молча.

## ADR-013. Поддерживается документированный YAML subset

До появления требования полной YAML 1.2 Takt сохраняет stdlib-only parser, формально ограничивает subset и покрывает block scalar тестами.

## ADR-014. Timeout ограничивает всю попытку узла

`node.timeout` охватывает portable hooks и действие узла. Timeout/cancellation внутри hook сохраняют execution kind и не преобразуются в `hook_failed`.

## ADR-015. Nested loop groups запрещены в v1alpha1

До введения path-based namespace дочерних состояний вложенные `loop_group` отклоняются валидатором и runtime. Это исключает коллизии ID и повреждение состояния внешнего DAG.

## ADR-016. `until` требует успешное завершение проверочного узла

Условие цикла оценивается только для child node со статусом `completed`. Значения output/exit code из `skipped` или failure-like состояний не могут завершить цикл.

## ADR-017. Классификация attempt context имеет приоритет над ошибкой контейнера

После возврата действия runtime сначала проверяет завершение attempt context. Deadline сохраняется как `timed_out`, внешняя отмена — как `cancelled`, включая родительский `loop_group`. Производные ошибки контейнера, например `loop_group exhausted`, не переопределяют эту причину.


## ADR-018. Process assistant может использовать строгий JSON-протокол

`protocol: takt-assistant/v1alpha1` переводит универсальный process adapter из текстового режима в строгий request/result envelope. Prompt, модель, Run/Node/Attempt, session и limits передаются через stdin JSON; stdout содержит ровно один result JSON. Malformed/truncated result и неуспешный resume являются protocol error. Текстовый process mode сохраняется для совместимости.


## ADR-019. OS exit code и protocol envelope обязаны совпадать

В `takt-assistant/v1alpha1` OS exit code и `result.exit_code` описывают один результат и обязаны совпадать всегда, включая ноль. При расхождении Takt возвращает `protocol`, а не выбирает одну сторону как авторитетную. Это предотвращает скрытие transport failure envelope-ом и ложное объявление failure при успешном OS-завершении.

## ADR-020. Pi интегрируется через официальный RPC mode

Специализированный `type: pi` запускает отдельный `pi --mode rpc` на каждую попытку узла. Prompt передаётся JSONL-командой, Session ID и модель проверяются через `get_state`, итог читается через `get_last_assistant_text` и `get_session_stats`, а закрытие stdin завершает процесс. Takt не парсит TUI и не реализует внутренний tool loop Pi. Resume считается успешным только при совпадении фактического Session ID; интерактивный extension UI не подменяет сохраняемые approval nodes Takt.

## ADR-021. Pi attempt завершается только на `agent_settled`

`agent_end` завершает один низкоуровневый запуск Pi и может сопровождаться `willRetry: true`, автоматическим retry, compaction retry или queued continuation. Takt считает агентную попытку завершённой только после `agent_settled`. `get_session_stats` трактуется как накопленная статистика сессии: adapter снимает значения до prompt и после settlement и возвращает дельту текущей попытки. Session/mode CLI-флаги полностью принадлежат adapter и запрещены в пользовательских `args`; fire-and-forget `set_editor_text` допускается наряду с другими UI-уведомлениями без ответа.

## ADR-022. Причина завершения context имеет приоритет над переполнением вывода Pi

Для Pi adapter истёкший deadline и внешняя отмена являются первичной причиной завершения попытки, даже если одновременно достигнут общий лимит stdout/stderr. В таком случае результат классифицируется как `timed_out` или `cancelled`, а признак `output_truncated` сохраняется как дополнительная диагностика. `get_session_stats` трактуется как последовательность накопленных снимков: если usage присутствовал до prompt и исчез после `agent_settled`, adapter возвращает `protocol`, а не пустую статистику. Явные нулевые значения остаются валидными.


## ADR-023. Completion gate Route DSL принадлежит детерминированному валидатору

Текстовый ответ Pi и факт записи `route.yaml` не считаются успехом. После каждой агентной попытки Takt запускает внешний валидатор; его ненулевой exit code и diagnostics становятся `${feedback}` следующей попытки. Session ID сохраняется для `resume`. Итоговые файл и отчёт проверки сохраняются как artifacts, а завершение процесса требует отдельного approval. Контрактный стенд может использовать fake Pi и минимальный validator, но производственный сценарий обязан заменить валидатор штатным инструментом без изменения runtime.


## ADR-024. Evaluation использует изолированные workspace и состояние runtime

Каждое задание evaluation выполняется как обычный Run в отдельной копии workspace template. Evaluation runner не интерпретирует предметный DSL и не подменяет внешний completion gate. Метрики attempts, duration, usage, approvals, errors и truncation читаются из сохранённого RunState. Workflow failures являются результатами оценки; ошибки подготовки workspace и отчёта являются инфраструктурными.


## ADR-025. Evaluation preflight защищает идентичность и границы workspace

Evaluation runner обязан завершить проверку входных путей и идентификаторов до создания output. Два файла, дающие одинаковый нормализованный `case_id`, отклоняются вместо добавления неявного суффикса: исходное имя задания остаётся проверяемой частью набора. `workspace-template` и `output` должны находиться в непересекающихся деревьях после разрешения символических ссылок, чтобы копирование template не могло захватить собственный destination. Отчёт строится из сохранённого `RunState` и включает подтверждённый resume, накопленный feedback, ошибку и diagnostic output узла.

## ADR-026. Сравнение требует fingerprints и внешнего результата качества

Читаемые `strategy_id` и `benchmark_id` не считаются достаточной идентичностью эксперимента. Evaluation report фиксирует отдельные fingerprints workflow, config, Markdown-команд, упорядоченного набора заданий, копируемого workspace template и валидатора, а также assistant, его версию, requested model и фактический `responseModel` последнего сообщения Pi. Предметное качество возвращает внешний узел по строгому `takt-validation/v1alpha1`; runtime не содержит знаний о Route DSL. Нарушение контракта качества является ошибкой измерительного контура. Инфраструктурный fake-Pi suite и реальный quality benchmark хранятся и оцениваются отдельно.

## ADR-027. Benchmark хранит execution identity по каждой попытке

Агрегированные поля узла сохраняются для совместимости и быстрого просмотра, но не используются для атрибуции затрат нескольких попыток. Каждое фактическое выполнение действия записывается отдельно с номером попытки, assistant, версией assistant, запрошенной и фактически использованной моделью, Session ID, usage и результатом выполнения.

Если execution identity меняется между повторами, узел помечается как `mixed_execution_identity`. Summary группирует токены и стоимость по отдельным execution identity и не приписывает сумму последней модели.

Показатель качества учитывается только тогда, когда quality-node завершился со статусом `completed`. JSON с `valid: true`, напечатанный неуспешным, отменённым или прерванным узлом, не является результатом измерения.

Нулевые измеренные показатели сериализуются числом `0`, а недоступные средние значения — `null`. Амортизированная длительность всего benchmark на один корректный результат называется `amortized_end_to_end_ms_per_valid` и не трактуется как фактическое время достижения валидного результата.
## ADR-028. Validation envelope отделён от статуса процесса

Exit code и terminal status quality-node описывают выполнение процесса, а `takt-validation/v1alpha1` — предметный результат проверки. Takt декодирует доступный envelope независимо от статуса узла и сохраняет score, checks и diagnostics. Benchmark считает результат корректным только при одновременном выполнении двух условий: quality-node имеет статус `completed`, а envelope содержит `valid: true`.

Корректный `valid: false` с ненулевым exit code является предметным результатом, а не потерей измерения. `valid: true` из failed, errored, timed_out или cancelled узла сохраняется для аудита, но не считается успехом. Malformed envelope при любом статусе является ошибкой измерительного контура.

## ADR-029. Структурный validation envelope читается только из stdout

Bash-узел сохраняет stdout и stderr раздельно. Совместимое поле `output` объединяет оба потока и используется для шаблонов, feedback и диагностики, но не является источником структурного результата. Evaluation декодирует `takt-validation/v1alpha1` только из stdout quality-node. Предупреждения и служебные сообщения в stderr не могут повредить корректный envelope; они сохраняются отдельно и остаются видимыми в diagnostic output.

## ADR-030. OpenCode интегрируется через официальный JSON CLI mode

`type: opencode` запускает отдельный `opencode run --format json` на каждую попытку. Prompt передаётся через stdin, stdout читается как NDJSON event stream, stderr сохраняется как диагностика. Takt не парсит TUI и не реализует внутренний tool loop OpenCode.

Resume считается успешным только при совпадении Session ID. Usage и cost одной попытки суммируются по уникальным `step_finish`; event `error` является отказом агента даже при нулевом OS exit code. Если event stream не сообщает иную модель, `resolved_model` фиксирует явно переданный `provider/id` и помечает источник как `requested_cli_model`. Флаг `--auto` доступен только через явное `auto_approve` в trusted workspace.

## ADR-031. Provider diagnostics дополняют context classification OpenCode

Timeout и cancellation остаются authoritative execution kind. При этом OpenCode adapter обязан сохранить доступные сообщения о provider retry и connection failure из stderr и JSON `error` events. Эти данные записываются в raw stdout/stderr, logical output и текст context-ошибки. Scheduler не заменяет специализированную ошибку adapter общим `node attempt`, если execution kind уже совпадает с завершившимся parent context.

## ADR-032. Markdown остаётся авторитетным планом профиля code

**Статус:** принято.

Профиль `code` передаёт coding agent исходный путь и содержимое Markdown-плана. Runtime не преобразует план в обязательный task AST. Формализованные входы могут появляться как расширяемые adapters и использоваться только профилями, которым это действительно нужно.

Это сохраняет сильную сторону coding agents — работу с естественным Markdown-контекстом — и отделяет пользовательский формат задания от runtime-модели DAG.


## ADR-033. Subworkflow и foreach компилируются в обычный DAG

**Статус:** принято.

Takt не вводит отдельный runtime для reusable workflow. `subworkflow` и последовательный `foreach` разворачиваются до запуска в обычные узлы с устойчивым namespace `__`, публичным контейнерным ID и тем же scheduler, persistence, retry, approval и error contract.

Подключённые workflow, локальные Markdown-команды, inputs и `foreach.items` входят в каноническое скомпилированное определение и workflow fingerprint. Изменение любого из них блокирует resume ранее начатого Run.

`foreach` принимает только явно заданный список. Markdown-план остаётся авторитетным документом и не преобразуется ядром в обязательный task AST. Другие форматы подключаются будущими input adapters как отдельное расширение.

## ADR-034. Сохранённое и публичное состояние композиции разделены

**Статус:** принято.

Скомпилированные узлы `subworkflow` и `foreach` сохраняются в полном `RunState` со стабильными namespaced ID. Это состояние является внутренней моделью исполнения и необходимо для pause/resume, проверки fingerprint и восстановления scheduler.

CLI возвращает отдельную публичную проекцию: внутренние узлы скрываются, а `current_node`, `waiting.node_id` и approvals отображаются через публичный ID контейнера. `takt answer` принимает этот ID и разрешает его в фактический approval-узел. Таким образом внешний контракт не зависит от схемы развёртывания, а persistence остаётся точным.

Внешний `foreach.items_from` считается частью определения. Runtime читает явный YAML/JSON-массив до Validate и включает SHA-256 исходных байтов в каноническое развёрнутое определение. `subworkflow` и `foreach` внутри `loop_group` проходят тот же compile-to-DAG этап; отдельный runtime для композиции не вводится.


## ADR-035. Параллельность является scheduler-семантикой одного Run

**Статус:** принято.

Scheduler собирает готовые независимые `command`, `prompt` и `bash` в параллельную волну. Перед запуском все участники последовательно переводятся в `running` и фиксируются в общем журнале; после завершения результаты применяются в стабильном порядке определения узлов. Ошибка одного участника не отменяет остальные уже запущенные действия.

`foreach.parallel` компилируется в такие же независимые DAG-ветви, а aggregator сохраняет порядок исходного списка. Отдельный runtime, отдельный Run или недетерминированная запись state/events для параллельности не вводятся. Узлы с portable hooks и автоматическими повторами пока исполняются последовательным путём, пока для них не определена отдельная конкурентная семантика.

## ADR-036. Каталог и умный роутер являются обычными workflow

**Статус:** принято.

Profile может публиковать карту именованных workflow. Default workflow профиля `code` является аудируемым роутером: агент возвращает решение по проверяемому `output_format`, после чего `when` по JSON-полю открывает ровно одну ветку `subworkflow`. Решение роутера, модель, usage и ошибки остаются частью того же Run и event log.

Прямой селектор `profile:workflow` обходит роутер и нужен для воспроизводимых запусков, тестов и ручного выбора. Каталог из 19 процессов является содержимым профиля, а не захардкоженной логикой Go. Добавление процесса требует изменения `profile.yaml`, workflow и команд, но не нового типа runtime.

## ADR-037. Control workspace and execution worktree are separate Run contexts

Run state, events, locks, artifacts, workflow definitions, config, and Markdown-command fingerprints belong to the control workspace. A workflow may move node execution into a managed Git worktree with its own branch. The router may activate this boundary at a selected subworkflow gate, before the first child action.

Takt removes only a clean successful worktree whose policy is `on_success`; the branch remains. Dirty, failed, cancelled, waiting, manually retained, or uninspectable worktrees are preserved for inspection. A dirty control checkout is rejected by default. Explicit `allow_dirty` starts from a committed base and never pretends to copy uncommitted changes.

This is a local trusted-runtime isolation boundary, not a security sandbox. Server, Web UI, database, remote execution, and multi-user authorization require a separate architecture and threat model.
