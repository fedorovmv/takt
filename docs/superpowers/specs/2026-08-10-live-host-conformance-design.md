# Live Host Conformance: дизайн стабилизации

## Статус и цель

Дизайн подтверждён пользователем 2026-08-10. Цель — получить проверяемые live-свидетельства для установленных Pi `0.83.0` и OpenCode `1.18.14`, не расширяя runtime и не повышая bundled integrations из `guarded` в `strict` без доказанного полного host contract.

## Подтверждённое состояние

- `make check` проходит на `master` в Linux/macOS CI.
- Детерминированные Pi/OpenCode fixtures и TypeScript compiler smoke проходят.
- Реальные CLI доступны локально: Pi `0.83.0`, OpenCode `1.18.14`.
- Pi host extension типизирован против Pi `0.73.1`; текущая установленная версия новее этого контракта.
- `takt compatibility check --live` видит оба CLI, но честно возвращает `warning`; bundled host integrations остаются `guarded` и `live_verified: false`.
- Доступны модели для bounded smoke: Pi `aihub/Qwen/Qwen3.6-27B`, OpenCode `opencode/deepseek-v4-flash-free`.

## Выбранный подход

Используется evidence-first подход:

1. существующие opt-in adapter smoke tests запускаются на точных установленных версиях;
2. отдельно проверяются fresh/resume, version capture и terminal result;
3. host-control проверяется на фактически доступных hook surfaces: загрузка extension/plugin, command/input interception, tool blocking и recovery;
4. каждый воспроизведённый дефект сначала получает минимальный regression test, затем исправляется в общей точке контракта;
5. результаты фиксируются с точными версиями, командами и границами доказательства.

Не выбираются:

- новый универсальный live-test framework до появления второго независимого потребителя;
- откат локальных CLI к старым версиям вместо проверки реально используемой среды;
- объявление `strict` по одному успешному prompt smoke;
- изменение scheduler/runtime без воспроизведённого нарушения его контракта.

## Контур проверки

### Session adapters

Для Pi и OpenCode выполняется короткий prompt с точным ожидаемым маркером. Проверка обязана подтвердить:

- обнаруженную версию CLI;
- успешный fresh session;
- terminal output с ожидаемым маркером;
- resume того же Session ID отдельной попыткой;
- отсутствие подмены failed resume новым fresh session;
- корректный OS/adapter terminal status.

### Host control

Проверяются только реально наблюдаемые гарантии bundled `guarded` integrations:

- extension/plugin загружается целевым host без API/type error;
- `/takt` или его command marker не попадает в основную модель;
- последующий input маршрутизируется в managed Takt session;
- mutating/unknown tool блокируется до выполнения;
- потеря daemon при активном cached session оставляет интерфейс fail-closed;
- после восстановления daemon durable host session снова находится.

Completion blocking не считается доказанным для Pi/OpenCode, пока конкретный host не предоставляет и не проходит соответствующий fail-closed hook. Это сохраняет enforcement `guarded`.

## Ошибки и безопасность

- Live tests opt-in и не входят в обычный `make check`, поскольку требуют credentials, модели и внешнего host process.
- Prompt минимален и не разрешает файловые изменения; рабочая директория для adapter smoke временная.
- Секреты, resolved provider config и raw credentials не сохраняются в репозитории или отчётах.
- Raw diagnostics допускаются только после проверки, что они не содержат секретов.
- Невозможность автоматизировать конкретный TUI hook фиксируется как непроверенная граница, а не как PASS.

## Результаты и критерии готовности

Срез готов, когда:

1. Pi и OpenCode adapter smoke прошли либо выявили воспроизводимый дефект с regression test;
2. для каждой host capability указан `PASS`, `FAIL` или `NOT VERIFIED` на точной версии;
3. compatibility metadata не преувеличивает доказанный уровень;
4. документация отличает session-adapter evidence от host-control evidence;
5. `make check` и `scripts/verify.sh` проходят после любых изменений;
6. в git отсутствуют credentials, временные host configs и live session artifacts.

## Условия остановки

Остановить реализацию и вернуть решение на проектирование, если live host требует:

- нового публичного Workflow/Config поля;
- изменения durable Run/state semantics;
- ослабления fail-closed policy;
- глобальной установки или мутации пользовательской host-конфигурации вместо изолированного smoke;
- заявления `strict` без всех пяти capability guarantees.
