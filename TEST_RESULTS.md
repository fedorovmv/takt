# Результаты проверки v0.1.2-alpha

Проверка выполнена в среде Go 1.23.2 после стабилизационного аудита.

## Базовые проверки

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/takt
./scripts/verify.sh
```

Результат:

```text
unit tests: PASS
race detector: PASS
go vet: PASS
build: PASS
verification: PASS
```

## Сквозные CLI-сценарии

Проверены в отдельной временной рабочей директории:

1. Route DSL: `run` → `waiting` → отдельный `answer` → `completed`;
2. hook retry: первая проверка вызывает retry, вторая попытка завершается успешно;
3. отсутствующий assistant binary при `allow_failure: true` завершает Run ошибкой `start`, а не `completed`;
4. неизвестный CLI flag в JSON-режиме возвращает единственный корректный JSON error envelope;
5. изменённый workflow блокирует `answer`, при этом approval остаётся `waiting` и не потребляется.

Все сценарии прошли.

## Контрактные тесты отказов

Добавлены и пройдены тесты:

- ненулевой exit code с `allow_failure`;
- start error с `allow_failure`;
- `all_done` после failed dependency;
- `all_success` skip после failed dependency;
- `when` и `trigger_rule` внутри `loop_group`;
- node timeout и downstream cleanup;
- persistence error propagation;
- state/event revision mismatch;
- concurrent Run lock;
- unsafe Run ID;
- process start classification;
- process timeout;
- output truncation;
- fingerprints Markdown-команд;
- block scalar с пустыми строками и chomp modes;
- JSON mode defaults.

## Покрытие

```bash
go test -cover ./...
```

Наиболее содержательные пакеты:

- `internal/assistant`: 67.8%;
- `internal/runtime`: 66.5%;
- `internal/definition`: 74.0%;
- `internal/config`: 57.9%;
- `internal/yamlmini`: 55.3%;
- `internal/store`: 54.9%;
- `internal/command`: 52.6%;
- `internal/workflow`: 49.5%;
- `cmd/takt`: 18.3%.

## Схемы и документация

- все `schemas/*.json` успешно разбираются JSON parser;
- README links проверены;
- workflow/config schemas включают `timeout` и `max_output_bytes`;
- RunState/Event schemas включают revisions, fingerprints и новые statuses;
- документация отражает trusted local scope и исправления аудита.

## Что пока не проверялось

- реальный Pi и OpenCode;
- session resume конкретного кодового агента;
- native hooks конкретных SDK;
- platform-specific process-group termination вне Unix;
- `takt cancel`;
- server/untrusted scope;
- parallel DAG;
- MCP и Web UI.
