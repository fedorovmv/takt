# Дополнительная стабилизация по аудиту в v0.1.3-alpha

## Статус

Повторный аудит выявил три P1-дефекта и одну неоднозначность `until`. Все четыре пункта закрыты кодом, спецификацией и регрессионными тестами.

## 1. Общий output budget stdout/stderr

Проблема: `os/exec` копирует stdout и stderr параллельно, а общий счётчик `used/truncated` менялся без синхронизации.

Исправление:

- `outputBudget` защищён mutex;
- stdout и stderr продолжают использовать единый лимит;
- добавлен process-тест с одновременным выводом в оба потока;
- тест входит в штатный `go test -race ./...`.

## 2. Timeout и cancellation portable hooks

Проблема: context попытки завершался сразу после действия, поэтому `after_node` и `before_complete` выполнялись без timeout узла. Cancellation hook превращалась в обычный `hook_failed`.

Исправление:

- один attempt context живёт от `before_node` до `before_complete`;
- timeout охватывает `before_node`, действие, `on_failure`, `after_node`, `before_complete`;
- `timed_out` и `cancelled` из hook пробрасываются как execution kind;
- cancellation родительского context переводит Node и Run в `cancelled`;
- добавлены тесты всех hook-фаз.

## 3. Вложенные loop_group

Проблема: дочерние состояния хранились в общей карте по необработанному ID. Вложенный цикл мог перезаписать и удалить NodeState внешнего графа.

Решение для `v1alpha1`:

- вложенные `loop_group` запрещены валидатором;
- JSON Schema использует отдельный тип child node без `loop_group` и approval;
- runtime повторно проверяет запрет даже для программно созданного Workflow;
- runtime отклоняет коллизию child ID с существующим состоянием;
- path-based namespace отложен до отдельного изменения контракта.

## 4. Семантика until

`until` теперь проверяется только для child node со статусом `completed`. `skipped`, `failed`, `errored`, `timed_out`, `cancelled` и `blocked` не завершают цикл, даже если поле `exit_code` содержит ноль по умолчанию.

## Регрессии

Добавлены проверки:

1. concurrent stdout/stderr под общим output limit с `-race`;
2. timeout в `before_node`, `on_failure`, `after_node`, `before_complete`;
3. cancellation во время hook;
4. runtime-защита от nested loops без повреждения top-level state;
5. статический запрет nested loops;
6. skipped until-node не завершает цикл.
