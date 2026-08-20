# OpenCode adapter v0.1.19

## Назначение

Takt получил второй специализированный coding-agent adapter: `type: opencode`. Он использует неинтерактивный `opencode run --format json`, оставляя OpenCode ответственность за внутренний tool loop, файлы, shell, MCP и собственные agents.

## Контракт

Takt управляет `--dir`, `--model`, `--agent`, `--variant`, `--session`, `--auto` и форматом JSON. Prompt передаётся через stdin. Stdout содержит NDJSON events, stderr — диагностику.

Adapter:

- проверяет версию через `opencode --version`;
- собирает итоговый текст из `text` events;
- суммирует input/output tokens и cost по уникальным `step_finish`;
- сохраняет Session ID и проверяет resume;
- считает `error` event отказом даже при OS exit 0;
- сохраняет requested/resolved model и assistant version;
- применяет общий stdout/stderr limit и сохраняет приоритет timeout/cancel над overflow.

Начиная с v0.1.20 adapter также сохраняет доступные provider diagnostics при timeout/cancellation. Сообщения о retry из stderr и ошибки соединения из JSON `error` events попадают в raw streams, logical output и текст ошибки узла, но execution kind остаётся `timed_out` или `cancelled`.

## Конфигурация

```yaml
assistants:
  opencode:
    type: opencode
    binary: opencode
    agent: build
    auto_approve: false
    max_output_bytes: 10485760
```

`auto_approve` включает OpenCode `--auto` и допустим только в доверенной рабочей директории.

## Проверки

`scripts/test-opencode-adapter.sh` покрывает request mapping, fresh/resume, mismatch, warning stderr, malformed events, error event с OS exit 0, process exit, missing/negative usage, timeout, provider diagnostics при timeout, cancellation, output overflow, context priority и reserved flags. Runtime test подтверждает retry с продолжением Session ID, накоплением usage и сохранением provider diagnostics без потери per-attempt execution records.

Опциональная проверка реального CLI включается через `TAKT_OPENCODE_SMOKE=1` вместе с `TAKT_OPENCODE_SMOKE_PROVIDER` и `TAKT_OPENCODE_SMOKE_MODEL`.
