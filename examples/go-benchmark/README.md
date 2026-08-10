# Go stabilization benchmark

Этот opt-in benchmark сравнивает один прямой coding-agent вызов с bounded feedback repair на пяти небольших Go-задачах. Задачи основаны на фактических классах дефектов Takt, но являются `production-shaped`, а не обезличенными production-данными.

## Что проверяется

- `baseline-direct`: один fresh turn, затем независимый validator;
- `feedback-repair`: до трёх попыток с deterministic diagnostics и exact resume;
- validator разрешает менять только production `.go` целевого package, запрещает изменения тестов и запускает gofmt, test, race и vet;
- успех учитывается только при `quality_node_status=completed && valid=true`.

Pi использует `aihub/Qwen/Qwen3.6-27B`. OpenCode использует запрошенную CLI model `aihub-sbt/Qwen/Qwen3.6-27B`; provider-side routing не считается наблюдаемым, если NDJSON не публикует его отдельно.

## Запуск

Требуются собранный `bin/takt`, установленные и авторизованные Pi/OpenCode и доступ к модели.

Сначала один повтор:

```bash
make build
TAKT_BENCH_HOST=pi TAKT_REPEAT=1 ./examples/go-benchmark/run.sh
TAKT_BENCH_HOST=opencode TAKT_REPEAT=1 ./examples/go-benchmark/run.sh
```

После успешного measurement smoke:

```bash
TAKT_BENCH_HOST=all TAKT_REPEAT=3 ./examples/go-benchmark/run.sh
```

`TAKT_BENCH_HOST` принимает `pi`, `opencode` или `all`. Результаты пишутся в `examples/go-benchmark/.takt/evals/<host>`; путь можно заменить через `TAKT_BENCH_OUTPUT`.

OpenCode `auto_approve` включён явно и только для копии доверенного benchmark workspace. Live output, Session ID и credentials не коммитятся. Невалидное решение модели является результатом benchmark; ошибка подготовки workspace, запуска host или validation envelope — ошибкой измерительного контура.
