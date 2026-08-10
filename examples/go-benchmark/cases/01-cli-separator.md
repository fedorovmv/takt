Benchmark-Package: ./internal/cliargs

Исправь `Inject`: служебные аргументы должны добавляться перед первым `--`, а пользовательская часть после `--` должна сохраниться без изменений. Функция не должна мутировать входные slices.

Не изменяй `go.mod`, `*_test.go` и соседние packages. Выполни `go test -count=1 ./internal/cliargs`.
