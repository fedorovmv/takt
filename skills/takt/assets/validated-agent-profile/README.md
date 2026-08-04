# Стартовый профиль Takt

1. Замените `provider` и `id` моделей в `.takt/config.yaml`.
2. Выберите установленный assistant: `pi` либо `opencode`. Для Pi настройте `project_trust`; для OpenCode при необходимости измените `agent`.
3. Проверьте оба workflow:

```bash
takt validate .takt/workflows/basic.yaml --config .takt/config.yaml --workspace . --json
takt validate .takt/workflows/validated.yaml --config .takt/config.yaml --workspace . --json
takt validate .takt/workflows/opencode.yaml --config .takt/config.yaml --workspace . --json
```

4. Запустите проверяемый профиль:

```bash
takt run .takt/workflows/validated.yaml \
  --config .takt/config.yaml \
  --workspace . \
  --input request.md \
  --json
```

Шаблон `replace-me` проходит структурную проверку, но для реального запуска нужны существующие provider/model и доступный Pi либо OpenCode.
