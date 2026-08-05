# Script-узлы и типизированные артефакты v0.1.29

## Назначение среза

`v0.1.29-alpha` добавляет детерминированные script-узлы и явный контракт артефактов. Workflow больше не обязан использовать inline `bash` для небольших программ обработки данных и не должен передавать значимые результаты через случайные имена файлов.

Срез решает четыре задачи:

1. Запускает версионируемый скрипт через понятный runtime-контракт.
2. Включает исходник скрипта и его зависимости в fingerprint определения.
3. Сохраняет результат узла как типизированный неизменяемый снимок с SHA-256 и producer metadata.
4. Передаёт ссылки на артефакты между обычными узлами, governed child Run и fan-out без копирования содержимого в prompt.

## Script-узел

```yaml
- id: build-plan-index
  script:
    runtime: command
    path: tools/build-plan-index
    args: [--format, json]
    env:
      MODE: strict
    dependencies:
      - schemas/plan.schema.json
  output_format:
    type: object
    properties:
      files:
        type: array
        items:
          type: string
    required: [files]
  output_type: plan-index
  output_mime: application/json
```

Поддерживаемые runtime:

- `command` — запускает указанный исполняемый файл напрямую;
- `python` — запускает файл через `python3` либо inline-код через `python3 -c`;
- `node` — запускает файл через `node` либо inline-код через `node -e`.

Поля:

- `path` — файл относительно workflow;
- `inline` — встроенный код; задаётся вместо `path`;
- `args` — аргументы процесса после runtime и source;
- `env` — дополнительные переменные окружения;
- `working_directory` — каталог относительно workflow;
- `dependencies` — дополнительные файлы, влияющие на поведение скрипта и fingerprint.

Для `command` и `go` обязателен `path`. Для `python` и `node` задаётся ровно одно из `path` и `inline`.

Runtime передаёт:

```text
TAKT_RUN_ID
TAKT_NODE_ID
TAKT_ATTEMPT
TAKT_WORKSPACE
TAKT_ARTIFACTS_DIR
```

Stdout и stderr сохраняются раздельно. `Output` формируется из stdout без потери raw stdout. Если задан `output_format`, stdout должен содержать одно JSON-значение, которое нормализуется только в `Output`.

## Fingerprint

В fingerprint workflow входят:

- каноническое определение script-узла;
- исходные байты `script.path`;
- каждый файл из `script.dependencies`;
- inline-код, аргументы, environment и working directory как часть определения.

Изменение исходника или зависимости блокирует resume старого Run с ошибкой изменения определения.

## Типизированный артефакт

Любой `command`, `prompt`, `bash` или `script` может объявить:

```yaml
output_type: plan
output_mime: text/markdown
output_path: $ARTIFACTS_DIR/plan.md
```

`output_type` включает сохранение артефакта. Если `output_path` отсутствует, Takt сохраняет нормализованный `Output` узла. Если путь указан, Takt копирует регулярный файл в хранилище Run после успешного завершения узла.

Источник файла должен находиться в execution workspace либо внутри каталога артефактов Run. Это предотвращает случайную регистрацию произвольного файла за пределами выполняемого процесса.

Метаданные:

```json
{
  "id": "create-plan:plan:1",
  "type": "plan",
  "mime": "text/markdown",
  "path": ".takt/runs/<run-id>/artifacts/nodes/create-plan/1/plan.md",
  "sha256": "...",
  "size": 4096,
  "producer_run_id": "...",
  "producer_node_id": "create-plan",
  "attempt": 1,
  "created_at": "..."
}
```

Артефакт появляется только после успешного действия, успешных hooks и проверки `output_format`. Ошибка чтения, копирования или хеширования переводит узел в ошибку `artifact`.

## Ссылки в шаблонах

Downstream-узел может использовать:

```text
${nodes.create-plan.artifacts.plan.path}
${nodes.create-plan.artifacts.plan.sha256}
${nodes.create-plan.artifacts.plan.mime}
${nodes.create-plan.artifacts.0.path}
```

Поддерживаемые поля: `id`, `type`, `mime`, `path`, `sha256`, `size`, `producer_run_id`, `producer_node_id`, `attempt`.

Путь указывает на сохранённый снимок, а не на временный файл producer-узла.

## Governed child Run и fan-out

Артефакты ребёнка:

- остаются в собственном хранилище child Run;
- включаются в состояние родительского `workflow`-узла;
- поднимаются в агрегированный список артефактов родительского Run;
- доступны через обычные `${nodes.<id>.artifacts...}` ссылки.

Fan-out сохраняет артефакты для каждого `ChildRunItemState`. Агрегация не меняет путь и checksum, поэтому provenance остаётся привязанным к фактическому producer Run.

## CLI

```bash
takt artifacts <run-id>
takt artifacts <run-id> --node create-plan
takt artifacts <run-id> --type plan
takt artifacts <run-id> --recursive
takt artifacts <run-id> --json
```

`--recursive` обходит дерево governed children. CLI дедуплицирует одинаковые ссылки по идентификатору артефакта.

## Использование в профиле code 0.8.0

- review perspectives строятся script-узлом из `tools/review-perspectives` и сохраняются как `application/json`;
- процессы PIV и idea-to-PR регистрируют `plan.md` как артефакт типа `plan`;
- interactive PRD регистрирует `prd.md` как артефакт типа `prd`.

Это проверяет новую функцию на самом встроенном каталоге, а не только на искусственном примере.

## Ограничения

- Takt не устанавливает Python/Node dependencies и не создаёт виртуальные окружения;
- артефакты локальны и не загружаются во внешний object storage;
- повторная регистрация одного type одним producer/attempt заменяет ссылку в агрегированном представлении, при этом события и child history сохраняют фактическое происхождение;
- secret redaction содержимого артефактов пока отсутствует, поэтому runtime остаётся trusted local.

## Проверки

Контракт `scripts/test-script-artifacts.sh` проверяет:

- запуск command script;
- structured output без потери raw stdout;
- сохранение output и file artifacts;
- SHA-256 и producer metadata;
- CLI-фильтрацию;
- передачу child artifact родителю;
- изменение script dependency и блокировку небезопасного resume.
