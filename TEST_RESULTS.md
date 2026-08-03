# Результаты проверки v0.1.4-alpha

Дата проверки: 2026-08-03.

## Базовые проверки

```text
go test ./... -count=1                         PASS
go test -race ./... -count=1                   PASS
go vet ./...                                    PASS
go build ./cmd/takt                             PASS
./scripts/verify.sh                             PASS
```

## Регрессии аудита

```text
allow_failure distinguishes exit/start          PASS
all_done after failed dependency                 PASS
when/trigger_rule inside loop_group              PASS
concurrent stdout/stderr under shared limit      PASS
hook timeout: before_node                        PASS
hook timeout: on_failure                         PASS
hook timeout: after_node                         PASS
hook timeout: before_complete                    PASS
hook cancellation → Node/Run cancelled           PASS
nested loop_group validation/runtime guard       PASS
skipped/failed until-node does not finish loop   PASS
parent loop_group timeout → timed_out             PASS
parent loop_group cancellation → cancelled        PASS
Run cancellation error code is not internal      PASS
```

Race-регрессия process assistant одновременно пишет в stdout и stderr и входит в обычный запуск `go test -race ./...`.

## Сквозные CLI-сценарии

```text
Route DSL mock workflow → waiting approval      PASS
takt answer → completed                         PASS
hook retry workflow                             PASS
JSON success/error envelopes                    PASS
```

## Проверка форматов и состава

```text
JSON Schemas parse as JSON                      PASS
workflow schema forbids nested loop_group       PASS
VERSION/CLI version = 0.1.4-alpha               PASS
MANIFEST.sha256                                 PASS after packaging
```

## Зафиксированные ограничения

- локальный однопользовательский trusted runtime;
- последовательный DAG;
- approval и вложенные `loop_group` внутри `loop_group` запрещены;
- собственный документированный YAML subset;
- fake-assistant contract suite ещё не собран полностью;
- специализированные Pi/OpenCode adapters ещё не реализованы;
- platform-specific cancellation требует проверки на целевых ОС.
