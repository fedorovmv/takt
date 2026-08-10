Benchmark-Package: ./internal/opencodeevents

Исправь обработку OpenCode NDJSON. Usage суммируется только по уникальным `step_finish.part.id`. Событие `error` всегда означает неуспех, даже если поток содержит корректный usage и внешний процесс завершился с кодом 0. Верни содержательную provider diagnostic.

Не изменяй `go.mod`, `*_test.go` и соседние packages. Выполни `go test -count=1 ./internal/opencodeevents`.
