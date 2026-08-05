# Состояние реализации

## Реализовано

### Форматы и загрузка

- workflow/config/profile в YAML и JSON со строгим decode неизвестных полей;
- документированный YAML subset с block scalar;
- JSON Schemas для config, workflow, profile, state, events, Markdown-команд и assistant protocol;
- проверка ссылок, ID, циклов DAG, рекурсии structural и governed workflow и глубины композиции 16;
- именованный каталог workflow в профиле и селектор `profile:workflow`;
- `takt workflow list` и `takt workflow describe`;
- проверяемый JSON `output_format` для `command` и `prompt`.

### Runtime

- DAG с `depends_on`, `when`, JSON-путями и правилами `all_success`, `all_done`, `none_failed_min_one_success`, `one_success`;
- реальные параллельные волны независимых простых `command`, `prompt`, `bash`;
- одна scheduler-семантика root DAG, `loop_group`, скомпилированных `subworkflow` и `foreach`;
- `command`, `prompt`, `bash`, `approval`, `loop_group`, `subworkflow`, последовательный и параллельный `foreach`;
- approval внутри `loop_group` с pause/resume той же итерации;
- inline `foreach.items` и внешний `items_from.path` с fingerprint исходного файла;
- детерминированный массив результатов `foreach` в порядке входа независимо от порядка завершения;
- публичная проекция Run без внутренних `__`-ID;
- defaults `assistant`, `model`, `session` на structural container;
- portable hooks, retry с feedback, timeout/cancellation, `allow_failure`;
- fingerprints workflow/config/commands/subworkflow/items source;
- файловые state/events/artifacts, revision consistency и блокировка Run;
- управляемая Git worktree isolation с отдельной веткой, control/execution workspace, safe cleanup и resume;
- CLI `worktree list/remove/prune` и persisted Run overrides.

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
- contract suite `scripts/test-child-runs.sh`.

### Профиль code 0.5.0

- 19 процессов разработки: assist, issue/PR flows, PIV, Ralph, idea/plan-to-PR, reviews, architecture, safe refactoring, PRD, workflow builder, Remotion и conflict resolution;
- умный роутер как корневой Run с отдельным governed child Run выбранного процесса;
- структурированный выбор маршрута с enum всех 19 процессов;
- параллельные многоаспектные PR-review ветви, включая `foreach.parallel` по пяти перспективам;
- интерактивные PIV и PRD-циклы;
- reusable `review-block` и `smart-review-block` как отдельные child Runs с `isolation: inherit`;
- отдельный запуск любого процесса через `code:<workflow>`.

### Assistants, protocol и evaluation

- `mock`, `process`, `pi`, `opencode`;
- строгий `takt-assistant/v1alpha1`, fake contract suites, verified resume и usage;
- Pi RPC и OpenCode JSON event stream с сохранением provider diagnostics;
- Route DSL end-to-end и evaluation runner с изоляцией, fingerprints и quality envelopes;
- отдельные execution records и атрибуция usage.

### CLI

- `init`, `validate`, `run`, `answer`, `resume`, `status`, `children`, `cancel`, `command run`;
- `worktree list`, `worktree remove`, `worktree prune`;
- `workflow list`, `workflow describe`;
- единый JSON success/error envelope.

## Осознанно ограничено

- параллельная волна пока не включает `workflow`-узлы, узлы с portable hooks или `attempts.max > 1`;
- динамический fan-out child Runs из output предыдущего узла отсутствует;
- нет отдельного лимита конкурентных детей и fan-out join policy;
- `attempts`, `timeout`, hooks, `native_hooks` и `allow_failure` structural-группы задаются внутри дочернего workflow;
- вложенные `loop_group` запрещены до path-based namespace;
- `items_from` является статическим compile-time источником, а не output предыдущего узла;
- язык условий и `output_format` — намеренно небольшой проверяемый subset;
- process protocol не передаёт потоковые tool events;
- native hooks передаются adapter, но не исполняются ядром.

## Отличия от полной платформы Archon

Все 19 пользовательских процессов, роутер, managed worktree и governed child Run lifecycle перенесены. На уровне инфраструктуры пока отсутствуют:

- per-node tool allow/deny, skills, MCP и sandbox policy;
- script nodes и semantic artifact `output_type`;
- динамический fan-out и параллельные governed children;
- server/Web UI, БД, message adapters, notifications и проверка подключённой GitHub identity — proposal для будущего нелокального режима.

Пока tool/sandbox guarantees выражаются командами workflow и возможностями внешнего coding agent, а не отдельными механизмами ядра.

## Текущая граница безопасности

Текущая версия — локальный однопользовательский trusted runtime. Workflow, config, Markdown-команды, shell, assistants и workspace считаются доверенными. Separate child Run и Git worktree являются границами lifecycle/изменений, но не sandbox. Server/untrusted scope требует sandbox, path/network policy, secret redaction, авторизацию и отдельную threat model.

## Ближайший целевой срез

Следующий крупный системный приоритет — per-node capability and policy contract: `allowed_tools`, `denied_tools`, skills, MCP, sandbox и проверка поддержки adapter до запуска. Затем — динамический fan-out governed children, script nodes и типизированные артефакты. Предметная задача остаётся прежней: запустить Route DSL benchmark со штатным валидатором и реальными обезличенными заданиями на неизменных fingerprints.
