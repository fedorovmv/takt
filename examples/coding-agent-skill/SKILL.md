---
name: takt-runner
description: Запускает готовые и динамические Takt workflows из основной сессии кодинг-агента, показывает preview, наблюдает Run и обрабатывает approval/steering. Для создания и настройки workflow используй canonical skill skills/takt/SKILL.md.
---

# Запуск готового workflow

1. Выполни `takt validate <workflow> --config <config> --workspace <workspace> --json`.
2. Запусти `takt run <workflow> --config <config> --workspace <workspace> --input <request-file> --json`.
3. При статусе `waiting` покажи пользователю `waiting.message`.
4. После ответа продолжи через `takt answer <run-id> <node-id> --workspace <workspace> --value <answer> --json`.
5. Читай результаты из `<workspace>/.takt/runs/<run-id>/artifacts/`.

Этот пример отвечает только за запуск. Полный скилл создания config, workflow, prompts и retry-профилей находится в `skills/takt/`.


## Динамический процесс

1. Для сложной задачи вызови MCP-инструмент `takt.plan` или CLI `takt plan <goal>`.
2. Покажи пользователю решение `existing|planned`, фазы, бюджеты и требуемое подтверждение.
3. После подтверждения вызови `takt.execute` и сохрани `plan_id` вместе с `run_id`.
4. Читай `takt.plan.get` и `takt.run.events`; показывай краткие изменения фаз, usage и новые артефакты.
5. Уточнение пользователя передавай через `takt.run.steer`. Оно применяется на ближайшем checkpoint и не изменяет завершённую историю.
6. После успешного завершения предложи `takt.plan.promote`, если процесс полезен повторно.

Не выполняй все внешние узлы основной сессией, пока она блокирующе ждёт Run. Takt запускает отдельные Pi/OpenCode процессы либо выдаёт задания отдельному external worker; основная сессия наблюдает и управляет.
