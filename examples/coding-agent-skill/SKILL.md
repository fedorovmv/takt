---
name: takt-runner
description: Запускает уже подготовленные Takt workflows и обрабатывает approval. Для создания и настройки workflow используй canonical skill skills/takt/SKILL.md.
---

# Запуск готового workflow

1. Выполни `takt validate <workflow> --config <config> --workspace <workspace> --json`.
2. Запусти `takt run <workflow> --config <config> --workspace <workspace> --input <request-file> --json`.
3. При статусе `waiting` покажи пользователю `waiting.message`.
4. После ответа продолжи через `takt answer <run-id> <node-id> --workspace <workspace> --value <answer> --json`.
5. Читай результаты из `<workspace>/.takt/runs/<run-id>/artifacts/`.

Этот пример отвечает только за запуск. Полный скилл создания config, workflow, prompts и retry-профилей находится в `skills/takt/`.
