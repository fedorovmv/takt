# Backlog Takt v0.2

Задачи расположены в рекомендуемом порядке. Каждая задача должна завершаться тестами и обновлением соответствующей спецификации.

## Текущее состояние после v0.1.4-alpha

Стабилизационные части TAKT-001, TAKT-002 и TAKT-004 реализованы в объёме текущего локального runtime: классификация execution errors, fingerprints, timeout, output limit и регрессии parent `loop_group`. Следующая обязательная задача перед реальным adapter — выделить fake-assistant contract suite как отдельный исполнимый набор тестов. `takt cancel`, capability negotiation и строгий template renderer остаются открытыми.

## TAKT-001. Типизированные ошибки

**Цель:** заменить произвольные `fmt.Errorf` на нормализованные ошибки runtime.

**Результат:**

- `Error.Code`, `Message`, `RunID`, `NodeID`, `Attempt`, `Cause`;
- коды из `09-runtime-semantics.md` и `10-assistant-adapter-spec.md`;
- JSON CLI возвращает структурированную ошибку;
- stderr остаётся удобным для человека.

**Приёмка:** unit tests для adapter-not-found, process-start-failed, node-attempts-exhausted и loop-exhausted.

## TAKT-002. Fingerprints запуска

**Цель:** сделать resume воспроизводимым.

**Результат:** SHA-256 workflow, config и разрешённых Markdown-команд сохраняется в RunState.

**Приёмка:** изменённый workflow блокирует resume с понятной ошибкой; предусмотрен явный override для разработки.

## TAKT-003. Строгий template renderer

**Цель:** исключить незаметные ошибки переменных.

**Результат:** неизвестная переменная вызывает ошибку; предусмотрен синтаксис необязательного значения либо функция default.

**Приёмка:** тесты известных, неизвестных и отсутствующих optional values.

## TAKT-004. Timeout и output limit process adapter

**Цель:** ограничить зависшие и чрезмерно шумные процессы.

**Результат:** конфигурация timeout и max output bytes, корректная отмена context, отдельные коды ошибок.

**Приёмка:** fake process проверяет timeout, cancellation и превышение лимита.

## TAKT-005. Команда cancel

**Цель:** управляемо завершать Run.

**Результат:** `takt cancel <run-id>`; идемпотентность; событие `run.cancelled`.

**Приёмка:** отмена running/waiting Run и повторная отмена.

## TAKT-006. Capability contract

**Цель:** проверять совместимость workflow и assistant до запуска.

**Результат:** типизированные capabilities; `requires` на уровне узла; проверка config и adapter discovery.

**Приёмка:** workflow с `session_resume` отклоняется для неподдерживающего adapter.

## TAKT-007. Специализированный Pi/OpenCode adapter

**Цель:** проверить реальное выполнение агентного узла.

**Результат:** один adapter по `10-assistant-adapter-spec.md`.

**Приёмка:** fake integration suite и opt-in smoke test с реальным бинарником.

## TAKT-008. Session resume

**Цель:** сравнивать fresh и продолженную сессию.

**Результат:** сохранение Session ID; явное поле `resumed`; ошибка при неуспешном resume без тихого fallback.

**Приёмка:** fresh, resume success и resume failure.

## TAKT-009. Route DSL end-to-end

**Цель:** заменить mock в основном примере.

**Результат:** agent → validator → feedback → retry → success → approval.

**Приёмка:** минимум один тест требует двух попыток; success определяется только валидатором.

## TAKT-010. Нормализованные diagnostics

**Цель:** давать агенту компактную и стабильную обратную связь.

**Результат:** JSON diagnostics преобразуются в общий формат code/path/line/message.

**Приёмка:** одинаковые ошибки не раздувают feedback; хранится fingerprint ошибки.

## TAKT-011. Изоляция loop group

**Цель:** устранить конфликт дочерних NodeState.

**Результат:** отдельная структура iteration state; child ID локальны loop group.

**Приёмка:** два loop group используют одинаковые child ID без конфликта.

## TAKT-012. Structured outputs

**Цель:** передавать данные между узлами без разбора свободного текста.

**Результат:** JSON output и JSON Schema validation.

**Приёмка:** valid output, malformed JSON и schema mismatch.

## TAKT-013. Go workflow

**Цель:** подтвердить универсальность на кодовой задаче.

**Приёмка:** workflow добавляется без изменения runtime; `go test` управляет повтором.

## TAKT-014. Document workflow

**Цель:** подтвердить универсальность на не-кодовой задаче.

**Приёмка:** draft → approval comment → revise → artifact без изменения runtime.

## TAKT-015. Метрики eval

**Цель:** сравнивать стратегии.

**Результат:** export Run metrics по `13-evaluation-plan.md`.

**Приёмка:** отчёт сравнивает минимум fresh и resume на одном наборе задач.
