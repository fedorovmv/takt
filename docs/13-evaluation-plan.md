# План оценки агентных стратегий

Статус: в `v0.1.52-alpha` workflow-level контур поддерживает `takt eval run/report/benchmark/compare`, а task-level — `takt eval task-benchmark`. Fake contract benchmarks отделены от live Route DSL/Go/Document evidence со штатными validators и реальными моделями.

## 1. Цель

Takt должен позволять сравнивать стратегии выполнения, а не только запускать workflow. Для доказательного сравнения нужны неизменный набор задач, предметный валидатор и полная идентичность эксперимента.

## 2. Стратегии

Минимальный набор конфигураций:

1. один агент, fresh context;
2. один агент, resume context;
3. анализ → реализация;
4. реализация → LLM review;
5. реализация → детерминированная проверка;
6. реализация → две проверки → исправление.

Читаемый `strategy_id` задаётся явно. Содержательную идентичность определяет fingerprint workflow, config и всех используемых Markdown-команд.

## 3. Наборы задач

### Route DSL

- простой HTTP-маршрут;
- ветвление;
- обработка ошибок;
- тяжёлый jq;
- Bloblang;
- неполное требование;
- незнакомая комбинация элементов;
- отрицательный тест на несуществующую возможность.

### Go

- локальная ошибка;
- конкурентная ошибка;
- изменение API с тестами;
- исправление по failing test;
- задача с необходимостью изучить несколько файлов.

### Документы

- краткий проектный документ;
- переработка по комментарию approval;
- сравнение вариантов;
- документ со структурированным результатом.

## 4. Идентичность benchmark

`report.json` фиксирует:

- версию Takt и формат `takt-evaluation/v1alpha1`;
- `strategy_id` и fingerprints workflow/config/commands;
- `benchmark_id`, fingerprints упорядоченного набора заданий и копируемого workspace template, число cases;
- quality/generation nodes;
- ID, версию и fingerprint валидатора;
- assistant, его версия, requested provider/model/params и фактический `responseModel` каждой попытки;
- GOOS, GOARCH и версию Go.

Результаты разных fingerprints считаются разными экспериментами даже при совпадающем читаемом имени.

## 5. Предметный результат качества

Узел `--quality-node` возвращает один объект `takt-validation/v1alpha1`:

```json
{
  "protocol_version": "takt-validation/v1alpha1",
  "type": "validation_result",
  "valid": true,
  "score": 94,
  "checks": {
    "syntax": {"passed": true, "score": 100, "weight": 1},
    "semantics": {"passed": true, "score": 88, "weight": 4}
  },
  "diagnostics": []
}
```

Состав checks определяет предметный валидатор. Takt проверяет общий контракт и хранит результат без знания Route DSL. Envelope декодируется при любом terminal status; score и diagnostics сохраняются. Корректным результатом считается только `quality-node completed && valid=true`.

## 6. Метрики

Для каждого Run:

- success/failure и предметный `valid`;
- score и checks;
- число попыток до корректного результата;
- assistant/version, requested/resolved model по каждой фактической попытке;
- Session ID и resume;
- длительность;
- input/output tokens и стоимость;
- diagnostics, feedback и ошибки;
- число approval answers;
- количество approval answers как наблюдаемый proxy вмешательств пользователя;

Агрегаты:

- `success_at_1`;
- `final_success_rate`;
- `average_attempts_to_valid`;
- `average_score`;
- `cost_per_valid`;
- `amortized_end_to_end_ms_per_valid`;
- настоящий `average_time_to_valid_ms`;
- `retry_scheduled`, failed executions и их стоимость;
- diagnostics по severity/code/fingerprint;
- stable-valid / stable-invalid / unstable cases при repeat;
- распределение assistant/requested/resolved model;
- `usage_by_execution_identity` и число mixed-узлов.

Стоимость и амортизированная end-to-end длительность включают неуспешные запуски. `time_to_valid_ms` измеряет момент durable завершения корректного quality-node внутри конкретного Run и является отдельной метрикой.

Run с durable `error_code`/diagnostic kind `provider_unavailable` является
`outcome: infrastructure_error`: usage, duration, provider attempts и
diagnostics остаются в отчёте, но он не входит в `evaluated_runs`, quality
denominators (`quality_runs`, valid/invalid, scores, stability, time-to-valid)
и не получает true/false accept/reject. Suite продолжает следующий case;
`flow_completion_rate` по-прежнему использует все scheduled cases.

Измеренный ноль сериализуется как `0`; недоступный показатель — как `null`.

## 7. Правила сравнения

- EvaluationMatrix запускает все стратегии на одном corpus/repeat;
- baseline_strategy задаётся явно;
- сравнение выполняется попарно по case_id + repeat;
- изменения prompts, config или commands создают новый fingerprint;
- набор заданий, workspace template и валидатор должны иметь одинаковые fingerprints;
- успех определяется внешним критерием, а не сообщением агента;
- минимум три повтора на задачу для стохастических моделей;
- resolved model проверяется отдельно от запрошенной модели, а версия assistant — отдельно от config fingerprint;
- usage сравнивается по execution identity, а mixed-узлы анализируются отдельно;
- infrastructure contract suite не смешивается с quality benchmark.

## 8. Запуск

```bash
takt eval run <workflow> \
  --config <config> \
  --cases <cases-dir> \
  --workspace-template <template-dir> \
  --output <output-dir> \
  --strategy-id <strategy-id> \
  --benchmark-id <benchmark-id> \
  --quality-node <validator-node> \
  --generation-node <generator-node> \
  --validator-id <validator-id> \
  --validator-version <version> \
  --validator-path <file-or-dir> \
  --repeat 3 \
  --answer approved \
  --replace \
  --json
```

`examples/route-dsl-eval` проверяет инфраструктуру с fake adapter. `examples/route-dsl-benchmark` содержит agent-neutral matrix на 25 regression/production-shaped synthetic cases; live запуск использует configured coding-agent и штатный валидатор.

## Production flow evaluation

`takt eval flow` evaluates cases sequentially: every case/repeat receives a fresh
control workspace and produces `cases/<case>/repeat-<NNN>/` evidence containing
the durable run snapshot, validator request/result, artifact manifest, a
portable full-history `repository.bundle`, a baseline-to-final `diff.patch` and
the final product tree in `source/`. Source evidence excludes `.git/` and
`.takt/`, rejects symlinks, preserves file modes
and applies the common secret redactor before cleanup. The mini-du delegation
oracle permits `os/exec` only in `_test.go` files so behavioral tests may invoke
the system oracle; production sources remain fail-closed. The report
distinguishes `true_accept`, `false_accept`, `true_reject` and
`false_reject`. Fake SCM fixtures are deterministic test inputs, not a security
boundary or evidence of a remote provider effect. Start a new suite with `takt
eval flow init <selector> --output DIR`; deterministic executable validation,
not agent text, owns correctness.

The mini-du public task text explicitly states the corpus-specific numeric
contract: default output and `-k` are both integer KiB matching `du -k`.
Scenario mismatches retain bounded normalized candidate/oracle exit codes and
outputs in the validator diagnostic. Advisory analysis must explain that exact
delta; an unrelated discrepancy mentioned by a validation assistant is not a
causally sufficient root cause.

### Runs, outcomes и проценты

Один flow evaluation Run — это один полный изолированный запуск workflow для
одной пары `case + repeat`. Retry или resume узла остаётся попыткой внутри того
же Run и увеличивает `attempts`, но не количество Runs. Если suite содержит три
case и команда запущена с `--repeat 3`, отчёт содержит девять Runs.

После Run внешний validator независимо проверяет итоговый продукт. Сочетание
terminal status workflow и результата validator определяет outcome:

| Workflow | Validator | Outcome | Смысл |
|---|---|---|---|
| `completed` | `valid:true` | `true_accept` | workflow завершился и продукт корректен |
| `completed` | `valid:false` | `false_accept` | workflow заявил завершение, но продукт некорректен |
| не `completed` | `valid:false` | `true_reject` | workflow не завершился и продукт некорректен |
| не `completed` | `valid:true` | `false_reject` | workflow не завершился, хотя продукт уже корректен |

Знаменатели различаются намеренно:

```text
total_runs = все case/repeat Runs в отчёте

evaluated_runs = Runs с успешно завершённым validator,
                 кроме infrastructure_error

valid_rate = true_accept / evaluated_runs
false_accept_rate = false_accept / evaluated_runs
false_reject_rate = false_reject / evaluated_runs

flow_completion_rate = Runs со status=completed / total_runs
validation_error_rate = Runs без корректного результата validator / total_runs
```

Поэтому `Flow valid` отвечает на вопрос «в какой доле измеренных запусков flow
завершился и продукт прошёл validator», а `Completion` — только «как часто
workflow технически дошёл до `completed`». `False accept` показывает опасные
ложные успехи, `False reject` — потери уже корректного результата, а `Validation
errors` — надёжность самого измерительного контура. При одном Run значение
`100%` означает только `1/1`; для сравнения стохастических моделей нужны
несколько cases и repeats. Если `evaluated_runs == 0`, quality rates равны
`null`, а не `0`. Infrastructure errors сохраняют usage и diagnostics, но не
загрязняют quality-знаменатель.

The feature corpus contract is explicit in each `input.md`: `-s`, `-k`, `-H`,
`-h`, `--help`, `--`, `-sk`, `-ks`, `-sH`, and fail-closed unknown options. Every
feature case includes external validator scenarios for these surfaces;
`eval-feature-smoke` runs `implement-basic`, while `eval-feature` runs all
feature cases. Diagnostics with contractually unspecified wording are checked
as separate stdout/stderr streams: failure must use the required exit code,
leave stdout clean where required and write a non-empty stderr diagnostic.
Missing required workflow evidence uses the distinct `missing_artifact` code.
The feature implementation hook requires a non-empty regular `implementation.md` and
retries the same session before downstream review. `time_to_valid_ms` is
available only for `valid: true` results.
Use `--trace` for live runs: progress and durable Run/node events are written to
stderr, while stdout remains the final machine-readable JSON report. Periodic
heartbeats identify the active root/child Run, idle time, limit, last normalized
or streaming activity, awaited boundary and last measured model-request input
tokens. The heartbeat prints `context=<N>t` when optional message usage is
available and `context=unknown` otherwise; it does not report cumulative usage
or infer the model context-window limit. Human lines use `SCOPE | EVENT |
DETAILS`; short Run ID and node attempt are placed in the scope, while full Run
and Session IDs are announced once instead of repeated on every tool event.
Validation/cleanup report checkpoints and the final report write have distinct
event names. Eval-only
assistant inactivity defaults to `5m`; override it with
`--assistant-idle-timeout` without changing the production workflow.

For a quieter external check, run `takt eval status <output-dir>` or
`make eval-status RUN=<output-dir>` from another terminal. The command only
reads the atomically replaced `progress.json`; it never starts or resumes a
workflow and never contacts Pi/models. The snapshot advances through
`prepare → validator_preflight → workflow → validator → evidence → cleanup →
finalized`, updates on durable Run revisions and periodically while waiting,
and retains the final completed/failed state beside `report.json`. Live token
and cost counters contain only completed executions already persisted by the
runtime. If the eval process is killed, `status` can remain `running`; inspect
`Updated`/`updated_at` for staleness.

Для оценки через Pi объект `assistants.<name>.settings` копируется как нативный
JSON Pi в изолированный `.pi/settings.json`; конфигурация корпуса задаёт
`httpIdleTimeoutMs: 300000`. Затем контур оценки отключает повторы уровня агента
Pi и SDK провайдера. Эти ключи имеют приоритет над встроенными настройками,
поэтому видимым сохраняемым циклом повторов владеет Takt. `eval status`
добавляет для каждого узла состояние провайдера (`awaiting_response`,
`streaming` или повтор),
порядковый номер вызова модели, длительность состояния и последнюю
отредактированную ошибку провайдера. Обычные настройки Pi вне оценки не
изменяются.

Model comparisons use shared Config presets: `takt eval flow <suite.yaml>
--model-preset <name>` or `EVAL_PRESET=mixed make eval-feature`. One-off alias
overrides use repeated `--model alias=provider/model`; Make passes generic
`MODEL_<ALIAS>` environment variables. Reports record the selected preset and effective model
references in `strategy`; the benchmark fingerprint remains model-independent.
Inspect one saved suite report with `takt eval stats <output-dir>` (human text by
default, `--json` for structured output). It includes identity, outcomes,
node attempts, assistant executions, attempts/retries/resumes, tokens, duration,
time-to-valid, cost, diagnostics, per-execution identity usage and case rows.
For new flow reports the assistant-step table shows wall time measured from the
first durable `node.started` to the terminal node event. This includes tools,
retry backoff and waiting and must not be interpreted as provider inference
latency; old reports without optional node `duration_ms` show `-`.
`ASSISTANT SESSIONS` lists the opaque Session ID of each actual assistant
execution together with case, step, workflow attempt, provider attempt and
fresh/resume mode. Session IDs come from durable execution records; no stable
Pi/OpenCode URL is inferred by Takt.
`takt eval compare A B` renders explicit A/B assessments for correctness,
reliability, efficiency and every case/metric. Overall direction prioritizes
valid results, then flow reliability, then resources only when quality is equal.
Higher valid/completion is better; lower false accepts/rejects,
infrastructure/validator errors, tokens, duration, attempts and cost is better.
Percentages retain their numerator/denominator, missing measurements are not
printed as percentages, all deltas are `B-A`, and differing benchmark
fingerprints remain fail-closed.

Расследование отказов детерминировано и отделено от оценки качества. Команда
`takt eval stats <output-dir>` связывает каждый неуспешный сценарий с первой
авторитетной причиной уровня валидатора, среды выполнения или узла. Команда
`takt eval inspect <output-dir> [--case ID] [--repeat N]` читает только
сохранённые свидетельства и показывает эту причину, незавершённые узлы, diff,
исходники, полный Git bundle, артефакты,
вызовы SCM и нормализованные события инструментов и жизненного цикла провайдера.
`activity.json` не включает сообщения
ассистента, повторяющиеся обновления потока и вывод инструментов, но сохраняет
ограниченные наблюдения Pi о ходе запроса, первом событии потока, завершении и
повторах, после чего проходит через общий механизм редактирования. Наблюдаемые
клиентом `wait_ms`, `stream_ms` и `total_ms` не определяют время очереди
провайдера или
вычисления на сервере. Отдельная детерминированная причинная цепочка связывает
сохранённую конечную причину ассистента, статистику использования и число
вызовов инструментов с пустым результатом, отказом валидации и пропущенными
зависимыми узлами.
Производные наблюдения имеют явную уверенность
`CONFIRMED|INFERRED|UNAVAILABLE` и не заменяют вердикт валидатора. Ни одна из
команд не запускает и не возобновляет процесс и не обращается к модели; любой
будущий LLM-анализ должен быть отдельной явно выбранной командой с цитируемыми
свидетельствами и сохранёнными данными модели, сессии и статистики
использования.

Before the first case checkpoint creates `report.json`, `eval inspect` falls
back to `progress.json` and reports the current case/running nodes with an
explicit `UNAVAILABLE` cause. `eval analyze` does not run against incomplete
evidence: it returns `evaluation is still running` without starting the
dedicated analyzer model.

`takt eval analyze <saved-output-dir>` is an optional, read-only advisory
investigation of saved problem cases. It requires the dedicated `takt_analyze`
alias, stores redacted timestamped manifests and structured citations, and never
changes deterministic outcomes, benchmark identity or quality denominators.
Provider/protocol failures remain explicit analysis statuses; protocol failures
retain bounded redacted raw model output when available, the generated
`evidence-manifest.json` is a validated citation target, and relative analyzer
session paths are resolved within the execution workspace before cleanup. The
manifest-root-prefixed citations are accepted only when their suffix is listed
in the manifest. Equivalent `#/pointer`, `path:line-range`, and zero-based text
`/N` forms are normalized to canonical citations. `--language en|ru` (default
`en`) controls human-readable advisory strings and is persisted in the analysis
report and manifest; JSON keys and enum values remain stable. `failure_mode`
remains an untranslated lowercase snake_case machine code for cross-run
comparison. A completed
advisory result must explain the causal mechanism, identify the bounded failure
point and give
one concrete prevention. Validator-only citations are insufficient: at least
one checked runtime, assistant, artifact, source, diff, or SCM citation is
required. The original evaluation report remains immutable.
При наличии свидетельств жизненного цикла Pi рекомендательный анализ обязан
различать задержку перед повтором, ожидание клиента и передачу потока;
ненаблюдаемый интервал нельзя называть размышлением модели.

## 9. Критерий полезности Takt

Takt подтверждает ценность, если новая стратегия добавляется изменением workflow/config/commands, общий benchmark запускается без изменения runtime, а отчёт позволяет доказательно связать результат с точной моделью, стратегией, набором заданий и валидатором.

## 10. Сравнительный benchmark v0.1.45

`examples/route-dsl-benchmark` содержит 25 размеченных cases и три agent-neutral workflow-стратегии. 10 cases являются regression corpus, 15 — production-shaped synthetic cases. Live качество оценивается только штатным внешним валидатором; репозиторий не заявляет synthetic cases как обезличенные production данные.

`takt eval benchmark` пишет immutable strategy reports и общий `benchmark.json`; `takt eval compare` показывает both-valid / baseline-only / candidate-only / both-invalid и разрез по category. Regression gates допускают абсолютные пороги success и ограничение регрессии cost/time. Полный task-level Dynamic Takt benchmark остаётся отдельным следующим измерительным расширением.


## Task-level Dynamic Takt benchmark

`TaskEvaluationMatrix` проверяет не отдельный workflow, а полный control path `Task → Router → workflow|template|dynamic → checkpoint → replan → result`. Case manifest задаёт ожидаемый route/template/workflow, terminal status и минимальное число plan revisions.

Основные метрики:

- route accuracy;
- final success rate;
- plan revisions и replanner runs;
- replan expectation rate;
- unexpected needs-input и router fallbacks;
- aggregate usage/duration;
- pairwise baseline/candidate outcomes.

Deterministic fake-agent scenario специально отделён от model-quality evaluation: он доказывает, что measurement path видит неправильный route и фактический `replace_remaining`.

## 11. Go production-shaped live-срез — 2026-08-10

Пять изолированных Go cases проверялись внешним validator `gofmt + go test + race + vet`. Default output вынесен в `${TMPDIR:-/tmp}/takt-go-benchmark/evals`, потому что OpenCode внутри родительского Git-root может выбрать корень repository вместо переданного workspace. Success по-прежнему требует `quality_node_status=completed && valid=true`.

- Pi `0.83.0`, `aihub/Qwen/Qwen3.6-27B`, `repeat=3`: direct `14/15`, feedback repair `14/15 → 15/15`, один exact resume, среднее число попыток repair `1.0667`.
- OpenCode `1.18.14`, current requested CLI model `aihub-sbt/Qwen/Qwen3-Coder-Next`, `--pure`, `skills: []`: smoke `5/5 → 5/5`; полный `repeat=3` — direct `15/15`, feedback repair `13/15 → 15/15`. Два `GOFMT_FAILED` восстановлены exact resume, failed executions отсутствуют, все пять cases stable-valid.
- OpenCode `1.18.14`, direct requested CLI model `aihub-sbt/Qwen/Qwen3.6-27B`, `--pure`, `skills: []`: isolated smoke дал `0/5 → 5/5`, но полный `repeat=3` — `0/15 → 0/15` без настоящих NDJSON `tool_use`.
- OpenCode через requested `aihub-proxy/Qwen/Qwen3.6-27B`: полный `repeat=3` до исправления SSE дал direct `0/15`, feedback repair `0/15 → 6/15`; все шесть valid outcomes получены на третьей попытке. Все наблюдавшиеся tool calls были native, compact rewrite в этом прогоне не использовался.
- Proxy-прогон выявил три transport failure из-за отсутствующего SSE-разделителя перед `[DONE]` и один внешний `Unauthorized`. После исправления разделителя диагностический `repeat=1` выполнил 20 OpenCode executions без parse/adapter/provider failure, но остался `0/5 → 0/5` из-за отсутствия tool calls.

Противоположные Qwen 3.6 outcomes сохранены без повторного полного прогона до желаемого результата. Evidence подтверждает measurement/resume path, устраняет конкретный transport defect и показывает межзапусковую нестабильность tool use. Для Coder-Next repair сохранил final success `1.0`, но не улучшил direct `15/15`; отдельное преимущество стратегии на этом corpus не заявляется. Метрики времени не используются из-за неравномерной загрузки provider, а provider-side routing не считается наблюдаемым сверх requested CLI model.
