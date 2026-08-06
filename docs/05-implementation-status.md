# Состояние реализации

## Реализовано

### Форматы и загрузка

- workflow/config/profile в YAML и JSON со строгим decode неизвестных полей и path-aware `did you mean`;
- документированный YAML subset с block scalar;
- JSON Schemas для config, workflow, profile, state, events, Markdown-команд и assistant protocol;
- проверка ссылок, ID, циклов DAG, рекурсии structural и governed workflow и глубины композиции 16;
- именованный каталог workflow в профиле и селектор `profile:workflow`;
- `takt workflow list` и `takt workflow describe`;
- расширенный проверяемый JSON `output_format` для `command`, `prompt` и `script`;
- authoring diagnostics: output/artifact references, dependency direction, suspicious combinations и `--warnings-as-errors`;
- capability preflight выполняется уже в `takt validate`, включая governed child definitions.

### Runtime

- DAG с `depends_on`, `when`, JSON-путями и правилами `all_success`, `all_done`, `none_failed_min_one_success`, `one_success`;
- реальные параллельные волны независимых простых `command`, `prompt`, `bash`, `script`;
- одна scheduler-семантика root DAG, `loop_group`, скомпилированных `subworkflow` и `foreach`;
- `command`, `prompt`, `bash`, `script`, `approval`, `loop_group`, `subworkflow`, последовательный и параллельный `foreach`;
- approval внутри `loop_group` с pause/resume той же итерации;
- inline `foreach.items` и внешний `items_from.path` с fingerprint исходного файла;
- детерминированный массив результатов `foreach` в порядке входа независимо от порядка завершения;
- публичная проекция Run без внутренних `__`-ID;
- defaults `assistant`, `model`, `session` на structural container;
- portable hooks, retry с feedback, timeout/cancellation, activity-based `idle_timeout`, `allow_failure` и cleanup `always_run`;
- fingerprints workflow/config/commands/subworkflow/items source;
- файловые state/events/artifacts, revision consistency и блокировка Run;
- script runtime `command|python|node|go|validation` с file/inline source, args, env, working directory, timeout/cancellation и structured output;
- fingerprint исходника script и явно объявленных dependencies;
- типизированные `output_type`/`output_mime`/`output_path` артефакты с SHA-256 и producer metadata;
- строгий renderer `${path}`/`${path?}`/`${path:-default}` и ссылки `${nodes.<id>.artifacts.<type>.<field>}`;
- CLI `takt artifacts` и передача артефактов parent/child/fan-out;
- управляемая Git worktree isolation с отдельной веткой, control/execution workspace, safe cleanup и resume;
- CLI `worktree list/remove/prune` и persisted Run overrides.

### Локальный MCP, daemon и управляемый worker lifecycle

- прямой `takt mcp` и `takt mcp --daemon` через локальный Unix socket;
- `takt daemon start|status|stop|serve` без БД: background Runs, event subscriptions, несколько клиентов, recovery потерянных локальных executor и notification dispatch;
- legacy initialization `2025-03-26|2025-06-18|2025-11-25` и stateless discovery `2026-07-28`;
- 54 internal operations разделены на surfaces; основная LLM видит пять `takt.task.*`, host и external worker получают только собственные протоколы;
- detached start с durable `run_id`;
- indexed revision cursor и bounded long polling событий без полного пересканирования журнала;
- structured/text tool results, bounded artifact content, request cancellation и strict arguments;
- MCP и daemon используют существующие fingerprints, locks, store и parent/child lifecycle;
- concurrent control mutations сериализуются bounded retry, а daemon shutdown ожидает monitor goroutines;
- claimed external `idle_timeout` обслуживается daemon и закрывает зависшие tool calls как cancelled перед `timed_out`.
- `executor: external` передаёт один command/prompt узел внешнему worker через durable claim/lease/token, capability preflight, normalized events и обычные retry/hooks/output/artifact semantics.
- event protocol v2 сохраняет `assistant.session.started|session.resumed|message|tool.requested|tool.allowed|tool.denied|tool.started|tool.completed|artifact.declared|usage|diagnostic|completed|failed`;
- capability declaration различает наблюдательные events и настоящий `tool_control`; OpenCode/Pi не заявляют pre-execution interception;
- внешний executor применяет node policy до запуска инструмента, поддерживает blocking approval, отдельную отмену и запрещает terminal-result при незавершённых tool calls;
- process protocol `takt-assistant/v1alpha2` поддерживает двунаправленный tool decision channel.

### Governed child Runs

- узел `workflow` запускает подключённое определение как отдельный Run, а не разворачивает его в DAG родителя;
- отдельные Run ID, state, events, artifacts, fingerprints, output и usage;
- `parent_run_id`, `parent_node_id`, direct `child_run_ids` и current/history links на узле;
- передача input и публичного output через `output_node` либо единственный terminal-узел;
- failure/cancellation ребёнка определяют результат родительского узла;
- approval ребёнка приостанавливает родителя; `takt answer` через корневой Run продолжает ребёнка и всю parent chain;
- durable cancellation marker останавливает активный узел, а `takt cancel` каскадно отменяет дерево;
- `takt children` показывает прямых детей, `takt status` работает для любого ребёнка;
- режимы `isolation: inherit|worktree|none` и собственная worktree policy ребёнка;
- retry родительского `workflow`-узла создаёт новый child Run, сохраняя прошлые child attempts;
- рекурсивный fingerprint статически подключённых детей, запрет рекурсии и предел глубины 16;
- contract suite `scripts/test-child-runs.sh`;
- динамический fan-out из JSON-массива upstream-узла: устойчивые Run ID, `max_parallel`, resume, ordered aggregation, `all_success|all_done|one_success`, выборочная и каскадная отмена;
- contract suite `scripts/test-child-fanout.sh`.


### Dynamic Takt и доверенные блоки

- решение `existing|planned` по естественной цели;
- ограниченный `WorkflowPlan` с budgets, `task|map`, dependencies и checkpoint;
- явно подключённые `BlockPackage` со scope `builtin|corporate|project`;
- типизированные выходы блоков, capabilities, integrations, templates и governance;
- обязательные блоки/проверки, branch/change-request rules, security policy и сужаемые бюджеты;
- fingerprint каталога, блокирующий execute/replan/promote после изменения;
- встроенный каталог `discover|investigate|implement|validate|review|adversarial-verify|synthesize`;
- компиляция каждого сегмента в обычный Takt Workflow без второго runtime;
- preview и обязательное подтверждение planned-плана;
- hard cap child Run/fan-out, parallelism, revisions и token usage на границах фаз;
- checkpoint replanning с immutable revisions незавершённого хвоста;
- `takt plan|execute|steer`, phase/run/artifact view и promotion в `.takt/workflows/generated`;
- MCP `takt.plan`, `takt.plan.get`, `takt.execute`, `takt.run.steer`, `takt.plan.promote`;
- логический `coding-agent`: конкретный Pi/OpenCode/process adapter выбирается через `default_assistant`; отдельные worker-сессии исполняют фазы.
- внутренний `EvidenceManifest` сохраняет baseline, check evidence и verdict, связанный с candidate content SHA-256;
- известные baseline failures не считаются новой regression и не запускают automatic repair;
- `parked` хранит failure code, owner и безопасное продолжение и попадает в attention;
- external `side_effect.mode: reconcile` блокирует повтор внешней мутации при неизвестном исходе до reconciliation + receipt.


### Simple Reliable Task Router

- высокоуровневый `takt task start|status|respond|stop|explain`;
- route `workflow|template|dynamic` с JSON-схемой и проверкой ссылок;
- `simple-reliable`: investigate → implement → validate → review;
- прогрессивные controls baseline, independent tests, enhanced review, inspect checkpoint и max_parallel;
- детерминированные risk signals могут только усилить controls;
- invalid/unavailable semantic router даёт durable inspect-first fallback;
- MCP surfaces `agent|host|worker|operator|all`;
- `SessionAdapter` и `takt-assistant/v1alpha2` как нейтральная граница Codex/Oh My Pi/Qwen CLI и других wrappers.

### Coding Agent Host Control и автономные Run

- durable host sessions с begin/confirm/find/get/release и межпроцессной сериализацией;
- default-deny tool guard с точным allowlist и игнорированием клиентского `read_only`;
- `strict` доступен только адаптеру с command/input/tool/completion/recovery capabilities; bundled Pi/OpenCode integrations честно заявляют `guarded`;
- fail-closed local cache bundled extensions при потере daemon, перехват steering и пользовательского shell;
- реестр Run и attention queue с причинами approval/question/tool approval/failure/paused;
- безопасная пауза на границе узла и fan-out batch, каскадный marker для governed child Runs и resume остатка;
- operator retry failed-узла и зависимого хвоста, fork и отдельное состояние `abandoned`;
- PID-based recovery после daemon restart с событием `worker_lost`, recovery count и продолжением parent chain;
- агрегированный `run summary` с descendants, usage, artifacts, retry/recovery history;
- durable notification inbox, desktop/process sinks, дедупликация и ack;
- Pi/OpenCode команды runs, attention, pause, resume и result.

### Профиль code 0.15.0

- встроенный пакет `code-core` с девятью атомарными блоками Dynamic Takt, включая baseline и independent test-design;
- внутренние роли `baseline-observer|investigator|implementer|test-designer|validator|verifier` и fresh bounded `TaskBrief` для trusted phases;
- structured required/preferred checks, реакции `deny|repair|warn` и одна bounded automatic repair-итерация для recoverable technical failures;

- 19 процессов разработки: assist, issue/PR flows, PIV, Ralph, idea/plan-to-PR, reviews, architecture, safe refactoring, PRD, workflow builder, Remotion и conflict resolution;
- умный роутер как корневой Run с отдельным governed child Run выбранного процесса;
- структурированный выбор маршрута с enum всех 19 процессов;
- smart review динамически выбирает перспективы, а comprehensive review запускает пять governed child Runs через `workflow.fan_out`;
- review perspectives формируются script-узлом; PIV, idea-to-PR и interactive PRD публикуют типизированные plan/PRD артефакты;
- интерактивные PIV и PRD-циклы;
- reusable `review-block` и `smart-review-block` как отдельные child Runs с `isolation: inherit`;
- отдельный запуск любого процесса через `code:<workflow>`;
- шесть основных процессов (`fix-github-issue`, `idea-to-pr`, `plan-to-pr`, `smart-pr-review`, `piv-loop`, `ralph-dag`) принимают строгие JSON-входы;
- специализированные Git/issue/plan/review/PIV/Ralph/validation/recovery/PR-команды вместо универсальных каркасов;
- обязательные типизированные checkpoint artifacts и ограниченные domain error codes;
- явные Git decision trees и validation → recovery → revalidation;
- сквозной contract на настоящем локальном Git repository, bare remote и fake `gh`.

### Assistants, protocol и evaluation

- `mock`, `process`, `pi`, `opencode`;
- per-node `allowed_tools`, `denied_tools`, explicit empty allowlists, skills, MCP, assistant-enforced filesystem/network policy and `requires`;
- capability preflight before assistant invocation, persisted effective policy and child-Run inheritance;
- Pi tool/skill CLI mapping, OpenCode permission/MCP mapping and path-skill prompt injection;
- строгий `takt-assistant/v1alpha1`, fake contract suites, verified resume и usage;
- Pi RPC и OpenCode JSON event stream с сохранением provider diagnostics;
- Route DSL end-to-end и evaluation runner с изоляцией, fingerprints и quality envelopes;
- отдельные execution records и атрибуция usage.

### CLI

- `init`, `validate`, `run`, `answer`, `resume`, `status`, `children`, `cancel`, `events`, `command run`;
- `runs`, `attention`, `run list|summary|watch|pause|resume|retry|fork|abandon|recover`;
- `notify list|ack|test|dispatch` и `host begin|confirm|status|find|guard-tool|guard-completion|release`;
- `validate --warnings-as-errors`; `run --daemon`; `mcp --daemon`; `daemon start|status|stop|serve`;
- `worktree list`, `worktree remove`, `worktree prune`;
- `workflow list`, `workflow describe`;
- `artifacts` с фильтрацией по узлу/типу и рекурсивным обходом child Runs;
- единый JSON success/error envelope.

## Осознанно ограничено

- обычная DAG-волна не исполняет одиночные `workflow`-узлы конкурентно; конкурентность governed children задаётся через `workflow.fan_out.max_parallel`;
- `one_success` ожидает завершения всей группы и пока не отменяет остальные элементы досрочно;
- `attempts`, `timeout`, hooks, `native_hooks` и `allow_failure` structural-группы задаются внутри дочернего workflow;
- вложенные `loop_group` запрещены до path-based namespace;
- `items_from` является статическим compile-time источником, а не output предыдущего узла;
- язык условий и `output_format` остаются проверяемым subset, а не полной JSON Schema;
- process protocol v1alpha1 не передаёт потоковые tool events; v1alpha2 передаёт;
- native hooks передаются adapter, но не исполняются ядром.

## Отличия от полной платформы Archon

Все 19 пользовательских процессов, роутер, managed worktree, governed child Run lifecycle и локальный daemon реализованы. На уровне инфраструктуры отсутствуют:

- удалённый server/Web UI, БД, внешние message adapters и многопользовательская авторизация — proposal для будущего нелокального режима. Локальные уведомления уже реализованы через inbox/desktop/process sinks.

Tool/skills/MCP policy теперь является контрактом ядра и adapters. Filesystem/network policy остаётся assistant-enforced и не заменяет OS sandbox.

## Текущая граница безопасности

Текущая версия — локальный однопользовательский trusted runtime. Daemon расширяет время жизни и число локальных клиентов, но не меняет trust boundary. Workflow, config, Markdown-команды, shell, assistants и workspace считаются доверенными. Separate child Run и Git worktree являются границами lifecycle/изменений, но не sandbox. Server/untrusted scope требует sandbox, path/network policy, secret redaction, авторизацию и отдельную threat model.

## Ближайший целевой срез

Dynamic Takt, доверенные блоки, host-control core, автономная эксплуатация Run, Simple Reliable Task Router, Role Contract, bounded TaskBrief, реакции `deny|repair|warn`, EvidenceManifest, baseline classification, parking и external side-effect reconciliation реализованы к `v0.1.40-alpha`. Bundled Pi/OpenCode остаются guarded до live contract tests. Следующий крупный продуктовый приоритет — нейтральный SDK доменных адаптеров SCM/tracker/CI; затем полная доставка пакетов, multi-repo orchestration, runtime/security hardening и предметный Route DSL benchmark.
