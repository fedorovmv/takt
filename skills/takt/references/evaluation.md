# Flow evaluation

Опиши evaluation обычным `takt/v1alpha1` workflow. В нём `matrix` выполняет
authored DAG для каждого case/repeat, governed `workflow` при необходимости
создаёт candidate Run, deterministic validator пишет
`takt-validation/v1alpha1`, а `assessment` pin-ит result и evidence.

Минимальная форма запуска:

```bash
takt eval flow workflows/evaluate.yaml \
  --target code:feature-development \
  --config config.yaml \
  --cases cases \
  --repeat 3 \
  --gate validation_error_rate.max=0 \
  --assistant-idle-timeout 15m \
  --trace
```

Launcher проверяет corpus/config/workspaces, формирует
`takt-evaluation-input/v1alpha1` и стартует один root Run. В workflow читай
items через `$INPUTS.cases`; внутри branch используй `$MATRIX.item`. Передавай
validator request через `script.stdin`. Фиксированных стадий candidate и
validator нет: preparation, несколько моделей, review, checks, evidence и
advisory assessments — обычные узлы DAG.

Primary assessment обязан происходить из deterministic `bash|script|adapter`,
иметь `case_id`, положительный `repeat` и immutable evidence artifact.
`valid:false` — корректно измеренный результат, а malformed result или missing
evidence — failure измерительного Run. Gate failure меняет только exit code
команды после durable reload, не `Run.status`.

Сохрани выведенный Run ID и используй общие команды:

```bash
takt run status run-...
takt run stats run-... --check-gates
takt run inspect run-... --case implement-basic --repeat 1
takt run assessment run-... --role primary
```

`takt eval status|stats|inspect` принимают тот же Run ID. Для bundled mini-du
corpus используй `EVAL_PRESET=qwen38 EVAL_IDLE_TIMEOUT=15m make eval-feature`;
live eval требует модели/credentials и не входит в release checks.

## Legacy compatibility

`takt eval flow init` пока создаёт deprecated
`takt-flow-evaluation/v1alpha1` `suite.yaml`. Только exact suite version выбирает
старый fixed-stage runner; `eval status|stats|inspect <directory>` продолжает
read-only разбор его `progress.json`/`report.json`. Не используй legacy suite
как шаблон для нового evaluation workflow и не импортируй старые отчёты в Run
Store.
