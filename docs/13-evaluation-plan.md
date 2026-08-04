# План оценки агентных стратегий

Статус: benchmark-контур реализован в `v0.1.16-alpha` командами `takt eval run/report`. Инфраструктурный набор с fake Pi отделён от реального Route DSL benchmark.

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
- количество ручных исправлений результата — следующий отдельный показатель.

Агрегаты:

- `success_at_1`;
- `final_success_rate`;
- `average_attempts_to_valid`;
- `average_score`;
- `cost_per_valid`;
- `amortized_end_to_end_ms_per_valid`;
- diagnostics по severity/code;
- распределение assistant/requested/resolved model;
- `usage_by_execution_identity` и число mixed-узлов.

Стоимость и амортизированная end-to-end длительность на корректный результат включают затраты неуспешных запусков. Настоящий time-to-valid пока не рассчитывается.

Измеренный ноль сериализуется как `0`; недоступный показатель — как `null`.

## 7. Правила сравнения

- одна стратегия запускается на всех заданиях;
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

`examples/route-dsl-eval` проверяет инфраструктуру с fake Pi. `examples/route-dsl-benchmark` запускает реальный Pi и штатный валидатор на десяти заданиях.

## 9. Критерий полезности Takt

Takt подтверждает ценность, если новая стратегия добавляется изменением workflow/config/commands, общий benchmark запускается без изменения runtime, а отчёт позволяет доказательно связать результат с точной моделью, стратегией, набором заданий и валидатором.
