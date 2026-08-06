# Changelog

## v0.1.30-alpha

- Добавлена команда `takt mcp` — локальный stdio MCP control plane поверх существующих runtime и файлового store.
- Поддержаны legacy `initialize` для версий 2025 и stateless `server/discover` для `2026-07-28`.
- Опубликованы 10 детерминированно упорядоченных tools для discovery, start/get/resume/answer/cancel, child Runs, artifacts и events.
- `takt.run.start` выполняется detached по умолчанию и возвращает durable `run_id`; `takt.run.events` поддерживает revision cursor и bounded long polling.
- Tool results одновременно содержат text content, `structuredContent`, `resultType: complete` и `isError`.
- Artifact tool поддерживает recursive filters и ограниченное UTF-8/base64 содержимое с сохранением checksum/provenance.
- Добавлены общий local control service, чтение events из store, MCP cancellation request contexts, unit/lifecycle tests и `scripts/test-mcp.sh`.
- Roadmap локального MCP перенесён в выполненные работы и заархивирован в `docs/44-local-mcp-control-plane-v0.1.30.md`.
- Authoring skill обновлён до 0.12.0 и дополнен инструкцией локального MCP.

## v0.1.29-alpha

- Добавлены `script`-узлы с runtime `command`, `python`, `node` и `go`, file/inline source, args, env, working directory и зависимостями.
- Исходники скриптов и явно объявленные зависимости входят в workflow fingerprint и блокируют небезопасный resume.
- `command`, `prompt`, `bash` и `script` поддерживают `output_type`, `output_mime` и `output_path`.
- Артефакты сохраняются как локальные снимки с SHA-256, размером, producer Run/Node и номером попытки.
- Добавлены шаблоны `${nodes.<id>.artifacts.<type>.<field>}` и CLI `takt artifacts`.
- Governed child Run и fan-out передают ссылки на типизированные артефакты родителю без потери provenance.
- Профиль `code` 0.8.0 использует script-узел для review perspectives и регистрирует plan/PRD артефакты.
- Authoring skill обновлён до 0.11.0; добавлен контракт `scripts/test-script-artifacts.sh`.

## v0.1.28-alpha

- Добавлен динамический `workflow.fan_out` из структурированного output upstream-узла.
- Каждый элемент получает отдельный governed child Run, устойчивый ID, состояние, события, артефакты и usage.
- Реализованы `max_parallel`, `all_success|all_done|one_success`, ordered aggregation, частичный resume и fingerprints списка.
- Добавлены множественное ожидание approval, выборочная отмена ребёнка и CLI metadata в `takt children`.
- Smart/comprehensive review профиля `code` 0.7.0 переведены на runtime fan-out.
- `scripts/test-worktree.sh` совместим со штатным Bash 3.2 macOS.
- OpenCode `read_only` теперь всегда запрещает `write`; merge `OPENCODE_CONFIG_CONTENT` стал рекурсивным.
- Зафиксировано различие skills: Pi поддерживает существующие path skills, OpenCode — path и named skills.

## v0.1.27-alpha

- Добавлены per-node `allowed_tools`, `denied_tools`, `skills`, `mcp`, assistant-enforced `sandbox` и `requires`.
- Явные пустые allowlists сохраняются как запрет инструментов/skills.
- Добавлена capability preflight до запуска adapter, persistence effective policy и наследование governed child Run.
- Process protocol и `TAKT_POLICY_JSON` передают policy внешнему adapter.
- Pi и OpenCode применяют tool/skill policies; OpenCode получает MCP config и path-skill instructions.
- Исправлена работа managed worktree через symlinked paths на macOS.
- Usage structural composition включает hidden nodes.
- `takt cancel` отклоняет все terminal Run.
- Governed recursion проверяется уже командой `takt validate`.
- Пустые worktree-ветки удаляются, ветки с коммитами сохраняются.
- Профиль `code` 0.6.0 применяет no-tool policy к роутерам и read-only tool restrictions к review agents.
- Добавлен контракт `scripts/test-policies.sh`.

## v0.1.26-alpha

- добавлен узел `workflow`, запускающий подключённый workflow как отдельный governed child Run со своим ID, state, events, artifacts, output и usage;
- RunState хранит `parent_run_id`, `parent_node_id`, `child_run_ids`, aggregate usage и durable cancellation state; узел хранит текущего ребёнка и историю child attempts;
- approval внутри дочернего Run можно отвечать через корневой Run; CLI продолжает ребёнка и каскадно возобновляет всю parent chain;
- добавлены `takt children` и `takt cancel`, включая durable marker, остановку активного процесса и каскадную отмену дерева;
- реализованы режимы изоляции ребёнка `inherit`, `worktree`, `none` и собственная policy дочернего workflow;
- retry узла `workflow` создаёт новый дочерний Run, сохраняя предыдущие попытки для аудита;
- fingerprint родителя рекурсивно включает definitions governed children; рекурсия и глубина 16 проверяются до запуска;
- умный router и reusable review-блоки профиля `code` переведены на отдельные child Runs;
- профиль `code` обновлён до 0.5.0, authoring skill — до 0.8.0;
- добавлены ADR-038, governed child run contract suite и `docs/40-governed-child-runs-v0.1.26.md`.

## v0.1.25-alpha

- добавлена управляемая Git worktree isolation: политика workflow, CLI-переопределения, отдельная ветка/каталог выполнения, сохранение состояния и безопасная очистка;
- умный роутер создаёт worktree только после выбора изменяющего дочернего workflow; direct selector применяет ту же политику при старте Run;
- добавлены `takt worktree list/remove/prune`, блокировка удаления активного Run и защита грязного worktree без `--force`;
- определения и команды fingerprint-ятся из control checkout, а CLI-решение по изоляции сохраняется для resume;
- исправлено сохранение raw stdout при `output_format`, добавлен protocol retry с точным `${feedback}`, устранён двойной retry роутера;
- исправлены approved-итерация `interactive-prd`, fallback `create-issue`, публикация параллельного статуса и exact integer validation;
- полный review block использует настоящий `foreach.parallel` по пяти перспективам;
- профиль `code` обновлён до 0.4.0, authoring skill — до 0.7.0;
- добавлены ADR-037, worktree contract suite и `docs/39-git-worktree-isolation-v0.1.25.md`.

## v0.1.24-alpha

- профиль `code` 0.3.0 получил полный каталог из 19 процессов разработки и умный роутер в обычном Run;
- Profile поддерживает именованные `workflows`, селектор `profile:workflow`, `takt workflow list` и `takt workflow describe`;
- `command` и `prompt` поддерживают проверяемый `output_format`, а шаблоны и `when` — вложенные JSON-пути;
- scheduler выполняет независимые простые узлы параллельными волнами с сериализованным persistence и моделью all-settled;
- `foreach.parallel` выполняет итерации конкурентно и сохраняет порядок входного массива в агрегированном output;
- approval разрешён внутри `loop_group`; resume продолжает активную итерацию, а следующая итерация создаёт новый approval;
- добавлено `trigger_rule: one_success` для соединения условных ветвей;
- добавлены reusable full/smart review blocks, тест роутера, race/timing regressions и контракт всех 19 workflow;
- authoring skill обновлён до 0.6.0;
- добавлены ADR-035, ADR-036 и `docs/38-archon-workflow-catalog-v0.1.24.md`.

## v0.1.23-alpha

- `foreach` поддерживает внешний YAML/JSON-массив через `items_from.path`; содержимое входит в workflow fingerprint;
- `subworkflow` и `foreach` разрешены внутри `loop_group` без второго scheduler;
- output `foreach` стал упорядоченным JSON-массивом результатов всех итераций;
- CLI возвращает публичное состояние Run без внутренних развёрнутых ID и принимает approval по ID контейнера;
- контейнер поддерживает defaults `assistant`, `model` и `session`;
- локальные команды подключённого workflow ищутся до корня композиции, включая корневой `commands/` профиля;
- схема согласована с value-семантикой контейнера, задокументированы рекурсия и предел глубины 16;
- усилены timeout/overflow-регрессии, устранена гонка закрытия stderr в Pi adapter, расширены тесты композиции и исправлена проверка документации;
- профиль `code` обновлён до 0.2.1, authoring skill — до 0.5.0;
- добавлены ADR-034 и `docs/37-composition-hardening-v0.1.23.md`.

## v0.1.22-alpha

- добавлены reusable `subworkflow` с inputs, автоматическим или явным `output_node` и публичным output;
- добавлен последовательный `foreach` по явным scalar/JSON-object items;
- композиция компилируется в обычный DAG с единым scheduler и сохраняемыми approval/retry/status;
- подключённые workflow и локальные команды входят в workflow fingerprint;
- профиль `code` 0.2.0 разделён на переиспользуемые фазы implementation и review;
- authoring skill обновлён до 0.4.0;
- добавлены composition example, contract suite, ADR-033 и `docs/36-workflow-composition-v0.1.22.md`.

## v0.1.20-alpha

- OpenCode сохраняет сообщения о provider retry и connection failure при timeout/cancellation;
- raw stdout/stderr и краткая диагностика попадают в NodeState и per-attempt execution record без изменения execution kind;
- scheduler сохраняет специализированную context-ошибку adapter вместо общего `node attempt`;
- authoring skill обновлён до v0.2.1, а его README и поддерживаемая версия Takt проверяются автоматически;
- добавлены ADR-031 и `docs/34-opencode-provider-diagnostics-v0.1.20.md`.

## v0.1.19-alpha

- добавлен специализированный `type: opencode` через `opencode run --format json`;
- поддержаны model/agent/variant, fresh/resume, version probe, per-step usage/cost и error events;
- добавлены fake OpenCode binary, contract suite, runtime retry/resume test и opt-in real smoke;
- OpenCode включён в config schema, примеры и Takt authoring skill v0.2.0;
- добавлены ADR-030 и `docs/33-opencode-adapter-v0.1.19.md`.

## v0.1.18-alpha

- добавлен canonical skill `skills/takt/SKILL.md` для создания, изменения, проверки и запуска Takt-профилей;
- справка скилла разделена на configuration, workflows, patterns и troubleshooting;
- добавлен копируемый `validated-agent-profile` с inline prompt, моделями на узлах, Markdown-командой, validator retry/resume и approval;
- добавлен контрактный `scripts/test-takt-skill.sh`, который проверяет структуру скилла и валидирует оба шаблонных workflow;
- прежний `examples/coding-agent-skill` переименован по назначению в минимальный `takt-runner`;
- добавлен документ `docs/32-takt-authoring-skill-v0.1.18.md`.

## v0.1.17-alpha

- добавлен корневой `AGENTS.md` с краткими правилами работы кодовых агентов;
- bash runtime сохраняет stdout и stderr отдельно, сохраняя объединённый `output` для feedback и диагностики;
- evaluation декодирует `takt-validation/v1alpha1` только из stdout quality-node;
- stderr валидатора больше не повреждает корректный validation envelope и сохраняется в отчёте отдельно;
- добавлены регрессии `valid:false + stderr + exit 1`, схема состояния и схема evaluation report;
- добавлены ADR-029 и документ `docs/31-quality-stdout-separation-v0.1.17.md`.

## v0.1.16-alpha

- quality envelope декодируется и сохраняется независимо от exit code и terminal status quality-node;
- `score`, `checks` и diagnostics из `valid: false` с ненулевым exit code участвуют в предметных агрегатах;
- успех benchmark определяется только сочетанием `quality_node_status: completed` и `quality.valid: true`;
- `valid: true` из failed/errored/timed_out/cancelled узла сохраняется для аудита, но не повышает success rate;
- malformed validation envelope при любом статусе остаётся ошибкой измерительного контура;
- evaluation report сохраняет `quality_node_status`;
- добавлены ADR-028 и документ `docs/30-quality-envelope-semantics-v0.1.16.md`.

## v0.1.15-alpha

- quality summary сохраняет измеренные нули как `0`, а недоступные средние значения как `null`;
- `NodeState.executions` сохраняет assistant/version/requested/resolved model и usage каждой фактической попытки;
- evaluation report помечает mixed execution identity и группирует tokens/cost по отдельным identity;
- JSON с `valid: true` учитывается только от quality-node со статусом `completed`;
- benchmark fingerprint включает ID и объявленную версию валидатора;
- `duration_per_valid_ms` заменён на точное по смыслу `amortized_end_to_end_ms_per_valid`;
- opt-in Pi smoke проверяет наличие фактического `ResolvedModel`;
- добавлены ADR-027 и отчёт `docs/29-benchmark-metric-semantics-v0.1.15.md`.

## v0.1.14-alpha

- evaluation report получил формат `takt-evaluation/v1alpha1`;
- добавлены `strategy_id` и fingerprints workflow/config/Markdown-команд;
- добавлены benchmark/dataset/workspace/validator fingerprints и версия валидатора;
- `NodeState` и report сохраняют assistant, его версию, requested model и фактический Pi `responseModel`;
- добавлен строгий предметно-независимый контракт `takt-validation/v1alpha1`;
- summary рассчитывает success@1, final success, average score, attempts/cost/time per valid и diagnostics по severity/code;
- `examples/route-dsl-eval` закреплён как инфраструктурный suite, добавлен отдельный `examples/route-dsl-benchmark` для реального Pi и штатного валидатора;
- добавлены схемы validation result/evaluation report, ADR-026 и отчёт `docs/28-benchmark-identity-quality-v0.1.14.md`.

## v0.1.13-alpha

- evaluation preflight отклоняет коллизии нормализованных `case_id` до создания output;
- `workspace-template` и `output` не могут совпадать или быть вложены друг в друга, включая разрешение символических ссылок;
- `NodeState` сохраняет подтверждённый факт resume;
- `report.json` содержит `resumed`, `feedback`, ошибку и диагностический вывод каждого узла;
- Route DSL eval suite проверяет retry/resume и сохранение validator diagnostics;
- добавлены ADR-025 и отчёт `docs/27-evaluation-isolation-report-v0.1.13.md`.

## v0.1.12-alpha

- Route DSL E2E больше не зависит от команды `python`; JSON-проверки выполняются Go helper-ом;
- интеграционные timeout/cancel + overflow тесты используют корректные `context.WithTimeout` и `context.WithCancel`;
- runtime scheduler проверяет сохранение `output_truncated` в итоговом `NodeState`;
- `NodeState.usage` накапливает tokens и cost всех агентных попыток;
- добавлены `takt eval run` и `takt eval report` для изолированного прогона каталогов заданий;
- evaluation report содержит статусы, attempts, duration, usage, approvals и truncation;
- добавлен стартовый набор из десяти Route DSL заданий и контрактный eval-тест;
- добавлен отчёт `docs/26-evaluation-runner-v0.1.12.md`.

## v0.1.11-alpha

- добавлены fake-Pi overflow-сценарии, проходящие через реальный `Pi.Run`;
- интеграционно проверены `timed_out`/`cancelled` и `Result.Truncated=true`;
- runtime-регрессия подтверждает сохранение `NodeState.OutputTruncated` для context errors;
- добавлен воспроизводимый Route DSL end-to-end: Pi → validator → feedback → retry/resume → artifacts → approval;
- первая попытка намеренно не проходит проверку, вторая использует Session ID и диагностику;
- новый сквозной сценарий включён в `make check` и `scripts/verify.sh`;
- добавлены ADR-023 и отчёт `docs/25-route-dsl-e2e-v0.1.11.md`.

## v0.1.10-alpha

- timeout/cancellation Pi attempt имеют приоритет над одновременно обнаруженным output overflow;
- `output_truncated` сохраняется как диагностика без изменения `timed_out`/`cancelled`;
- исчезновение cumulative usage после его наличия в первом снимке классифицируется как `protocol`;
- явные нулевые usage-значения остаются валидными;
- добавлены регрессии timeout+overflow, cancel+overflow, missing usage и zero usage;
- добавлены ADR-022 и отчёт `docs/24-pi-context-usage-hardening-v0.1.10.md`.

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

## v0.1.21-alpha

- Добавлены пакеты профилей и команды `takt init <profile>`, `takt validate <profile>`, `takt run <profile>`.
- Добавлен встроенный профиль `code` для реализации Markdown-плана без обязательного task AST.
- Добавлена схема `schemas/profile.schema.json` и контрактный тест `scripts/test-code-profile.sh`.
- Authoring skill обновлён до 0.3.0 и обучен использовать готовые профили.
