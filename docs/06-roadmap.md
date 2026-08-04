# План развития

Этот документ показывает направления после ближайшего этапа. Обязательный объём v0.2 и порядок реализации определены в `08-target-v0.2.md` и `11-implementation-plan.md`.

## Этап 1. Проверка модели

- заменить минимальный Route DSL validator штатным `route-tool` и прогнать eval-набор;
- прогнать исправление Go-проекта;
- прогнать подготовку документа;
- собрать единый eval-набор;
- проверить, какие изменения стратегии выполняются только конфигурацией.

## Этап 2. Исполнители

- поддерживать Pi adapter по официальному RPC contract;
- поддерживать OpenCode adapter по официальному JSON CLI contract;
- добавить Codex/Claude Code adapters;
- ввести capability negotiation;
- поддержать session resume там, где он доступен.

## Этап 3. Полноценный runtime

- параллельные DAG-слои;
- расширяемые input adapters и typed inputs;
- retries с backoff;
- SQLite store;
- блокировки и recovery;
- typed outputs по JSON Schema;
- richer expressions через CEL-совместимый слой.

## Этап 4. Интеграция с кодовыми агентами

- MCP server: list/describe/start/get/answer/cancel/artifacts;
- готовые skills для Pi, OpenCode, Codex и Claude Code;
- поток событий запуска;
- режим `caller`, в котором внешний агент выполняет отдельный узел.

## Этап 5. Продуктовые функции

- server и web UI;
- worktree isolation;
- удалённые workers;
- GitHub integration;
- секреты и права;
- каталог пакетов workflows/commands/hooks.