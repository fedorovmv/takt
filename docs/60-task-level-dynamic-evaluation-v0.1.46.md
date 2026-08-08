# Task-level Dynamic Evaluation & Security Closure — v0.1.46-alpha

`v0.1.46-alpha` делает две вещи: закрывает найденные обходы persistence redaction из ревью `v0.1.44` и добавляет следующий evidence-oriented срез — benchmark полного Task Router / Dynamic Takt control path. Runtime/scheduler не получают второй режим исполнения: task benchmark вызывает обычный `control.Service`, а затем анализирует durable Dynamic Plan и Run state.

## Общая граница redaction

Runtime и control/external worker paths теперь используют одну модель persistence redaction.

Перед durable commit редактируются известные secrets в:

- approval answers;
- external assistant messages;
- external tool input/output/reasons;
- external terminal result, stdout/stderr/error/structured payload;
- domain operation receipt;
- event data;
- textual artifacts.

External text artifact редактируется до записи и checksum считается по фактически сохранённому содержимому. Non-text artifact с известным secret отклоняется fail-closed.

SecretRef, который появляется только после template rendering assistant env, регистрируется до запуска adapter. Поэтому `TOKEN: ${...}`/request templating не является обходом explicit `secret://ENV_NAME`.

## Task-level evaluation

Новый контракт:

```bash
takt eval task-benchmark matrix.yaml --output results --repeat 3 --replace --json
```

Matrix использует `TaskCaseManifest` и несколько workspace strategies. Каждый case проходит реальный control path:

```text
Goal
→ Plan
→ Task Router
→ workflow | simple-reliable | dynamic
→ ExecutePlan
→ checkpoint replanner
→ terminal plan status
```

Отчёт `takt-task-evaluation-matrix/v1alpha1` сохраняет для каждой пары `case_id + repeat`:

- фактический route/template/workflow;
- route correctness относительно case expectation;
- final success;
- plan revisions;
- replanner/execution Runs;
- ожидаемое перепланирование;
- needs-input/router fallback;
- aggregate usage и duration.

Pairwise compare различает cases, где правильный route получила только baseline или только candidate, а также отдельно сравнивает terminal success. Это принципиально: успешно завершившийся workflow не доказывает, что Router выбрал подходящую стратегию.

## Regression gates

Task matrix поддерживает:

```yaml
gates:
  - strategy: semantic-router
    route_accuracy_min: 0.9
    final_success_rate_min: 0.9
    replan_expectation_rate_min: 0.8
    unexpected_needs_input_max: 1
    router_fallbacks_max: 0
```

Как и workflow-level benchmark, отчёт сохраняется до возврата non-zero gate error.

## Deterministic E2E

Release fixture сравнивает две реальные конфигурации builtin `code` profile:

- `force-template` принудительно выбирает `simple-reliable`;
- `semantic-router` использует обычный Router.

Три cases:

1. ordinary task → template;
2. `fixture dynamic audit` → dynamic;
3. `fixture dynamic replan` → dynamic и минимум две plan revisions.

Fixture replanner реально возвращает `replace_remaining`. В release contract baseline получает `1/3` route accuracy, candidate — `3/3`, а replan case фиксирует revision 2.

Это проверка correctness измерительного контура, а не заявление о качестве модели. Production evidence по-прежнему требует реальных обезличенных задач и реальных models/validators.

## Исправления evaluation v0.1.45

- explicit `benchmark.repeat: 0` отклоняется так же, как требует schema;
- immediate `node.retry` также несёт diagnostic fingerprint;
- stable-valid/stable-invalid/unstable aggregation покрыта прямым тестом;
- failed-execution cost покрыт прямым тестом;
- time-to-valid проверяется на точном durable event timestamp;
- gate failure имеет unit coverage;
- новые evaluation schemas зарегистрированы в `schemas/README.md`;
- Route DSL strategy benchmark и task benchmark входят в `scripts/verify.sh`;
- matrix/compare report schemas больше не оставляют основные массивы как нетипизированные `object`.

## macOS / sandbox regressions

- `test-adapter-platform.sh` выбирает `python3` с fallback на `python`;
- optional sandbox test больше не зависит от `/bin/true`;
- `sandbox-exec` profile проверяется кроссплатформенно;
- на macOS при доступном `sandbox-exec` есть исполняемый fail-closed test записи;
- degraded sandbox decision проверяется после перечитывания durable state;
- after-hook с `sandbox.enforcement: required` не может обойти OS sandbox.

## План после релиза

Roadmap теперь evidence-driven:

1. live Route DSL production matrix;
2. Go + Document production scenarios;
3. v0.2 contract/schema stabilization;
4. reference external wrappers/adapters;
5. human-reviewed learning loop для skills/blocks.

Исторический backlog заменён фактическими незакрытыми gaps в `docs/14-backlog-v0.2.md`.

## Версии

```text
Takt:         0.1.46-alpha
code profile: 0.16.0
Takt skill:   0.28.0
```

Профиль `code` не меняет внешний контракт в этом срезе: task benchmark использует его существующий Router/Planner/Replanner как объект измерения.
