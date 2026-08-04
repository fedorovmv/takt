# Результаты проверки Takt v0.1.23-alpha

Проверено 5 августа 2026 года.

## Среда

```text
Go: go1.23.2 linux/amd64
OS: Linux 6.12.13 x86_64
Исходный архив: takt-v0.1.22-alpha.zip
SHA-256 исходного архива: 4496a3492401917b6e58ca7c2bcc233fb16144fff083cf9ccdefcc6f8c31a6e0
```

Контрольная сумма исходного архива совпала с приложенным файлом `takt-v0.1.22-alpha.sha256`.

## Проверенный контракт среза

- `foreach` принимает inline `items` или внешний YAML/JSON-массив через `items_from.path`;
- изменение внешнего массива меняет workflow fingerprint и блокирует resume старого Run;
- `subworkflow` и `foreach` работают внутри дочернего DAG `loop_group` с тем же scheduler;
- публичный output `foreach` содержит упорядоченный JSON-массив результатов всех итераций;
- CLI скрывает развёрнутые ID, отображает ожидание через ID контейнера и принимает approval по этому ID;
- контейнер задаёт defaults `assistant`, `model` и `session` для дочернего workflow;
- локальные команды ищутся от каталога ребёнка до корня композиции;
- схема и Go-валидатор одинаково трактуют нулевые значения полей контейнера;
- задокументированы рекурсия, предел глубины 16, запрещённые поля и оставшиеся ограничения;
- профиль `code` обновлён до 0.2.1, authoring skill — до 0.5.0.

## Полные проверки рабочего дерева

Фактически завершились успешно:

```text
go vet ./...
go test ./... -count=1                  PASS, real 25.18s
go test -race ./... -count=1            PASS, real 27.89s
make check                               PASS
./scripts/verify.sh                      PASS
```

`make check` последовательно подтвердил:

```text
fake-assistant contract suite: PASS
Pi adapter contract suite: PASS
OpenCode adapter contract suite: PASS
Route DSL end-to-end: PASS
Route DSL evaluation: PASS
Route DSL evaluation isolation: PASS
workflow composition: PASS
Takt authoring skill: PASS
code profile contract: PASS
documentation check: PASS
```

`verify.sh` дополнительно собрал fake binaries и проверил `takt validate` для штатных примеров, включая composition.

## Повтор нестабильных сценариев

После исправлений отдельно выполнены:

```text
go test ./internal/runtime \
  -run TestOpenCodeTimeoutPreservesProviderDiagnostics -count=3
PASS

go test ./internal/assistant \
  -run TestOpenCodeRunPreservesContextPriorityWithRealOverflow -count=3
PASS

go test ./internal/assistant \
  -run 'TestOpenCodeAdapterContract/provider_diagnostics_survive_timeout' -count=3
PASS

go test ./internal/assistant \
  -run 'TestPiAdapterContract/process_exit' -count=20
PASS
```

Для OpenCode timeout-тестов увеличен только запас на запуск тестового процесса и выдачу диагностик. Тестовый provider продолжает работать дольше deadline, поэтому сценарий по-прежнему проверяет timeout, а не обычное завершение. Pi adapter считает `os.ErrClosed` от stderr pipe после `cmd.Wait()` штатным закрытием потока и не превращает успешный `process_exit` в protocol error.

## Прерванные запуски

Один предварительный холодный `go test -race ./... -count=1` был прерван внешним лимитом запуска инструмента до получения результата. Он не учитывается как успешная проверка. Полный повтор той же команды, `make check` и `verify.sh` завершились с кодом 0.

Комбинированная команда с тремя повторными наборами тестов также достигла внешнего лимита после успешного завершения первых двух наборов. Третий набор `Pi process_exit -count=20` был сразу выполнен отдельной командой и прошёл.

## Внешние smoke-тесты

Реальные Pi и OpenCode smoke не запускались: в среде сборки нет пользовательских credentials и provider-конфигурации. Контрактные fake adapters прошли полностью.

## Оставшиеся ограничения

- `foreach` выполняется последовательно; конкурентный режим требует отдельной семантики scheduler;
- `attempts`, `timeout`, hooks, `native_hooks` и `allow_failure` для всей композиционной группы задаются внутри дочернего workflow;
- полный служебный `state.json` сохраняет namespaced ID для resume и диагностики; внешним контрактом является публичная проекция CLI;
- approval и вложенный `loop_group` внутри `loop_group` остаются запрещены;
- `items_from.path` читает статический файл при компиляции; динамический список из output другого узла пока не поддержан.
