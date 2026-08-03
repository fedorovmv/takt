# Результаты проверки v0.1.8-alpha

Дата проверки: 4 августа 2026 года.

## Автоматические проверки

| Проверка | Результат |
|---|---|
| `go test ./... -count=1` | PASS |
| `go test -race ./... -count=1` | PASS |
| `go vet ./...` | PASS |
| `go build ./cmd/takt` | PASS |
| `go build ./cmd/takt-fake-assistant` | PASS |
| `go build ./cmd/takt-fake-pi` | PASS |
| `scripts/test-fake-assistant.sh` | PASS |
| `scripts/test-pi-adapter.sh` | PASS |
| `scripts/check-docs.sh` | PASS |
| `scripts/verify.sh` | PASS |
| `make check` | PASS |
| JSON syntax всех schemas | PASS |
| локальные Markdown-ссылки | PASS |
| `VERSION` и CLI = `0.1.8-alpha` | PASS |

## Pi adapter contract suite

Проверены реальные дочерние процессы через `cmd/takt-fake-pi`:

- доступность и version probe;
- отображение provider, model и thinking level в CLI Pi;
- рабочий каталог, prompt, env, metadata и native hooks;
- успешное выполнение и нормализация итогового текста;
- usage и resolved model;
- fresh session без передачи устаревшего ID;
- resume с подтверждением фактического Session ID;
- resume mismatch;
- ошибка запуска бинарника;
- ненулевой код завершения Pi;
- timeout и cancellation;
- одновременный stdout/stderr под `-race`;
- общий output limit;
- большая JSONL-запись без перевода строки не обходит output limit;
- malformed JSONL и два JSON-объекта в одной записи;
- отказ prompt preflight;
- agent-level failure;
- неподдерживаемый интерактивный extension UI;
- запрет переопределения зарезервированных CLI-флагов;
- runtime `fresh → retry → resume`.

## Совместимость конфигурации

Runtime validator и `schemas/config.schema.json` согласованы для:

- `type: mock`;
- `type: process`;
- `type: pi`;
- Pi-полей `binary`, `args`, `session_dir`, `project_trust`;
- запрета `argv` и `protocol` для Pi.

## Реальный Pi smoke

`TestPiAdapterOptInSmoke` реализован, но в среде сборки не запускался: бинарник `pi`, учётные данные и доступная модель отсутствуют. Запуск:

```bash
TAKT_PI_SMOKE=1 \
TAKT_PI_SMOKE_PROVIDER=<provider> \
TAKT_PI_SMOKE_MODEL=<model-id> \
./scripts/test-pi-adapter.sh
```

## Итог

Специализированный Pi RPC adapter реализован и защищён contract suite. Следующая проверка — opt-in smoke с настоящим Pi, затем Route DSL end-to-end с реальным валидатором, feedback и retry/resume.
