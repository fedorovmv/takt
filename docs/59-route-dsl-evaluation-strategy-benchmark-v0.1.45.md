# Route DSL Evaluation & Strategy Benchmark — v0.1.45-alpha

`v0.1.45-alpha` превращает существующий evaluation runner из инструмента прогона одной стратегии в воспроизводимый сравнительный benchmark. Runtime и scheduler не меняются: измерительный слой использует обычные workflow, durable Run state и события.

## Матрица стратегий

Новый `EvaluationMatrix` описывает общий benchmark и несколько стратегий:

```yaml
apiVersion: takt/evaluation/v1alpha1
kind: EvaluationMatrix
metadata:
  name: route-dsl
benchmark:
  id: route-dsl-v1
  baseline_strategy: baseline-direct
  cases: cases
  case_manifest: cases.yaml
  workspace_template: workspace
  repeat: 3
  quality_node: full-validation
  generation_node: implement
  validator:
    id: route-tool
    version: 1
    path: workspace/route-tool
strategies:
  - id: baseline-direct
    workflow: strategies/baseline-direct.yaml
    config: config.yaml
  - id: feedback-repair
    workflow: strategies/feedback-repair.yaml
    config: config.yaml
```

`takt eval benchmark matrix.yaml` запускает каждую стратегию на одном benchmark identity и пишет `benchmark.json`. Конкретный coding-agent выбирается через `default_assistant`; benchmark workflow использует логический `assistant: coding-agent`.

## Сравнение

`takt eval compare <baseline-dir> <candidate-dir>` сравнивает отчёты попарно по `case_id + repeat`. Результат различает:

- `both_valid`;
- `baseline_only_valid`;
- `candidate_only_valid`;
- `both_invalid`.

Дополнительно строится разрез по `category` из `CaseManifest` и delta для success@1, final success, attempts-to-valid, score, cost-per-valid и time-to-valid.

## Метрики

`report.json` сохраняет прежние метрики и дополнительно:

- точный `time_to_valid_ms`: от создания Run до durable `node.completed` quality-node;
- `retry_scheduled` и fingerprints retry;
- число и стоимость failed executions;
- diagnostics по fingerprint;
- `stable_valid_cases`, `stable_invalid_cases`, `unstable_cases` при повторных прогонах.

Настоящий time-to-valid не подменяется амортизированной длительностью эксперимента. `amortized_end_to_end_ms_per_valid` сохраняется как отдельная cost-of-experiment метрика.

## Regression gates

Matrix может задавать пороги:

```yaml
gates:
  - strategy: feedback-repair
    final_success_rate_min: 0.85
    success_at_1_min: 0.60
    cost_per_valid_max_regression_percent: 20
    time_to_valid_max_regression_percent: 20
    unstable_cases_max: 2
```

При нарушении gate команда возвращает non-zero, но `benchmark.json` всё равно сохраняется с `passed: false` и деталями каждого нарушения.

## Идентичность эксперимента

`experiment_fingerprint` зависит от benchmark ID, repeat и strategy/benchmark fingerprints всех участников, но не от временного пути сгенерированного matrix-файла. Benchmark fingerprint включает CaseManifest, поэтому изменение labels/corpus создаёт другую измерительную идентичность.

## Route DSL corpus

`examples/route-dsl-benchmark/` содержит 25 cases:

- 10 прежних regression cases;
- 15 новых `production-shaped` cases с ветвлениями, fallback, schema variants, mapping, rate limits, join и противоречивыми требованиями.

Новые cases не объявляются реальными обезличенными производственными ТЗ: таких исходных данных в репозитории нет. `cases.yaml` явно хранит `source`, `category` и `difficulty`.

Штатный live benchmark требует внешний Route DSL validator. В репозитории есть воспроизводимый fake benchmark, который доказывает работу matrix/compare/gates/time-to-valid: baseline остаётся невалидным, feedback-repair исправляет результат после детерминированной обратной связи.

## Ограничение этого среза

`v0.1.45` сравнивает workflow-стратегии через существующий evaluation runner. Полный task-level benchmark `Task Router → Dynamic Plan → replan` не маскируется под обычный workflow и остаётся отдельным расширением после появления реального корпуса и внешних результатов. Новых orchestration-механизмов в этом релизе нет.
