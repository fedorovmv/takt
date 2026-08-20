# Архив документации

Содержимое этого каталога сохранено для traceability и исторического анализа.
Архив не является текущим пользовательским контрактом.

## Категории

- [`releases/`](releases/) — release notes и implementation slices `v0.1.x`,
  ранее лежавшие в корне `docs/`;
- [`verification/`](verification/) — историческое verification evidence и
  завершённые task reports;
- [`analysis/`](analysis/) — первоначальная мотивация, датированные аудиты и
  рабочие исследования, которые повлияли на решения, но не являются
  спецификацией.

Для текущего поведения используйте [`../../README.md`](../../README.md),
[`../12-document-map.md`](../12-document-map.md), спецификацию и schemas.

`TEST_RESULTS-v0.1.57-2026-08-18.md` — исторический отчёт, а не живой статус
тестов. Актуальные проверки запускаются командами `make check`,
`make check-full` и `./scripts/verify.sh`; их политика описана в
[`../../CONTRIBUTING.md`](../../CONTRIBUTING.md) и
[`../../DEVELOPMENT.md`](../../DEVELOPMENT.md).
