# Flow gate matrix tiering

## Goal

Сохранить полное покрытие outcome-gated `feature-development`, но не платить
стоимостью отдельного Takt/E2E запуска за каждую комбинацию формы artifact или
ошибки parser-а.

## Contract

- `require-artifacts` принимает один или несколько путей и пропускает только
  non-empty regular non-symlink files;
- `require-verdict` принимает artifact и опциональный label, fail-closed
  проверяет NUL/ровно одну control line и печатает только `PASS|REPAIR|BLOCKED`;
- workflow YAML вызывает эти tools, сохраняя прежние ветвления и artifact
  names;
- профильный Go contract test держит полную missing/empty/directory и
  verdict-parser матрицы;
- full-flow E2E оставляет representative success/branch/failure cases, а не
  дублирует каждую детерминированную форму файла.

## Compatibility

Внешний Workflow/Config API не меняется. Profile assets устанавливаются тем же
`profile.Init` с executable mode для `tools/`; обычные production SCM runs и
live `eval-*` не затрагиваются.

## Verification

Матрица tools выполняется в `internal/profile`, workflow catalog проверяет
вызовы shared tools, representative feature-flow E2E проверяет scheduler и
downstream gates. Полный `tests/e2e` должен остаться зелёным и измеримо
сократиться.
