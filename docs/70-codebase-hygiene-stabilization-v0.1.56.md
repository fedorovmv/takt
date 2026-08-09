# Codebase Hygiene & Stabilization — v0.1.56-alpha

`v0.1.56-alpha` завершает архитектурный feature freeze без удаления существующих возможностей и без расширения Workflow/Run/MCP/SDK контрактов. Цель релиза — убрать оставшиеся дублирующие инфраструктурные реализации и provider/test-specific код из стабильного ядра, а пользовательские поверхности привести в соответствие фактическому статусу модулей.

## JSON Schema

`takt-schema-subset/v1` остаётся публичным ограниченным контрактом authoring/runtime. Takt по-прежнему определяет допустимые types/keywords и проверяет корректность subset definition, но больше не реализует собственный JSON Schema execution engine.

Runtime validation делегирован `github.com/santhosh-tekuri/jsonschema/v6` с Draft 2020-12. Из `internal/schemasubset` удалены собственные реализации numeric equality, `uniqueItems`, range/type recursion и instance validation. После успешной проверки Takt сохраняет прежнюю canonical compact-JSON normalization.

Contract tests опубликованных схем используют тот же upstream validator; отдельного test-only JSON Schema runtime больше нет. Внешний Takt error contract фиксирует контекст `JSON Schema validation failed`, но не текст diagnostics конкретной версии библиотеки.

## Assistant core и provider extensions

Stable `internal/assistant` содержит provider-neutral contracts:

- `SessionAdapter` / `Resolver`;
- `process` protocol v1alpha1/v1alpha2;
- policy/capability/event/tool-control semantics;
- logical `coding-agent` selection.

Pi и OpenCode реализации перенесены в `internal/extensions/assistants/{pi,opencode}` и подключаются только в `internal/bootstrap`. Stable workflow validation проверяет логическое имя configured assistant независимо от наличия конкретной provider implementation; capability preflight использует тот же injected resolver, что и фактическое выполнение.

Функциональность Pi/OpenCode и их contract fixtures сохранена. Их статус остаётся `supported-alpha`/guarded до live conformance и не расширяет stable core guarantee.

## Test fixtures

`takt-fake-*` больше не являются product commands. Их исходники находятся в `internal/testsupport/cmd`, а E2E/contract tests собирают fixtures по тем же историческим именам во временную директорию.

`cmd/` содержит только реальные пользовательские/reference binaries:

- `takt`;
- `qwen-takt-adapter`;
- `takt-github-scm-adapter`;
- `takt-github-task-source`.

Architecture gate запрещает возвращать fake binaries в product `cmd/`.

## Stable application decomposition

External worker/tool lifecycle вынесен из общего `internal/application` в самостоятельный `internal/externalworker`. Он не получает `RunStore`/`Runner` internals из application, а использует два узких Run-coordination действия: продолжить уже загруженный Run и распространить terminal/waiting состояние по parent chain.

Короткие общие операции persistence (`AcquireLock`, redacted commit, durable public reload) находятся в `internal/runcontrol`; это устраняет дублирование между обычным Run lifecycle и external-worker lifecycle без создания generic repository/framework. После переноса `internal/application` содержит около 2,2 тыс. production-строк вместо ~3,5 тыс. в v0.1.55.

## Stable runtime decomposition

Без изменения state/wire semantics разложены наиболее сложные участки стабильного пути:

- `script` execution: working directory, argument rendering, policy/sandbox construction, process setup и result mapping;
- `takt-assistant/v1alpha2`: process lifecycle, stream decoding, record dispatch, tool decision и final result verification;
- governed child Run: definition/output resolution, durable lineage link, start/resume, isolation/policy options и terminal projection.

Dynamic Flow validators и provider parsers остаются внутри experimental/extensions и не перерабатываются только ради метрик сложности.

## CLI и MCP stability

Имена команд и MCP tools сохранены. Пользовательская навигация теперь отражает границы ADR-088:

- CLI: `stable`, `extensions`, `experimental`, `tooling`;
- MCP Dynamic Flow (`takt.task.*`, `takt.plan*`, `takt.execute`, `takt.run.steer`) и Host Control получают явную `Experimental:` пометку в tool descriptions;
- compatibility matrix больше не называет `agent`/host surfaces stable-candidate, пока они зависят от experimental Dynamic Flow/Host Control.

## Архитектурные gates

`internal/architecture` дополнительно проверяет:

- отсутствие `cmd/takt-fake-*`;
- отсутствие Pi/OpenCode implementation files в stable `internal/assistant`;
- запрет stable assistant core импортировать extensions;
- наличие upstream JSON Schema validator в `internal/schemasubset`;
- отсутствие старых функций собственного instance validator;
- запрет возвращать external worker/tool lifecycle в общий `internal/application`.

После этого релиза дальнейшая стабилизация должна идти от реального пользовательского использования, а не от очередного общего package refactor.
