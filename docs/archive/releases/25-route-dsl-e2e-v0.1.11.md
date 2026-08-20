# Route DSL end-to-end и интеграционное покрытие Pi в v0.1.11-alpha

## Цель релиза

Релиз закрывает оставшийся пробел в регрессионном покрытии Pi adapter и запускает следующий вертикальный этап проекта: генерацию Route DSL с обязательной внешней проверкой.

## Интеграционные регрессии Pi

Добавлены fake-Pi сценарии `timeout-overflow` и `cancel-overflow`. Они создают реальное переполнение общего stdout/stderr budget внутри `Pi.Run`.

Проверка разделена на два уровня:

1. `internal/assistant` запускает настоящий дочерний fake-Pi процесс и проверяет, что результат одновременно содержит:
   - `KindTimedOut` либо `KindCancelled`;
   - `Result.Truncated = true`.
2. `internal/runtime` проверяет сохранение этой пары в состоянии узла:
   - статус `timed_out` либо `cancelled`;
   - соответствующий `error_code`;
   - `output_truncated = true`.

Такое разделение фиксирует обе границы: классификацию Pi adapter и перенос результата в `NodeState`.

## Route DSL end-to-end

Добавлен пример `examples/route-dsl-e2e` и скрипт `scripts/test-route-dsl-e2e.sh`.

Сценарий `Pi → validator → feedback → retry/resume → artifacts → approval`:

```text
техническое задание
→ Pi создаёт route.yaml
→ validator отклоняет первую версию
→ diagnostics сохраняются в feedback
→ повтор продолжается с тем же Session ID
→ Pi исправляет route.yaml
→ validator подтверждает результат
→ route.yaml и validation.json сохраняются как artifacts
→ approval
→ takt answer
→ completed
```

Первая попытка намеренно создаёт невалидный маршрут. Вторая попытка считается успешной только при одновременном выполнении двух условий:

- Pi продолжил сохранённую сессию;
- prompt содержит диагностику `ROUTE_INVALID` предыдущей проверки.

Это подтверждает, что успех определяется внешним валидатором, а не текстом агента или фактом создания файла.

## Граница тестового стенда

`route-tool` в примере является минимальным детерминированным стендом. Он проверяет управляющую семантику Takt, но не заменяет штатный валидатор пользовательского Route DSL.

Следующий производственный срез:

- подключить штатный `route-tool`;
- прогнать не менее 10 реальных технических заданий;
- нормализовать diagnostics;
- собирать количество попыток, длительность, usage, ошибки проверки и ручные исправления.

## Проверки

```text
go test ./...
go test -race ./...
go vet ./...
./scripts/test-fake-assistant.sh
./scripts/test-pi-adapter.sh
./scripts/test-route-dsl-e2e.sh
./scripts/check-docs.sh
./scripts/verify.sh
```
