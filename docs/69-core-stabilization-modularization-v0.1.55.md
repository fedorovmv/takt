# Core Stabilization & Modularization — v0.1.55-alpha

`v0.1.55-alpha` продолжает feature freeze. Релиз не удаляет возможности и не расширяет внешний Workflow/Run/MCP/adapter surface. Цель — отделить пользовательски стабильный контур от экспериментальных механизмов, расширений и внутренних инструментов так, чтобы они могли развиваться с разной скоростью и не увеличивали связность core.

## Граница модулей

Takt больше не трактует весь `internal/` как единый слой продукта.

### Stable core

К стабильному направлению зависимостей относятся прежде всего:

- `internal/application` — Run lifecycle и небольшие стабильные use cases;
- `internal/runtime` — DAG execution и durable execution semantics;
- `internal/workflow`, `internal/config`, `internal/profile`, `internal/store`;
- публичные SDK/контракты assistant, domain adapter и task source.

Stable packages не импортируют `internal/experimental`, `internal/tooling` или `internal/extensions`. Это проверяется architecture test.

### Extensions

`internal/extensions` содержит возможности, которые подключаются к core, но не являются его обязательной семантикой:

- Block Catalog;
- Package Distribution;
- Notifications;
- adapter/package/block application facades.

Загрузка установленных BlockPackage вынесена в extension-aware `catalogload`; `profile.Resolve` снова описывает только сам профиль и не зависит от package manager.

### Experimental

`internal/experimental` содержит механизмы, которые остаются рабочими и тестируемыми, но пока не замораживаются как стабильная модель использования:

- Dynamic Flow: Router, Dynamic Plan, checkpoint/replan/repair/promote и structured Task orchestration;
- Evidence routing;
- Host Control;
- Learning Loop;
- workspace/repository catalog для Dynamic Flow.

Dynamic Flow использует стабильные Run/Catalog APIs. Stable core не знает о Dynamic Plan/Router/Task evidence.

### Tooling

`internal/tooling` содержит средства проверки и развития Takt, а не runtime semantics:

- evaluation/benchmark/matrix/compare;
- compatibility/field/schema audit.

Команды CLI сохранены ради совместимости, но tooling больше не входит в stable application graph.

## Application graph

После выделения модулей production-код `internal/application` уменьшился примерно с 6,8 тыс. строк в `v0.1.54` до 3,5 тыс. строк без удаления функций.

Production LOC после разделения:

| Контур | Строк Go-кода |
| --- | ---: |
| stable `internal/application` | 3 493 |
| `internal/experimental` | 5 639 |
| `internal/extensions` | 2 681 |
| `internal/tooling` | 3 184 |
| `internal/runtime` | 5 164 |

LOC используется только как индикатор связности. Цель разделения — направленные зависимости и независимая стабилизация модулей, а не минимизация числа строк сама по себе.

## YAML

Самописный `internal/yamlmini` удалён. YAML syntax теперь разбирает поддерживаемый upstream `go.yaml.in/yaml/v3` v3.0.4.

В Takt остаётся только небольшой `internal/yamlcodec`, который отвечает за специфичную для Takt семантику:

- использование существующих `json` tags как канонических имён полей;
- strict unknown-field validation;
- подсказку ближайшего имени поля;
- единый YAML/JSON decode path для публичных контрактов.

Таким образом Takt больше не сопровождает YAML lexer/parser, block scalars, anchors и общую YAML grammar.

`internal/schemasubset` пока сохраняется: это не общий JSON Schema parser, а намеренно ограниченный публичный dialect `takt-schema-subset/v1`, зафиксированный ADR-078. Его замена на generic validator допустима только при сохранении этой contract boundary.

## User journey gate

Добавлен отдельный release gate `make journeys`. Он использует настоящий `takt` binary через Go E2E harness и проверяет пользовательский стабильный путь:

1. `init -> validate -> run -> status/events/artifacts`;
2. approval -> `answer` -> продолжение;
3. failure -> `retry` -> успешное завершение;
4. reusable subworkflow.

Полный Go suite остаётся источником внутренней correctness, а journeys отвечают на отдельный вопрос: может ли пользователь пройти основной сценарий через публичный CLI.

## CLI surface

Команды не удалены. Верхнеуровневая usage-подсказка теперь явно разделяет группы:

- `stable`;
- `extensions`;
- `experimental`;
- `tooling`.

Это статус стабильности, а не feature flag: существующие experimental/tooling команды продолжают работать и участвуют в regression tests.

## Что не меняется

- `takt/v1alpha1` Workflow contract;
- durable Run state/event semantics;
- MCP operation compatibility;
- process/adapter/task-source public SDK contracts;
- Dynamic Flow data/state compatibility;
- evaluation/learning/package functionality.

Следующий этап стабилизации должен быть evidence-driven: сначала user/live scenarios и исправление обнаруженных дефектов, затем решение о promotion отдельных experimental contracts в stable surface.
