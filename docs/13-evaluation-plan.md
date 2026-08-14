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
the durable run snapshot, validator request/result and artifact manifest. The
report distinguishes `true_accept`, `false_accept`, `true_reject` and
`false_reject`; rates use evaluated runs as their denominator. Fake SCM fixtures
are deterministic test inputs, not a security boundary or evidence of a remote
provider effect. Start a new suite with `takt eval flow init <selector> --output
DIR`; deterministic executable validation, not agent text, owns correctness.
Use `--trace` for live runs: progress and durable Run/node events are written to
stderr, while stdout remains the final machine-readable JSON report. Periodic
heartbeats identify the active root/child Run, idle time, limit, last normalized
or streaming activity and awaited boundary. Eval-only
assistant inactivity defaults to `5m`; override it with
`--assistant-idle-timeout` without changing the production workflow.

Model comparisons use shared Config presets: `takt eval flow <suite.yaml>
--model-preset <name>` or `EVAL_PRESET=mixed make eval-feature`. One-off alias
overrides use repeated `--model alias=provider/model`; Make passes generic
`MODEL_<ALIAS>` environment variables. Reports record the selected preset and effective model
references in `strategy`; the benchmark fingerprint remains model-independent.

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
