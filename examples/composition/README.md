# Subworkflow, foreach и публичное состояние

Пример подключает один переиспользуемый workflow напрямую, затем выполняет его для элементов из внешнего YAML-файла.

```bash
takt validate examples/composition/workflow.yaml \
  --config examples/composition/config.yaml \
  --workspace examples/composition

takt run examples/composition/workflow.yaml \
  --config examples/composition/config.yaml \
  --workspace examples/composition
```

`foreach.items_from.path` вычисляется относительно содержащего workflow. Содержимое файла входит в fingerprint определения, поэтому его изменение блокирует resume старого Run.

Публичные узлы `prepare` и `batch` остаются зависимостями и ключами состояния. Внутренние развёрнутые узлы сохраняются на диске для resume, но не выводятся через CLI. `batch.output` содержит JSON-массив результатов в порядке итераций: `["second","third"]`.
