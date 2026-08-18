# Test suite tiering

## Goal

Разделить ежедневный `make check` и полный release/diagnostic прогон так,
чтобы обычная проверка не запускала один и тот же process-heavy E2E набор
трижды (обычный, journeys и race), а полный набор оставался явно доступным.

## Contract

- `make test` и `make race` проверяют Go-пакеты без `tests/e2e`;
- `make test-all` и `make race-all` проверяют все Go-пакеты, включая E2E;
- `make e2e` запускает полный E2E один раз, `make e2e-race` — его race-вариант;
- `make journeys` остаётся отдельным user-facing smoke gate;
- `make check` выполняет format, vet, core tests, обычный E2E, build и
  TypeScript smoke;
- `make check-full` добавляет полный обычный и race-пакетный прогон;
- live `eval-*` цели не входят ни в один автоматический check.

## Compatibility

Старые target names сохраняются. Изменяется только состав `test`, `race` и
`check`; полный прежний контур вызывается через `check-full` или явные
`test-all`/`race-all`. `scripts/verify.sh` использует тот же полный контур,
чтобы release verification не зависела от сокращённого developer check.

## Verification

Makefile smoke проверяет, что core-команды не включают E2E, полный контур
включает его, а `make check` не вызывает `journeys` вторично. Go-код и
пользовательский runtime-контракт не меняются.
