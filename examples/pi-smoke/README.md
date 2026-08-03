# Pi smoke example

Требует установленный `pi`, настроенную авторизацию и модель, доступную под указанными `provider`/`id`.

```bash
takt run examples/pi-smoke/workflow.yaml \
  --config examples/pi-smoke/config.yaml \
  --workspace .
```

`project_trust: deny` не загружает проектные `.pi`-настройки и расширения. Для доверенного проекта, которому нужны локальные skills/extensions, укажите `approve`.
