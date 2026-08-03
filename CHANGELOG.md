# Changelog

## v0.1.1-alpha

- добавлено целевое состояние Takt v0.2;
- добавлена спецификация runtime-семантики;
- добавлен целевой контракт Pi/OpenCode/process adapters;
- добавлены план реализации, backlog и инструкция для кодового агента;
- добавлены JSON Schemas текущего `takt/v1alpha1`;
- добавлена карта документов и источников истины;
- process adapter выставляет переменные `TAKT_*`; `HARNESS_*` временно сохранены для совместимости;
- версия CLI обновлена до `v0.1.1-alpha`.

## v0.1.0-alpha

- базовый Go-runtime;
- Markdown-команды, модели и process/mock assistants;
- DAG, hooks, retries, loop_group и approval pause/resume;
- локальное состояние и журнал событий;
- примеры Route DSL и hook retry.
