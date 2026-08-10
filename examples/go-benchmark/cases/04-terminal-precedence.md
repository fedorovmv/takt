Benchmark-Package: ./internal/terminal

Исправь классификацию terminal result. `timed_out` и `cancelled` имеют приоритет над output overflow. Обычный overflow и ненулевой exit code должны сохранить прежнюю классификацию.

Не изменяй `go.mod`, `*_test.go` и соседние packages. Выполни `go test -count=1 ./internal/terminal`.
