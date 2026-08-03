# Результаты проверки v0.1.6-alpha

Дата проверки: 2026-08-04.

## Основной набор

```text
go test ./... -count=1                         PASS
go test -race ./... -count=1                   PASS
go vet ./...                                   PASS
go build ./cmd/takt                            PASS
go build ./cmd/takt-fake-assistant             PASS
./scripts/check-docs.sh                        PASS
./scripts/verify.sh                            PASS
JSON Schemas                                   PASS
README local links                             PASS
VERSION/CLI = 0.1.6-alpha                      PASS
```

## Fake-assistant contract suite

```text
success                                         PASS
exit code                                       PASS
start error                                     PASS
timeout                                         PASS
cancellation                                    PASS
concurrent stdout/stderr                        PASS
malformed result                                PASS
fresh session                                   PASS
resume session                                  PASS
resume rejection                               PASS
protocol output limit                           PASS
runtime fresh → retry → resume                  PASS
```

Проверяется настоящий дочерний бинарник `cmd/takt-fake-assistant`, а не подмена `os/exec`.

## Зафиксированный контракт

- `protocol: takt-assistant/v1alpha1` передаёт request JSON через stdin;
- stdout должен содержать ровно один строгий result JSON;
- неизвестные поля, malformed или truncated result дают `protocol`;
- start, timeout и cancel сохраняют собственную классификацию;
- ненулевой код остаётся `exit`;
- resume требует `resumed: true` и совпадающий Session ID;
- первый вызов `session: resume` без сохранённого ID нормализуется в `fresh`;
- общий budget stdout/stderr защищён от data race;
- текстовый режим process assistant сохранён для совместимости.

## Ограничения

- специализированные Pi/OpenCode adapters ещё не реализованы;
- потоковые события adapter пока не входят в protocol v1alpha1;
- structured result декодируется process adapter, но отдельный типизированный Node output остаётся задачей v0.2;
- реальные smoke tests с Pi/OpenCode должны быть opt-in.
