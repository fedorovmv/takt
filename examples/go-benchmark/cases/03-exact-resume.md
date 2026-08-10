Benchmark-Package: ./internal/session

Исправь exact resume contract. При resume возвращённый Session ID обязан совпадать с запрошенным. Несовпадение или отсутствие ID является ошибкой; fresh session принимает новый стабильный ID.

Не изменяй `go.mod`, `*_test.go` и соседние packages. Выполни `go test -count=1 ./internal/session`.
