# Go stabilization benchmark

Этот opt-in benchmark сравнивает один прямой coding-agent вызов с bounded feedback repair на пяти небольших Go-задачах. Задачи основаны на фактических классах дефектов Takt, но являются `production-shaped`, а не обезличенными production-данными.

Benchmark отвечает на один вопрос: получает ли workflow изменение, которое проходит независимый Go validator. Он не сравнивает «интеллект» моделей: `feedback-repair` намеренно получает до трёх попыток и проверяет пользу дополнительного цикла `agent → validator → feedback → resume`.

## Что проверяется

- `baseline-direct`: один fresh turn, затем независимый validator;
- `feedback-repair`: до трёх попыток с deterministic diagnostics и exact resume;
- validator разрешает менять только production `.go` целевого package, запрещает изменения тестов и запускает gofmt, test, race и vet;
- успех учитывается только при `quality_node_status=completed && valid=true`.

Pi запрашивает `aihub/Qwen/Qwen3.6-27B`, OpenCode — прямой provider `aihub-sbt/Qwen/Qwen3-Coder-Next`. Provider-side маршрутизация не считается наблюдаемой, пока host events не публикуют её отдельно.

OpenCode запускается с `--pure`, а agent-node задаёт `skills: []`. Внешние OpenCode plugins не загружаются, skill tool запрещён политикой Takt; глобальная пользовательская конфигурация не изменяется.

Стратегии выполняются последовательно в порядке matrix: сначала `baseline-direct`, затем `feedback-repair`. Runner не чередует их и не нормализует результаты по времени или нагрузке провайдера. Отчёт сохраняет сырые outcomes каждого `case × repeat`.

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

`TAKT_BENCH_HOST` принимает `pi`, `opencode` или `all`. По умолчанию результаты пишутся в `${TMPDIR:-/tmp}/takt-go-benchmark/evals/<host>` вне Git-репозитория; путь можно заменить через `TAKT_BENCH_OUTPUT`, но для OpenCode он также должен оставаться вне родительского Git-root.

OpenCode `auto_approve` включён явно и только для копии доверенного benchmark workspace. Live output, Session ID и credentials не коммитятся. Невалидное решение модели является результатом benchmark; ошибка подготовки workspace, запуска host или validation envelope — ошибкой измерительного контура.
