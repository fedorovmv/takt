# Идентичность benchmark и предметные метрики качества в v0.1.14-alpha

## Цель

Версия `v0.1.13-alpha` уже позволяла воспроизводимо запускать каталог заданий, но отчёты нельзя было доказательно сопоставлять между изменениями workflow, config, Markdown-команд, модели и валидатора. `v0.1.14-alpha` добавляет стабильную идентичность эксперимента и предметно-независимый контракт качества.

## Идентичность стратегии

`report.json` содержит:

- `strategy.id` — читаемое имя, заданное через `--strategy-id`;
- `strategy.workflow_fingerprint`;
- `strategy.config_fingerprint`;
- `strategy.commands_fingerprint`;
- `strategy.fingerprint` — общий fingerprint трёх определений.

Изменение prompt, workflow, параметров модели, исполнителя или Markdown-команды меняет fingerprint. Переименование `strategy.id` не подменяет содержательную идентичность.

## Идентичность benchmark

В отчёт входят:

- `benchmark.id`;
- fingerprint упорядоченного набора Markdown-заданий;
- fingerprint копируемого workspace template без runtime-каталогов `.takt` и `bin`;
- число заданий;
- `quality_node` и `generation_node`;
- версия протокола качества;
- ID, версия, путь и SHA-256 валидатора;
- общий `benchmark.fingerprint`. Изменение содержимого или executable mode файла workspace меняет fingerprint; сгенерированные `.takt` и `bin` исключаются так же, как при копировании workspace.

Валидатор можно указать файлом или каталогом через `--validator-path`. Относительный путь разрешается внутри workspace template. Fingerprint рассчитывается до создания output.

## Контракт качества

Узел, заданный через `--quality-node`, должен напечатать один JSON-объект:

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

Takt проверяет обязательные version/type/valid, запрет `null` вместо объявленных типов, диапазон score, структуру checks, severity диагностик, неизвестные поля и отсутствие второго JSON-значения. Нарушение контракта получает `quality_contract` и останавливает benchmark как ошибка измерительного контура.

## Модели и исполнители

`NodeState` и evaluation report теперь сохраняют:

- имя и версия assistant;
- запрошенное логическое имя модели;
- provider и model ID из config;
- параметры запрошенной модели;
- provider/model, фактически подтверждённые адаптером; для Pi используется `responseModel` последнего assistant message с fallback на выбранную модель.

Это позволяет обнаружить маршрутизацию провайдера на другую модель и не смешивать такие запуски в одной выборке.

## Метрики

Summary включает:

- `success_at_1`;
- `final_success_rate`;
- среднее число попыток до корректного результата;
- среднюю предметную оценку;
- стоимость и время на один корректный результат;
- diagnostics по severity и code;
- распределение assistant, requested model и resolved model.

Стоимость и время на корректный результат учитывают весь прогон benchmark, включая неуспешные задания.

## Два разных набора

- `examples/route-dsl-eval` остаётся инфраструктурным contract suite с fake Pi и синтетическим валидатором;
- `examples/route-dsl-benchmark` предназначен для реального Pi, штатного Route DSL validator и десяти заданий.

Реальный benchmark запускается только при явно переданных config и валидаторе. Он не входит в локальный `make check`, потому что требует внешней модели, авторизации и предметного инструмента.

## Схемы

Добавлены:

- `schemas/validation-result.schema.json`;
- `schemas/evaluation-report.schema.json`.

`schemas/run-state.schema.json` расширена assistant/requested/resolved model.
