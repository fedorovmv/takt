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

**Статус:** принято; execution boundary уточнена ADR-038.

Profile может публиковать карту именованных workflow. Default workflow профиля `code` является аудируемым роутером: агент возвращает решение по проверяемому `output_format`, после чего `when` по JSON-полю открывает ровно одну ветку. В `v0.1.24` ветвь была structural `subworkflow`; с `v0.1.26` она запускается как governed child Run. Решение роутера, модель, usage и ошибки остаются в root Run, а выбранный процесс имеет отдельный event log.

Прямой селектор `profile:workflow` обходит роутер и нужен для воспроизводимых запусков, тестов и ручного выбора. Каталог из 19 процессов является содержимым профиля, а не захардкоженной логикой Go. Добавление процесса требует изменения `profile.yaml`, workflow и команд, но не нового типа runtime.

## ADR-037. Control workspace and execution worktree are separate Run contexts

Run state, events, locks, artifacts, workflow definitions, config, and Markdown-command fingerprints belong to the control workspace. A workflow may move node execution into a managed Git worktree with its own branch. The router remains in the control workspace; since ADR-038 the selected governed child applies its own worktree policy before its first action. Structural subworkflow gates still support local policy activation when used directly.

Takt removes only a clean successful worktree whose policy is `on_success`; the branch remains. Dirty, failed, cancelled, waiting, manually retained, or uninspectable worktrees are preserved for inspection. A dirty control checkout is rejected by default. Explicit `allow_dirty` starts from a committed base and never pretends to copy uncommitted changes.

This is a local trusted-runtime isolation boundary, not a security sandbox. Server, Web UI, database, remote execution, and multi-user authorization require a separate architecture and threat model.

## ADR-038. Structural composition and governed child Runs are different contracts

`subworkflow` and `foreach` remain compile-time structural composition inside one Run and one scheduler. The new `workflow` node starts a separately persisted governed child Run with its own identity, state, events, artifacts, usage and worktree policy. Parent and child are linked explicitly; approval, resume and cancellation traverse that link without merging storage.

A child uses its own workflow policy by default. `isolation: inherit` shares the parent's execution workspace while preserving a separate lifecycle; `worktree` forces a separate managed worktree; `none` uses the control workspace. Retrying a governed node creates a new child Run rather than mutating the terminal child attempt. Static child definitions participate in the parent fingerprint, recursion is rejected, and depth is limited to 16. Server, Web UI and database are not required for this local file-backed lifecycle.


## ADR-039. Политика AI-узла проверяется до запуска adapter

**Статус:** принято.

`allowed_tools`, `denied_tools`, `skills`, `mcp`, `sandbox` и `requires` являются частью определения workflow. Runtime вычисляет effective policy до вызова assistant, проверяет `Adapter.Capabilities()`, сохраняет применённый контракт в state и передаёт его adapter. Неподдерживаемая возможность является ошибкой до запуска процесса и не может молча игнорироваться.

Явные пустые `allowed_tools: []` и `skills: []` являются заданными пустыми allowlists. Governed child Run наследует policy как верхнюю границу: allowlists пересекаются, deny/requirements объединяются, более строгий sandbox сохраняется, inherited MCP нельзя заменить. Policy resources входят в fingerprint.

Filesystem/network policy текущего локального runtime является assistant-enforced contract, а не OS sandbox. Встроенные adapters объявляют только реально реализованные зарезервированные capabilities; `process` может объявить их явно, поскольку исполнение лежит на внешнем adapter.

## ADR-040. Worktree paths canonicalize symbolic links and empty branches are disposable

**Статус:** принято.

Перед проверкой принадлежности workspace репозиторию Takt разрешает символические ссылки для workspace и результата `git rev-parse --show-toplevel`. Это устраняет различие логического и физического пути на macOS и в пользовательских symlinked workspaces.

После удаления worktree ветка удаляется только если её head по-прежнему равен записанному base commit. Ветка с коммитами считается результатом Run и сохраняется. Terminal failed/completed/cancelled Run нельзя переопределить последующей командой cancel.

## ADR-041. Runtime fan-out создаёт устойчивую группу governed child Runs

`workflow.fan_out` разрешает список только из структурированного output upstream-узла. Родитель до исполнения сохраняет fingerprint массива, индекс, канонический элемент и отдельный Run ID каждого ребёнка. Resume переиспользует terminal-детей и продолжает waiting-детей; изменение списка в той же попытке отклоняется. Retry создаёт новую группу без перезаписи истории.

`max_parallel` ограничивает активные дочерние выполнения, а итог всегда агрегируется в порядке исходного массива. Join policy (`all_success`, `all_done`, `one_success`) влияет на статус родительского узла, но не скрывает статусы и диагностику отдельных детей. Cancellation marker сохраняет возможность отменить один child Run или всю группу через родителя.
## ADR-042. Script-узел остаётся детерминированным процессом, а артефакт — сохраняемым снимком

`script` не является новым assistant и не получает скрытый tool loop. Runtime запускает явно указанный `command`, `python` или `node`, сохраняет stdout/stderr и применяет тот же timeout/cancellation и `output_format`, что к другим действиям. Исходник и объявленные dependencies входят в fingerprint.

`output_type` создаёт локальный неизменяемый снимок результата либо указанного файла после успешного завершения узла. Ссылка содержит type, MIME, SHA-256, размер и producer Run/Node/attempt. Governed children передают ссылки родителю, но файл остаётся в хранилище фактического producer Run; это сохраняет provenance и не требует соглашений о случайных именах файлов.



## ADR-043. Локальный MCP является адаптером существующего control plane

**Статус:** принято.

`takt mcp` не запускает CLI как подпроцесс и не создаёт отдельную модель состояния. MCP tools вызывают общий локальный control service, который использует тот же runtime, file store, fingerprints, locks, governed child lifecycle и artifact references. Это исключает расхождение CLI и MCP по основным гарантиям исполнения.

Run state остаётся явным handle в аргументах tools. Detached start возвращает `run_id`, а события читаются по durable revision cursor; MCP session не становится источником истины. Поддерживаются legacy initialize и stateless discovery, поскольку локальные coding-agent hosts обновляются не одновременно.

Транспорт ограничен stdio и полномочиями текущего процесса. HTTP, daemon, authentication, multi-user use и untrusted workflow не выводятся из факта наличия MCP и требуют отдельной threat model.

## ADR-044. Внешний executor является durable hand-off внутри обычного Run

**Статус:** принято.

`executor: external` не создаёт второй scheduler и не считает MCP-сессию источником истины. Takt до передачи полностью разрешает command/prompt, model/session, policy и output contract, сохраняет задачу в NodeState и приостанавливает обычную попытку. Worker получает bounded lease и opaque token, подтверждает capabilities, пишет нормализованные assistant/tool events и возвращает результат. После submission Takt продолжает прежнюю попытку через обычные retry, hooks, output validation, artifacts и parent/child lifecycle.

Built-in adapters используют тот же provider-neutral event contract. Raw provider streams сохраняются отдельно. Event journal остаётся авторитетным: indexed reads ускоряют polling, а `FS.Load` лечит только кратковременный torn read между atomic renames и по-прежнему отвергает устойчивую несогласованность после сбоя.

## ADR-045. Tool call является сохраняемой управляемой единицей внешнего узла

**Статус:** принято.

Наблюдательное событие `tool.started` не считается контролем инструмента. Adapter может заявить `tool_control` только если Takt получает запрос до фактического запуска и способен вернуть сохраняемое решение `allow|deny`. Внешний executor хранит каждый вызов по устойчивому `call_id`, применяет effective node policy до approval, поддерживает blocking decision, отдельную отмену и связывает объявленные артефакты с вызовом.

Внешний узел не может вернуть terminal-result при незавершённом tool call. `cancel_requested` не считается terminal: worker обязан прекратить действие и подтвердить завершение. OpenCode и Pi публикуют наблюдательные events без `tool_control`, пока их provider interface не предоставляет pre-execution interception.

## ADR-046. Основные workflow являются проверяемыми предметными процессами

**Статус:** принято.

Шесть основных workflow профиля `code` принимают строгий JSON input и разделяются на специализированные фазы. Каждая значимая фаза обязана вернуть checkpoint с ограниченным domain code, evidence и artifact path, а также сохранить типизированный артефакт. Git preparation, validation, recovery и PR finalization являются явными узлами, а не неформальными инструкциями внутри одной универсальной команды.

Сквозные contract tests используют настоящий локальный Git repository, bare remote, fake `gh` и детерминированный process adapter. Это проверяет orchestration, ветки, коммиты, push, recovery и provenance без зависимости от GitHub network. Production workflow по-прежнему использует фактический coding agent и `gh`/GitHub environment пользователя.

## ADR-047. Authoring preflight является частью исполняемого контракта

**Статус:** принято.

Takt проверяет не только синтаксис YAML, но и разрешимость значимых ссылок до создания Run. Неизвестные поля получают path-aware `did you mean`; capability-проверка использует фактический adapter; `${nodes.*}`, approvals и artifacts анализируются относительно DAG и объявленного `output_format`. Семантические подозрения являются diagnostics и могут стать ошибками через `--warnings-as-errors`.

Renderer является fail-closed: `${path}` обязателен, `${path?}` явно optional, `${path:-default}` задаёт fallback. Неразрешённая обязательная ссылка не сохраняется как буквальный текст и не передаётся модели. `always_run` и `idle_timeout` являются runtime-семантикой, а не prompt convention.

## ADR-048. Локальный daemon использует файловый Store и Unix socket

**Статус:** принято.

`takt daemon` добавляет время жизни процесса, а не второй runtime. CLI, event subscriptions и MCP вызывают общий `control.Service`; состояние, revisions, locks, fingerprints, child lifecycle и artifacts остаются в `.takt/runs`. Daemon слушает только Unix socket с правами текущего пользователя, не открывает TCP и не вводит БД.

Один workspace допускает один daemon через локальный file lock. Concurrent управляющие запросы сериализуются bounded retry на уровне control plane, при этом Store lock остаётся неблокирующим. Daemon гарантирует продолжение после закрытия клиента. После падения он не продолжает прежний OS-процесс, но следующий daemon выполняет PID-based recovery durable `running|pausing` Run: помечает attempt как `worker_lost`, возвращает node в `pending` и запускает новую attempt. Внешние side effects требуют идемпотентности adapter/workflow.

## ADR-049. WorkflowPlan является ограниченным планом компиляции

**Статус:** принято.

Dynamic Takt не вводит второй workflow runtime и не разрешает модели генерировать произвольный оркестрационный код. `WorkflowPlan` содержит только цель, бюджеты, зависимости, `task|map`, checkpoint и ссылки на разрешённые workflow-блоки профиля. После проверки каждая редакция компилируется в обычный `takt/v1alpha1 Workflow` с governed child Run и `workflow.fan_out`. Единственным источником исполнительной семантики остаётся существующий scheduler/runtime.

## ADR-050. Перепланирование изменяет только незавершённый хвост

**Статус:** принято.

Dynamic planner вызывается только в явных checkpoint. Решение `replace_remaining` создаёт новую immutable revision и может заменить только ещё не начатые фазы. Завершённые фазы, Run, события, usage и артефакты сохраняются как история и не переписываются моделью. Steering является входом ближайшего checkpoint, а не произвольной мутацией активного DAG.

## ADR-051. Доверенный BlockPackage является границей динамического планирования

**Статус:** принято.

Dynamic Takt не обнаруживает блоки автоматически и не строит путь workflow из имени, возвращённого моделью. Профиль явно перечисляет локальные `BlockPackage`; загрузчик проверяет каждый манифест и workflow, формирует типизированный каталог и сохраняет общий fingerprint в записи плана. Execute, replan и promote отклоняются после изменения любого подключённого пакета или его workflow.

Корпоративная политика применяется ко всему объединённому каталогу, включая встроенные блоки. Разрешающие ограничения пересекаются, запрещающие и обязательные требования объединяются, budgets сужаются до минимального положительного значения. Конфликтующие branch/change-request правила являются ошибкой, а не выбором по неявному приоритету.

Доверенный блок остаётся атомарным относительно WorkflowPlan и не запускает governed child Run. Дочерние Run создаются только фазами плана, чтобы `max_child_runs` и происхождение результатов оставались проверяемыми. Для `map` допустим только точно объявленный `output_path` типа `array`.

## ADR-052. Dynamic plan имеет один учтённый жизненный цикл во всех интерфейсах

**Статус:** принято.

Прямой CLI и прямой stdio MCP выполняют Dynamic Plan в процессе вызова до terminal либо устойчивого waiting-состояния. Отсоединённое выполнение доступно только через локальный daemon и явно отражается в записи плана. Это исключает goroutine, которая теряется вместе с завершением короткоживущего CLI-процесса.

`max_child_runs` и `max_tokens` учитывают planner, replanner, segment wrapper, governed phases и fan-out children. `max_parallel` ограничивает одновременно независимые task-фазы и fan-out. Любое перепланирование, включая ответ через steering, проверяет `max_iterations`; steering помечается применённым только после валидного решения. Продвижение планов между daemon и прямым MCP сериализуется межпроцессным file lock.

## ADR-053: Strict workflow starts in the coding-agent host before the main LLM

**Решение.** `/takt` является нативной командой Pi/OpenCode. Host extension создаёт durable host session, перехватывает последующий input до model dispatch, проверяет tool calls до исполнения и блокирует final completion до terminal-состояния Takt. Skill/MCP без этих возможностей могут заявлять только advisory/guarded, но не strict.

**Причина.** После запуска Takt контролирует DAG, но обычная основная LLM могла не вызвать Takt или параллельно изменить код. Enforcement обязан находиться в хосте, которому принадлежат input и tools.

## ADR-054: Trusted package fingerprint covers the transitive executable closure

**Решение.** Fingerprint блока включает package manifest, expanded workflow/subworkflow, разрешённые Markdown-команды, script source и dependencies, path skills и MCP-конфигурации.

**Причина.** Хэш только wrapper workflow не обнаруживает изменение фактической инструкции или исполняемого ресурса после preview.


## ADR-055. Автономная эксплуатация Run остаётся проекцией файлового Store

**Статус:** принято.

Реестр, attention queue, summary и уведомления не вводят отдельную БД или второй жизненный цикл. `run.list` и `run.summary` читают обычные RunState, notification dispatcher хранит только snapshot переходов и durable inbox. Ошибка доставки desktop/process не меняет статус Run; источником истины остаются state/events/artifacts.

Безопасная pause прекращает запуск новых узлов и fan-out batches, позволяет активной attempt дойти до границы и затем переводит Run в `paused`. `retry` сбрасывает только выбранный failed node и зависимый хвост, `fork` создаёт новый запуск, а `abandon` является отдельным terminal-состоянием с сохранённой историей.

## ADR-056. Восстановление daemon является PID-based повтором attempt

**Статус:** принято.

При старте daemon обнаруживает `running|pausing` Run, чей `executor_pid` больше не существует. Текущая execution record получает `worker_lost`, consumed attempt возвращается, node становится `pending`, recovery count увеличивается, а дочерние Run восстанавливаются раньше родителей. Это не продолжение того же provider-процесса и не exactly-once гарантия. Критичные внешние операции обязаны использовать идемпотентные адаптеры, ключи операции или отдельные детерминированные узлы.

## ADR-057. Task Router компилирует все маршруты в один runtime

**Статус:** принято.

Пользовательская задача сначала получает `TaskRoute`: готовый workflow,
стабильный `simple-reliable` template или bounded Dynamic Plan. Любой результат
после проверки компилируется в обычный `takt/v1alpha1 Workflow` и исполняется
существующим scheduler. Отдельные runtime для Autopilot, TaskRunner и dynamic
composition не вводятся.

Semantic router является оптимизацией выбора, а не обязательной зависимостью.
Его ошибка приводит к inspect-first варианту стабильного шаблона. Модель не
может добавлять неизвестные workflow/blocks, ослаблять детерминированные controls
или расширять governance.

## ADR-058. Профиль зависит от логического coding-agent, а не от продукта

**Статус:** принято.

Workflow и Markdown-команды встроенного профиля используют имя
`coding-agent`. `.takt/config.yaml` связывает его с конкретным assistant через
`default_assistant`. Pi и OpenCode остаются встроенными адаптерами; Codex, Oh My
Pi, Qwen CLI и другие хосты подключаются внешней обёрткой по
`takt-assistant/v1alpha2` либо будущим прямым адаптером.

Каноническая граница называется `SessionAdapter`. Она не предполагает Kiro CLI,
ACP или иной единственный provider protocol. Существующее имя `Adapter`
сохраняется как совместимый alias.

## ADR-059. MCP публикует разные поверхности для разных ролей

**Статус:** принято.

Операции agent, host, external worker и operator не публикуются одной основной
LLM. `agent` surface содержит только пять высокоуровневых `takt.task.*`; host
получает managed-session guards, worker — node/tool lifecycle, operator — явное
управление workflow/plan/Run/block/notifications. `all` сохраняется для
совместимости и диагностики.

Surface фиксируется при создании MCP-подключения или daemon-запроса. Tool call
не может изменить роль. Внутренние операции остаются гранулярными; сокращение
внешнего интерфейса не достигается объединением действий с разной семантикой.

## ADR-060. RoleDefinition и TaskBrief являются внутренними контрактами Takt

**Статус:** принято.

Пользователь не устанавливает отдельные planner/implementer/tester/verifier agents и не пишет TaskBrief вручную. Trusted `BlockPackage` объявляет функциональные роли, а Takt перед каждым child Run компилирует новый bounded `TaskBrief` из цели, objective, risk signals, scope и явно разрешённых предыдущих результатов. Кодинг-агент остаётся заменяемой исполнительной средой.

`model_profile` и `session` роли обязаны совпадать с фактическим atomic workflow блока. Read-only policy применяется только там, где adapter подтверждает capability; Takt не выдаёт post-hoc проверку `changed_files` за path-level OS sandbox.

## ADR-061. Проверка выбирает deny, bounded repair или warn, а не единое blocked

**Статус:** принято.

Checks доверенного блока имеют `required|preferred` и реакцию `deny|repair|warn`. Preferred failure всегда остаётся warning. Required `repair` запускает не более одной автоматической repair-итерации на устойчивый `block:check`, после чего повторный отказ требует одного явного решения пользователя. `deny` предназначен для нарушения обязательной границы; `warn` сохраняет диагностику без остановки.

Эта модель отделяет безопасность от качества: обычная исправимая техническая ошибка не должна превращаться в операторский gate, а необязательная усиленная проверка не должна делать Takt неудобным для повседневной работы.

## ADR-062. Verdict принадлежит конкретному candidate content fingerprint

**Статус:** принято.

Dynamic Takt хранит `EvidenceManifest` отдельно от свободного текста worker-а. Baseline, результаты trusted checks и итоговый verdict связываются с `candidate_sha`, вычисленным как SHA-256 базового commit, бинарного Git diff и содержимого untracked files execution workspace. Изменение candidate после verdict делает verdict `stale`; completion не переиспользует доказательство от другой версии содержимого.

Baseline failures сравниваются по детерминированному normalized fingerprint. Совпадение считается исходной проблемой и не запускает automatic repair; неясное или новое падение считается регрессией. LLM similarity не участвует в этом решении.

## ADR-063. Неизвестный внешний side effect нельзя повторять без reconciliation

**Статус:** принято.

`executor: external` может объявить `side_effect.mode: reconcile`. После истечения claim такого узла новый worker не получает право повторить действие, пока внешний adapter не классифицирует факт как `not_applied`, `applied` или `unknown`. `not_applied` разрешает новый claim; `applied` требует receipt и результат и завершает узел через обычный submit path; `unknown` сохраняет ожидание и блокирует replay.

Это не exactly-once гарантия внешней системы. Контракт предотвращает опасный blind retry и задаёт runtime seam для будущих SCM/tracker/CI adapters.


## ADR-064. Доменные интеграции являются нейтральными adapter-узлами единого runtime

**Статус:** принято.

SCM, tracker и CI подключаются через именованные `adapters` в Config и нейтральные операции (`change.create`, `item.get`, `run.start`). Workflow не содержит GitHub/Jira/корпоративные transport details. `adapter` является обычным действием Node: существующий scheduler применяет dependencies, attempts, timeout, hooks и structured output, а transport `process|mcp` скрыт за `domainadapter.Adapter`. Отдельный integration runtime не создаётся.

Capability discovery выполняется до вызова. Для `side_effect.mode: reconcile` поддержка сверки также обязана быть объявлена до первого внешнего изменения. Неопределённый результат запрещает blind retry и сначала проходит reconciliation с тем же durable idempotency key.

## ADR-065. Agent Adapter SDK проверяет контракт, но не объявляет неподтверждённые возможности хоста

**Статус:** принято.

`sdk/agentadapter` предоставляет product-neutral conformance kit для `takt-assistant/v1alpha2`: protocol/event/result/session invariants проверяются одинаково для Codex, Oh My Pi, Qwen CLI и других wrappers. Наличие transcript-conformance не повышает adapter до `tool_control`, strict completion gate или reliable resume; такие capabilities заявляются только после product-specific live/fixture проверки.

## ADR-066. Установленный BlockPackage остаётся тем же trusted catalog, а доставка фиксируется lock-файлом

**Статус:** принято.

Package distribution не вводит новый workflow/package runtime. `takt package install|update|sync` копирует целый `BlockPackage`, проверяет manifest, dependency/requirements, source policy, checksum и при необходимости Ed25519 signature, затем фиксирует точную поставку в `takt.lock.json`. `profile.Resolve` подключает locked package manifests к существующему `blockcatalog`.

Для конфликтующего имени блока применяется `project > corporate > global > builtin`. Governance всех уровней при этом продолжает объединяться fail-closed: precedence выбирает реализацию, но не является способом ослабить верхнеуровневые ограничения.

**Причина.** Пользователю нужна переносимая и воспроизводимая установка без второго DSL и без ручного списка `block_packages`. Lock отделяет желаемый источник от реально проверенного содержимого и позволяет восстановить Git-пакет по commit, обнаружить drift local source и выполнить capability preflight до Run.

## ADR-067. Package trust policy проверяет источник и содержимое до активации

**Статус:** принято.

Project/global `PackagePolicy` могут ограничивать префиксы local/Git sources, требовать подпись для выбранных scopes и задавать trusted Ed25519 public keys. Digest строится по всему дереву пакета, кроме `.git` и envelope `package.sig`; lock хранит SHA-256, source commit и факт проверки подписи. Установка сначала полностью проверяет staged content и dependency graph, только затем заменяет установленную версию и lock entry.

**Причина.** Корпоративный package может нести workflow, scripts, skills и MCP-конфигурацию, то есть исполняемый trusted content. Проверка только имени/версии создаёт ложное чувство воспроизводимости; checksum/source/signature policy делает изменение поставки наблюдаемым и fail-closed.

## ADR-068. Multi-repo uses repository-owned governed child Runs, not a second runtime

**Статус:** принято.

`.takt/workspace.yaml` описывает bounded catalog локальных Git-репозиториев и их зависимости. Repository-aware `WorkflowPlan` остаётся промежуточным планом компиляции. Каждая mutating repository phase компилируется в обычный governed child Run с `workflow.repository` и собственным managed worktree; scheduler, store, retry, pause, evidence и adapter semantics остаются общими.

Repository path проверяется относительно control workspace и повторно после symlink resolution. Catalog fingerprint включает repository HEAD и проверяется перед execute/replan. Первый контракт допускает один mutating owner phase на repository: это сохраняет один кандидат/worktree на репозиторий и не создаёт скрытую модель межсессионного merge.

**Причина.** Multi-repo должен расширять область задачи, а не вводить distributed runtime. Изоляция по child Run даёт существующие recovery/evidence guarantees и позволяет продолжать только незавершённый хвост.

## ADR-069. Cross-repo publication is a neutral adapter action and evidence remains repository-addressable

**Статус:** принято.

`publish_change` не добавляет SCM-провайдера в WorkflowPlan. Compiler создаёт обычный `adapter` node `scm/change.create`, который использует Adapter Platform, durable idempotency key и reconciliation. Repository dependency graph формирует deterministic merge order; provider-specific dependency metadata является возможностью внешнего adapter, а не частью runtime.

Каждый repository execution сохраняет candidate SHA, child worktree metadata, evidence и publisher output. Общий candidate fingerprint агрегирует repository fingerprints; фактический diff агрегируется как `<repository-id>/<path>` и сверяется с repository worker `changed_files`. Integration verification является обычным trusted read-only block.

**Причина.** Пользователь должен получить несколько переносимых change requests и общий verdict без потери доказательств конкретного репозитория и без GitHub-specific orchestration.

## ADR-070. Retry decisions and fan-out termination are durable runtime state

**Статус:** принято.

Retry остаётся явным свойством node policy. При настроенном backoff runtime фиксирует точный `not_before`, delay, execution kind и diagnostic fingerprint до ожидания; restart/resume не пересчитывает уже принятое решение. Нормализованный diagnostic является machine-readable записью конкретной попытки, а не результатом LLM similarity.

`workflow.fan_out` short-circuit выполняется тем же scheduler: `one_success` может остановить siblings после первого success, `all_success` — после первого failure. Отмена, вызванная уже известным результатом join, имеет отдельную причину `fanout_result_decided` и не смешивается с operator cancellation.

**Причина.** Автономный Run должен переживать восстановление без изменения времени retry и не тратить ресурсы после того, как результат fan-out уже детерминирован. Для этого не нужен второй scheduler или новый тип Run.

## ADR-071. Secret protection and OS sandbox are separate local security layers

**Статус:** принято.

Takt не хранит секреты самостоятельно. `secret://ENV_NAME` разрешается непосредственно перед process/script execution; известные значения удаляются из durable Run state, event data и текстовых artifacts. Non-text artifact с известным секретом отклоняется, потому что байтовая подмена сделала бы artifact недостоверным.

Assistant sandbox без `enforcement` остаётся capability-контрактом конкретного coding-agent. Реальный OS wrapper применяется только к локальным `bash/script` nodes, которые Takt запускает сам: `bubblewrap` на Linux или `sandbox-exec` на macOS при наличии. `enforcement: required` fail-closed до payload execution, `optional` сохраняет degraded decision.

**Причина.** Нельзя называть инструкцию coding-agent системной песочницей и нельзя делать секретное значение частью durable orchestration state. При этом текущий продукт остаётся trusted local single-user runtime; server/RBAC/Vault/container orchestration требуют отдельной threat model.


## ADR-072. Strategy benchmark is a measurement layer over the existing runtime

**Статус:** принято.

`EvaluationMatrix` запускает обычные Takt workflows через существующий evaluation runner. Baseline и candidates различаются workflow/config fingerprints; отдельного benchmark scheduler или специального execution runtime нет. Pairing выполняется по `case_id + repeat`, а сравнение допускается только при одинаковом benchmark fingerprint.

**Причина.** Средство измерения не должно менять исследуемую orchestration semantics. Matrix делает сравнение воспроизводимым, но оставляет Run, retries, evidence и coding-agent adapters теми же, что и в обычном использовании.

## ADR-073. Time-to-valid and experiment identity come from durable evidence

**Статус:** принято.

True time-to-valid вычисляется от `Run.CreatedAt` до durable `node.completed` выбранного quality-node при `valid=true`. Retry/failure fingerprints читаются из durable events/state. `experiment_fingerprint` зависит от benchmark ID, repeat и strategy/benchmark fingerprints, но не от временного пути matrix-файла. CaseManifest входит в benchmark identity.

**Причина.** Амортизированное время всего эксперимента не отвечает на вопрос, когда появился первый корректный результат, а путь временного файла не должен создавать новую экспериментальную идентичность. Измерения должны переживать перенос каталога и опираться на сохранённые факты runtime.

## ADR-074. Task-level evaluation measures the public control path, not a simulated planner

**Статус:** принято.

`TaskEvaluationMatrix` вызывает обычные `control.Plan` и `ExecutePlan`, использует builtin/profile Router, Dynamic Plan, checkpoint replanner и existing runtime. Evaluation package не реализует альтернативный Router или planner. Результат читается из durable `dynamicplan.Record` и Run state.

**Причина.** Нужно измерять реальную пользовательскую семантику `/takt`, включая ошибочный route и фактическое `replace_remaining`. Benchmark, который только декодирует заранее подготовленный RouteDecision, не проверял бы главный слой Dynamic Takt.

## ADR-075. Redaction является общей persistence boundary runtime и control/external paths

**Статус:** принято.

Known-secret redaction применяется перед durable commit независимо от того, кто изменяет Run: scheduler/runtime, control API или external worker protocol. External text artifacts редактируются до записи, non-text artifacts с known secret отклоняются. Explicit SecretRef регистрируется и после template rendering adapter environment.

**Причина.** Append-only event stream и artifacts нельзя исправить следующим commit. Защита только в `Runner.commit` оставляет внешние worker/control paths отдельным обходом и противоречит заявленному durable security contract.

## ADR-076. Loop iteration history is durable and bounded; nested loop_group stays out of v0.2

**Статус:** принято.

Каждый завершённый проход `loop_group` сохраняется как immutable `LoopIterationState` в `NodeState.loop_iterations`. История содержит iteration number, child node states, `until` result, `satisfied` и completion time. `loop_previous` сохраняется как совместимое представление последней завершённой итерации и не удаляется в `v0.2`.

Чтобы full history оставалась bounded частью Run state, `loop_group.max_iterations` ограничен 64. Public view применяет к каждому snapshot те же правила скрытия expanded/internal IDs, что и к обычному состоянию. Redaction рекурсивно покрывает iteration history.

Nested `loop_group` остаётся запрещённым в `v0.2` и первом `v1beta1`. `subworkflow`, `foreach`, governed child Runs и approval внутри loop уже дают необходимую композицию; рекурсивная loop-семантика не замораживается без production use case и отдельной проверки resume/evidence.

## ADR-077. v0.2 freezes only stable-candidate contracts, not every observable alpha format

**Статус:** принято.

Перед `v1beta1` контракты делятся на четыре категории: `stable-candidate`, `supported-alpha`, `deprecated`, `internal`. Workflow/Config/BlockPackage, durable Run lifecycle, typed artifacts, пять `takt.task.*` и neutral domain operations являются stable-candidate. Dynamic Plan/evaluation formats, Adapter SDK и advanced MCP host/worker/operator surfaces остаются supported-alpha до production evidence. `takt-assistant/v1alpha1` считается deprecated в пользу `v1alpha2`, но продолжает читаться ради совместимости. Store layout, expanded IDs, daemon socket и fake fixtures являются internal.

`v0.2` продолжает принимать `takt/v1alpha1`; additive state changes не требуют новой authoring apiVersion. Финальная field-by-field migration в `v1beta1` выполняется только после live Route DSL, Go и Document evidence.

## ADR-078. Structured JSON contracts use a versioned Takt subset, not implicit full JSON Schema

**Решение.** `Workflow.input.schema` и `Node.output_format` используют единый `takt-schema-subset/v1`. Реализация validation/normalization находится в одном пакете `internal/schemasubset`; текущий набор types/keywords фиксируется как контракт v0.2. Полный JSON Schema (`$ref`, `oneOf`, schema-valued `additionalProperties` и т. п.) не заявляется.

**Почему.** Поддержка произвольного JSON Schema потребовала бы отдельного полноценного validator/compiler и создала бы скрытые различия между editor schema, authoring и runtime. Текущий subset уже покрывает фактические structured contracts Takt. Новые keywords должны добавляться только по evidence и с явной версией совместимости.

## ADR-079. Compatibility is reported separately for session adapters, host integrations and domain adapters

**Решение.** Takt публикует `takt compatibility matrix|fields|schema|check`. Compatibility matrix не смешивает session adapter contract с host-control enforcement и domain transport. Live version probe или synthetic fixture не повышают host integration до `strict`; strict требует отдельного live conformance на pinned host version.

Field matrix автоматически перечисляет audited public JSON fields и имеет contract-test на точный набор полей stable-candidate authoring/config contracts.

**Почему.** До `v0.1.48` сведения о Pi/OpenCode version fixtures, host `guarded` status и process protocol deprecation находились в разных документах и легко воспринимались как одна гарантия. Машиночитаемая матрица делает границы совместимости проверяемыми и пригодными для CI/preflight.

## ADR-080. Reference adapters consume public SDKs and may reveal missing neutral context

**Статус:** принято.

Первый внешний coding-agent wrapper и первый production-like SCM adapter поставляются в `reference/` и `cmd/`, но не импортируют `internal/`. Их задача — проверять достаточность `sdk/agentadapter` и `sdk/domainadapter`, а не создавать provider-specific ветки runtime.

GitHub reference adapter выявил нейтральный недостающий контекст: domain operation должна знать execution workspace. Поэтому `workspace` добавлен в публичные Invoke/Reconcile request, process transport запускается в этом каталоге, а multi-repo publication передаёт точный repository child worktree. GitHub URL, PR number и `gh` остаются только внутри reference adapter.

**Причина.** Public seam считается доказанным только когда полезная внешняя реализация может работать без `internal/`; обнаруженный универсальный контекст исправляется в SDK, provider-specific детали — нет.

## ADR-081. Process v1alpha2 transport does not imply tool-control capability

**Статус:** принято.

`takt-assistant/v1alpha2` определяет транспорт, способный переносить declaration/events/tool requests/result. Он не означает, что каждый wrapper способен блокировать tool call, проектировать skills/MCP или обеспечивать sandbox. Static capability preflight использует только явно configured capabilities; stream declaration проверяется отдельно и является авторитетным утверждением конкретного запуска.

Runtime отклоняет configured capability, которой нет в stream declaration, event вне declared event_types и tool request без `tool_control=true`.

**Причина.** Иначе узкий observational wrapper, например Qwen Code headless reference adapter, получал бы ложную security guarantee только из-за версии transport protocol.

## ADR-082. Task Sources are an ingress boundary before Router, not workflow domain operations

**Статус:** принято.

Внешний issue/tracker/PRD/OpenSpec разрешается через `takt-task-source/v1alpha1` до вызова Task Router. Source adapter возвращает нормализованный Task; затем используется существующий `Plan → Router → Dynamic Plan → runtime`. Task Source не является `adapter`-узлом и не использует domain side-effect/reconcile lifecycle.

**Причина.** Получение авторитетного входа задачи и внешние SCM/tracker/CI действия имеют разную семантику. Их объединение сделало бы ingestion частью DAG и вынудило бы workflow знать provider-specific источник задачи.

## ADR-083. Resolved task provenance is immutable within one plan revision lineage

**Статус:** принято.

Нормализованный Task содержит `source.adapter/kind/reference/revision/url` и сохраняется в `dynamicplan.Record`. Router, Planner и Replanner получают тот же структурированный `task_source`; resume/replan не перечитывают источник автоматически. Для старых workflow дополнительно строится совместимый текстовый `GoalText`.

**Причина.** План и evidence должны быть привязаны к определённой ревизии входа. Тихое перечитывание изменившегося issue во время resume/replan сделало бы fingerprint/решения невоспроизводимыми. Новая ревизия внешней задачи должна начинать новый plan/run lineage.

## ADR-084. Learning produces reviewed staged candidates, never implicit trusted-package mutation

**Статус:** принято.

Human-reviewed learning использует durable Run history только как источник повторяемых устойчивых сигналов. Proposal фиксирует supporting Run IDs, expected benefit и immutable SHA-256 snapshot skill/BlockPackage candidate. Переход к `ready` требует отдельного human `accept` с rationale и passing versioned evaluation matrix с regression gates.

`stage` копирует кандидат только в `.takt/learning/ready/<proposal-id>`. Он не изменяет package lock, profile `block_packages`, global/corporate scopes и assistant skill configuration. Подключение staged candidate является отдельным явным действием.

**Причина.** История Run является evidence для предложения, но не источником доверия. Автоматическое обучение, сразу влияющее на следующие исполнения, создало бы самоподдерживающуюся мутацию control plane без независимого review/regression boundary.

## ADR-085. Takt uses an explicit application boundary and one production composition root

**Статус:** принято.

`cmd/takt`, CLI, MCP и daemon являются transport adapters и не владеют Run/runtime business semantics. Use cases находятся в `internal/application` и разделены на сервисы Run, Plan, Task, External, Host, Catalog, Authoring, Worktree, Command, Notification, Maintenance, Evaluation, Learning, Compatibility, Adapter и Package. Production concrete dependencies собираются в `internal/bootstrap`. CLI не вызывает runtime/store/evaluation/learning/package engines напрямую.

Общие daemon/MCP операции используют один `internal/appapi` handler registry со strict decode/default semantics. Daemon не реализует отдельный application switch. MCP получает только canonical API, Plan, External и Maintenance dependencies; его собственными остаются только MCP-specific protocol operations и явно отличающаяся foreground semantics `takt.execute`.

Application зависит от filesystem persistence через consumer-owned `RunStore`; production связывает его с текущим `store.FS`. Evaluation зависит от injected `EvaluationEngine`. Runtime создаётся через явные `Definition + Dependencies`; dependency fields `Runner` закрыты и задаются при construction, а не мутируются после него. Scheduler/attempt lifecycle/action implementations разделены, но конечный набор node actions остаётся closed-world: новый generic plugin/DI/event-bus framework не вводится.

Архитектурные import boundaries, отсутствие concrete store construction в application, private runtime dependencies и тонкий `cmd/takt` проверяются `scripts/test-architecture.sh`, входящим в release gate.

**Причина.** К `v0.1.51` один `control.Service`, крупный CLI и отдельные MCP/daemon dispatch paths нарушали SRP/DIP/ISP и создавали несколько мест для одинаковой validation/default semantics. Простое разбиение файлов не устраняло бы эту связность. Application boundary делает операции переиспользуемыми и тестируемыми, DRY оставляет одну бизнес-семантику, а KISS/YAGNI сохраняют обычные Go structs/interfaces вместо нового framework или инфраструктуры без подтверждённой потребности.
