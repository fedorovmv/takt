# План развития

Документ показывает приоритеты после `v0.1.35-alpha`. Server, Web UI и БД остаются proposal-направлением для возможного нелокального режима и не определяют ближайший локальный runtime.

## Выполнено в v0.1.27-alpha. Политики и возможности узлов

Реализованы `allowed_tools`, `denied_tools`, explicit empty allowlists, skills, MCP, assistant-enforced filesystem/network policy, `requires`, capability preflight, fingerprints ресурсов, persistence и наследование governed child Run. Неподдерживаемая гарантия отклоняется до вызова adapter.

## Выполнено в v0.1.28-alpha. Динамический fan-out governed children

Реализованы массив элементов из структурированного output upstream-узла, один child Run на элемент, `max_parallel`, устойчивые IDs и fingerprints, частичный resume, ordered aggregation, `all_success|all_done|one_success`, выборочная и каскадная отмена. Smart review и comprehensive review профиля `code` используют этот механизм.

## Выполнено в v0.1.29-alpha. Script nodes и типизированные артефакты

Реализованы runtime `command|python|node|go`, file/inline source, args/env/working directory, fingerprints исходника и dependencies, structured output, `output_type`/MIME/SHA-256/producer metadata, CLI `takt artifacts` и передача ссылок parent/child/fan-out. Профиль `code` использует script и plan/PRD artifacts.

## Выполнено в v0.1.30-alpha. Локальный MCP control plane

Реализованы stdio MCP server, dual-era protocol negotiation, workflow list/describe, detached Run start, get/resume/answer/cancel, children, artifacts с содержимым и revision event polling. Реализация использует существующий runtime/store и не вводит daemon, HTTP, Web UI или БД. Полный архив среза: `44-local-mcp-control-plane-v0.1.30.md`.

## Выполнено в v0.1.31-alpha. Агентные события и внешний исполнитель

Реализованы provider-neutral базовые `assistant.*`/tool-call events, `Request.Emit`, durable `executor: external`, claim с capability attestation и lease/token, MCP tools pending/claim/event/complete/fail и возврат результата в обычные retry/hooks/output/artifact semantics. Одновременно устранён torn-read store при конкурентном polling и добавлен индекс событий. Полный архив среза: `45-agent-events-external-executor-v0.1.31.md`.

## Выполнено в v0.1.32-alpha. Управляемый agent lifecycle и глубокие workflow

Завершён event protocol v2: session lifecycle, tool request/allow/deny/start/complete, отдельная отмена вызова, artifact declaration с `call_id`, usage/diagnostic/terminal events и capability declaration adapter. Внешний executor обеспечивает блокирующий policy/approval до запуска инструмента и не может завершить узел с незакрытыми tool calls.

Шесть основных процессов профиля `code` получили строгие JSON-входы, специализированные предметные команды, обязательные checkpoint artifacts, domain error codes, Git decision trees, validation recovery и сквозной локальный Git/GitHub fixture. Полный архив среза: `46-controlled-agent-events-deep-workflows-v0.1.32.md`.

## Выполнено в v0.1.33-alpha. Строгий authoring и локальный daemon

Authoring preflight обнаруживает опечатки с `did you mean`, проверяет capabilities при `validate`, анализирует output/artifact references, выдаёт semantic diagnostics и поддерживает `--warnings-as-errors`. Renderer стал fail-closed с `${path}`, `${path?}` и `${path:-default}`. Добавлены расширенный schema subset, `always_run` и activity-based `idle_timeout`.

`takt daemon` использует Unix socket и существующий файловый Store для background Runs, event subscriptions, MCP proxy и нескольких локальных клиентов без БД. Полный архив среза: `47-authoring-local-daemon-v0.1.33.md`.

## Выполнено в v0.1.34-alpha. Dynamic Takt и coding-agent experience

Реализованы `existing|planned`, ограниченный `WorkflowPlan`, разрешённые блоки, компиляция в обычный Takt Workflow, preview/confirmation, бюджеты, checkpoint replanning, immutable revisions, steering, phase/run/artifact view, MCP/CLI `plan|execute|steer` и promotion успешного плана в workflow проекта. Основная сессия Pi/OpenCode остаётся интерфейсом пользователя, а фазы исполняются отдельными worker-сессиями. Полный архив среза: `48-dynamic-takt-v0.1.34.md`.

## Выполнено в v0.1.35-alpha. Доверенные пакеты блоков

Профиль подключает явный каталог `BlockPackage` со встроенными, корпоративными и проектными блоками. Пакет описывает workflow, типизированные выходы, возможности, интеграции, шаблоны, обязательные проверки, правила веток, шаблон запроса на изменение, security policy и максимальные бюджеты. Ограничения нескольких пакетов объединяются fail-closed, каталог получает fingerprint и становится обязательной границей plan/execute/replan/promote.

Одновременно исправлены foreground-исполнение `takt execute`, обход лимита редакций через steering, macOS artifact path, составные `when`, полный учёт planner/replanner в бюджетах, ограничение параллельных task-фаз, безопасное promote и межпроцессное продвижение планов. Полный архив среза: `49-trusted-block-packages-v0.1.35.md`.

## Приоритет 1. Domain Adapter SDK

- нейтральные capability contracts для SCM, tracker и CI;
- MCP/process transports;
- типизированные inputs/results/errors, идемпотентность и capability discovery;
- GitHub/GitLab как эталонные реализации без платформенных имён в workflow;
- fake corporate SCM/tracker/CI для контрактных тестов;
- использование адаптеров как детерминированных узлов Takt и как разрешённых инструментов worker-сессии.

## Приоритет 2. Полная доставка пакетов

- области `global|project|corporate` с явным приоритетом;
- `install|update|uninstall|doctor`;
- Git/local sources;
- version pin и lock-файл;
- зависимости и проверка совместимости;
- checksum, подпись и политика источников;
- команды, scripts, skills, MCP и adapter requirements в одном пакете.

## Приоритет 3. Multi-repo dynamic workflow

- определение затронутых репозиториев;
- dependency graph;
- отдельные worktree и child Run;
- несколько change request через нейтральный SCM adapter;
- интеграционная проверка и общий порядок слияния.

## Приоритет 4. Усиление runtime и безопасность локального исполнения

- раннее завершение `one_success` и `all_success` с отменой ненужных детей;
- нормализованные diagnostics и fingerprints ошибок;
- retry с backoff;
- защита секретов в state/events/artifacts;
- реальный OS sandbox для недоверенных процессов;
- path-based namespace для вложенных циклов.

## Предметная проверка

Отдельно от системных функций нужен Route DSL benchmark со штатным валидатором и обезличенными реальными заданиями: success@1, final success, число попыток, стоимость и стабильность на неизменных fingerprints.

## Отложенные proposals

Server, Web UI, БД, удалённые workers, message adapters, notifications и многопользовательская авторизация рассматриваются только после появления задачи нелокального использования и отдельной threat model.
