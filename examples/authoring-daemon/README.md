# Authoring и daemon

Проверка строгих ссылок и схемы:

```bash
takt validate workflow.yaml --config config.yaml --workspace . --warnings-as-errors
```

Фоновый запуск:

```bash
takt daemon start --workspace .
takt run workflow.yaml --config config.yaml --workspace . --daemon --json
takt events <run-id> --workspace . --daemon --follow
takt daemon stop --workspace .
```

`${path}` обязателен, `${path?}` допускает отсутствие, `${path:-default}` задаёт fallback. `always_run` выполняет cleanup после terminal-состояния зависимостей и не скрывает ошибку основного графа.
