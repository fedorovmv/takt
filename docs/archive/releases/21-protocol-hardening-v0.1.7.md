# Усиление process-протокола в v0.1.7-alpha

## Причина изменения

Аудит `v0.1.6-alpha` выявил неоднозначность между кодом завершения дочернего процесса и `result.exit_code` из JSON envelope. При OS exit code `0` и envelope `exit_code: 7` runtime принимал результат как обычный `exit`, хотя транспорт и envelope противоречили друг другу.

Также contract suite не фиксировал часть уже реализованных строгих проверок, config JSON Schema разрешала `protocol` для `mock`, а decoder не проверял неотрицательные значения `usage`.

## Зафиксированная семантика

Для `protocol: takt-assistant/v1alpha1`:

1. дочерний процесс возвращает ровно один JSON result envelope в stdout;
2. OS exit code и `result.exit_code` обязаны совпадать всегда, включая нулевой код;
3. несовпадение классифицируется как `protocol`, а не `exit`;
4. `status: completed` требует `exit_code: 0`;
5. `status: failed` требует ненулевой `exit_code`;
6. `exit_code` обязателен и не может быть `null`;
7. `usage.input_tokens`, `usage.output_tokens` и `usage.cost` не могут быть отрицательными;
8. request и result не допускают неизвестных полей и дополнительных JSON-значений;
9. при `resume` обязательны `resumed: true` и совпадающий Session ID.

OS exit code считается фактом завершения транспорта, envelope — структурированным описанием того же результата. Ни одна сторона не является авторитетной при расхождении: расхождение означает нарушение протокола.

## Добавленные contract cases

Fake assistant и тесты теперь покрывают:

- неверный `protocol_version`;
- неверный `type`;
- неизвестное поле result;
- неизвестный `status`;
- отсутствующий и `null` `exit_code`;
- `completed` с ненулевым кодом;
- `failed` с нулевым кодом;
- два JSON result envelope;
- OS `0` при envelope nonzero;
- разные ненулевые OS/envelope exit codes;
- отрицательные token usage и cost;
- передачу `metadata` и `native_hooks`;
- два JSON request envelope на stdin fake assistant.

## Согласование схем

`schemas/config.schema.json` теперь запрещает `protocol` для `type: mock`, как и runtime validator.

`schemas/assistant-protocol.schema.json` и Go decoder согласованы по:

- обязательному `exit_code`;
- связи `status` и `exit_code`;
- неотрицательным usage-метрикам;
- запрету неизвестных полей.

## Итог

После `v0.1.7-alpha` специализированный Pi/OpenCode adapter может использовать `takt-assistant/v1alpha1` как строгую границу. Любое изменение семантики OS exit code, envelope или resume требует одновременного обновления:

- `docs/03-specification.md`;
- `docs/10-assistant-adapter-spec.md`;
- JSON Schema;
- fake-assistant contract suite;
- ADR.
