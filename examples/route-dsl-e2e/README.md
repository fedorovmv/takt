# Route DSL end-to-end

Сценарий проверяет основной контур Takt:

```text
Pi → route-tool → diagnostics → retry/resume → validation → artifacts → approval
```

Для реального запуска настройте `provider`, `id` и авторизацию Pi в `config.yaml`, затем выполните:

```bash
takt run workflow.yaml \
  --config config.yaml \
  --workspace . \
  --input specification.md \
  --json
```

После статуса `waiting` подтвердите результат:

```bash
takt answer <run-id> approve-result --workspace . --value approved --json
```

`route-tool` в примере — минимальный проверочный стенд. В рабочем проекте замените его штатным валидатором Route DSL, сохранив контракт: код `0` означает успешную проверку, ненулевой код и stdout/stderr содержат диагностику для следующей попытки.
