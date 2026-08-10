Benchmark-Package: ./internal/runstore

Исправь завершение Run: ошибка durable persistence должна возвращаться вызывающему коду без маскировки. Успешный commit сохраняет состояние `completed`.

Не изменяй `go.mod`, `*_test.go` и соседние packages. Выполни `go test -count=1 ./internal/runstore`.
