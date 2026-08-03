# Результаты проверки v0.1.7-alpha

## Основные проверки

| Проверка | Результат |
|---|---|
| `go test ./... -count=1` | PASS |
| `go test -race ./... -count=1` | PASS |
| `go vet ./...` | PASS |
| `go build ./cmd/takt` | PASS |
| `go build ./cmd/takt-fake-assistant` | PASS |
| `./scripts/test-fake-assistant.sh` | PASS |
| `./scripts/check-docs.sh` | PASS |
| `./scripts/verify.sh` | PASS |
| `make check` | PASS |
| VERSION/CLI = `0.1.7-alpha` | PASS |

## Protocol contract

Проверены положительные и отрицательные сценарии `takt-assistant/v1alpha1`:

- success и согласованный ненулевой exit;
- ошибка запуска;
- timeout и cancellation;
- параллельный stdout/stderr под общим race-safe лимитом;
- malformed и два JSON result envelope;
- неверные `protocol_version`, `type`, `status` и неизвестные поля;
- отсутствующий и `null` `exit_code`;
- несовместимые `completed/nonzero` и `failed/zero`;
- OS exit `0` при envelope nonzero;
- разные ненулевые OS/envelope exit codes;
- отрицательные `input_tokens`, `output_tokens` и `cost`;
- fresh, resume и отказ resume;
- передача metadata и native hooks;
- отклонение второго JSON request fake assistant;
- сквозной `fresh → retry → resume` через runtime.

## Согласование схем

- `config.schema.json` запрещает `protocol` для `type: mock`, как runtime validator;
- `assistant-protocol.schema.json` и Go decoder согласованы по обязательному `exit_code`, status/exit и неотрицательному usage;
- все JSON Schemas синтаксически корректны;
- защита документации от отката к `v0.1.1` проходит.

## Сквозные сценарии

- workflow validation для Route DSL и hook-retry — PASS;
- approval → `takt answer` → completed — PASS;
- единый JSON error envelope CLI — PASS;
- целостность `MANIFEST.sha256` в релизном архиве — PASS.

## Итог

Блокирующие замечания аудита `v0.1.6-alpha` закрыты. Следующий этап — специализированный Pi либо OpenCode adapter, обязанный пройти тот же contract suite.
