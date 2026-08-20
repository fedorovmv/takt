# Исправления по аудиту в v0.1.2-alpha

## Назначение

Документ связывает находки аудита `v0.1.1-alpha` с изменениями кода и тестами. Он описывает стабилизационный срез перед реализацией Pi/OpenCode adapter.

## Закрытые P1-находки

### `allow_failure` скрывал ошибки запуска и отмены

Исправлено:

- execution error разделён на `exit`, `start`, `timed_out`, `cancelled`, `protocol`, `internal`;
- `allow_failure` применяется только к `exit`;
- отсутствующий бинарник становится `errored/start`;
- timeout становится `timed_out`;
- cancellation становится `cancelled`.

Контрактные тесты: `TestAllowFailureOnlyAllowsNonZeroExit`, `TestNodeTimeoutAndAllDoneCleanup`, process adapter tests.

### `all_done` не выполнялся после failed dependency

Исправлено:

- scheduler не завершает Run сразу после failed/errored node;
- terminal failure остаётся в графе как результат узла;
- `all_done` выполняется;
- `all_success` и недостижимые ветви становятся `skipped`;
- итоговый статус Run вычисляется после завершения графа.

Контрактный тест: `TestAllDoneRunsAfterFailedDependency`.

### `loop_group` игнорировал `when` и `trigger_rule`

Исправлено:

- корневой workflow и дочерний DAG используют один `executeGraph`;
- `when`, `depends_on`, `trigger_rule`, hooks и классификация ошибок работают одинаково;
- дочерние состояния копируются в `LoopPrevious` после итерации.

Контрактный тест: `TestLoopGroupUsesWhenAndTriggerRules`.

### `answer` потреблял approval до проверки определений

Исправлено:

- `answer` получает lock Run;
- загружает и валидирует workflow/config/commands;
- сравнивает SHA-256 fingerprints;
- только после этого сохраняет ответ;
- добавлена команда `takt resume` для продолжения после временной ошибки.

Контрактный тест: `TestAnswerValidatesDefinitionsBeforeConsumingApproval`.

### Block scalar терял пустые строки

Исправлено:

- tokenizer сохраняет исходные строки;
- поддерживаются literal/folded block scalar и chomp modes;
- `#`, `${...}`, `:` и пустые строки внутри prompt не меняются.

Контрактный тест: `TestBlockScalarPreservesBlankLinesAndSpecialText`.

### Ошибки persistence и event log игнорировались

Исправлено:

- runtime использует обязательный `Store.Commit`;
- событие и состояние получают одну revision;
- ошибки возвращаются вызывающему коду;
- `Load` обнаруживает разницу ревизий;
- локальная блокировка предотвращает одновременный `answer/resume`.

Контрактные тесты: `TestPersistenceErrorsAreReturned`, store revision/lock tests.

## Закрытые P2-находки

- успешные и ошибочные ответы CLI используют envelope `ok/result` и `ok/error`;
- FlagSet не печатает дополнительный не-JSON текст;
- `command run` использует project, local и user command scope;
- добавлены тесты отказов scheduler, adapter, persistence, YAML и CLI.

## Дополнительное усиление

- поле node `timeout`;
- `assistant.max_output_bytes`;
- признак `output_truncated`;
- завершение Unix process group при timeout/cancel;
- безопасная проверка Run ID;
- JSON Schema обновлены под текущий контракт.

## Оставшиеся ограничения

- current lock не восстанавливает stale lock автоматически;
- нет sandbox и untrusted/server scope;
- нет `takt cancel`;
- template renderer пока оставляет неизвестную переменную без ошибки;
- assistant protocol и session resume ещё не проверены на Pi/OpenCode;
- полная YAML 1.2 не поддерживается.
