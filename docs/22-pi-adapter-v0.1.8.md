# Pi RPC adapter в v0.1.8-alpha

## Назначение

Контракт реализован по RPC/CLI документации `@earendil-works/pi-coding-agent` 0.83.0. Реальный smoke test остаётся opt-in, поскольку требует установленного бинарника, авторизации и доступной модели.

Релиз добавляет первый специализированный coding-agent adapter на основе `pi --mode rpc`. Он интегрирует Takt с `earendil-works/pi` через официальный RPC mode и сохраняет архитектурную границу: Pi управляет моделью, инструментами, файлами, shell, skills, extensions и историей сессии; Takt управляет DAG, проверками, retry и approval.

## Конфигурация

```yaml
assistants:
  pi:
    type: pi
    binary: pi
    args: [--offline]
    session_dir: .takt/pi-sessions
    project_trust: deny
    max_output_bytes: 10485760
```

Adapter сам формирует зарезервированные параметры `--mode rpc`, `--provider`, `--model`, `--thinking`, `--session`, `--session-dir` и project trust. Их нельзя переопределять через `args`.

## Выполнение попытки

1. Проверяется `pi --version`.
2. Запускается RPC-процесс в workspace узла.
3. `get_state` фиксирует Session ID и модель.
4. `prompt` получает полное задание через stdin JSONL.
5. Adapter ждёт `agent_end`.
6. Итоговый текст, статистика и финальное состояние читаются отдельными RPC-командами.
7. Stdin закрывается, после чего Pi штатно завершает RPC mode.

## Сессии

- `fresh` не передаёт старый Session ID;
- `resume` передаёт `--session <id>`;
- начальный и финальный `get_state` обязаны вернуть запрошенный ID;
- несовпадение классифицируется как `protocol`;
- тихий переход на новую сессию запрещён.

## Ошибки и ограничения

- отсутствующий бинарник — `start`;
- ненулевое завершение Pi — `exit`;
- deadline — `timed_out`;
- внешний cancel — `cancelled`;
- malformed JSONL, потеря Session ID, превышение output limit и интерактивный extension UI — `protocol`;
- notifications и UI-события без ответа допускаются;
- model params кроме thinking не переводятся в CLI-флаги, но доступны extensions через `TAKT_MODEL_PARAMS_JSON`;
- workflow runtime пока не формирует `Request.Metadata`; при заполненном поле adapter передаёт его через `TAKT_METADATA_JSON`.

## Проверки

Добавлены:

- `cmd/takt-fake-pi`;
- `TestPiAdapterContract`;
- race-проверка concurrent stdout/stderr;
- start/exit/timeout/cancel/output-limit/malformed cases;
- provider/model/thinking/env/native hooks mapping;
- fresh, resume и resume mismatch;
- prompt rejection и agent-level failure;
- runtime `fresh → retry → resume`;
- opt-in `TestPiAdapterOptInSmoke`.

Запуск контрактов:

```bash
./scripts/test-pi-adapter.sh
```

Реальный smoke:

```bash
TAKT_PI_SMOKE=1 \
TAKT_PI_SMOKE_PROVIDER=openai \
TAKT_PI_SMOKE_MODEL=<model-id> \
./scripts/test-pi-adapter.sh
```

## Следующий этап

Подключить Pi к Route DSL workflow, использовать настоящий validator как обязательный completion gate и проверить цикл `agent → diagnostics → retry/resume → success → approval`.
