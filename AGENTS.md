# Работа с проектом Takt

## Назначение

Takt — Go-runtime, который снаружи оркестрирует готовых кодовых агентов, модели, детерминированные проверки, циклы и approval. Внутренний tool loop, файловые инструменты, MCP, LSP, история и сжатие контекста остаются внутри Pi, OpenCode или другого исполнителя.

Текущий scope — локальный однопользовательский trusted runtime. Не расширяйте его до server/untrusted режима без отдельной threat model, sandbox и политики секретов.

## Перед изменением

1. Прочитайте `docs/12-document-map.md` и `docs/05-implementation-status.md`.
2. Для runtime изучите `docs/03-specification.md`, `docs/09-runtime-semantics.md` и ADR.
3. Для assistants изучите `docs/10-assistant-adapter-spec.md` и соответствующие contract tests.
4. Для evaluation изучите `docs/13-evaluation-plan.md` и документы `docs/26–30`.
5. Зафиксируйте исходное состояние командой `make check`.

Расширенная инструкция находится в `docs/15-coding-agent-start.md`.
Пользовательский скилл для создания workflow и профилей находится в `skills/takt/SKILL.md`; его references должны соответствовать текущей спецификации.

## Инварианты

- `allow_failure` разрешает только ненулевой exit code.
- `timed_out` и `cancelled` имеют приоритет над производными ошибками и output overflow.
- Root DAG, `loop_group`, скомпилированные `subworkflow` и `foreach` используют одну scheduler-семантику; независимые готовые узлы выполняются параллельными волнами, а вложенные `loop_group` в `v1alpha1` запрещены.
- `output_format` является проверяемым контрактом JSON-вывода агентного узла; условия и шаблоны могут читать его поля через JSON-путь.
- Approval внутри `loop_group` сохраняет номер активной итерации и после `answer` продолжает ту же итерацию.
- Markdown-план не преобразуется в task AST; `foreach` принимает только явный список или будущий явно выбранный adapter.
- `takt-assistant/v1alpha1` принимает один строгий JSON envelope; OS exit code совпадает с `result.exit_code`.
- Pi adapter ждёт `agent_settled`, проверяет Session ID, считает usage delta и не заменяет неуспешный resume на fresh.
- Retry сохраняет отдельную execution record с assistant, версией, requested/resolved model и usage.
- Измеренный ноль сериализуется как `0`; недоступная метрика — как `null`.
- Validation envelope декодируется только из stdout при любом terminal status; stderr остаётся диагностикой. Benchmark-успех требует `quality_node_status=completed` и `valid=true`.
- Текст агента не считается доказательством успеха: завершение подтверждает детерминированная проверка.
- Ошибки persistence всегда возвращаются вызывающему коду.

## Порядок изменения

1. Сначала добавьте регрессионный unit/contract/E2E тест.
2. Внесите минимальное изменение без параллельного расширения scope.
3. Изменение внешнего YAML/JSON-контракта сопровождайте обновлением `docs/03-specification.md` и `schemas/*.json`.
4. Изменение runtime/protocol semantics сопровождайте contract tests, `docs/09-runtime-semantics.md` или `docs/10-assistant-adapter-spec.md` и ADR.
5. Обновите `docs/05-implementation-status.md`, changelog и рабочие планы, когда меняется фактическое состояние.
6. Сохраняйте infrastructure contract suites отдельно от quality benchmark.

## Проверка

Минимум перед завершением:

```bash
gofmt -w cmd internal
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
./scripts/check-docs.sh
```

Полный релизный шлюз:

```bash
make check
./scripts/verify.sh
```

Реальные Pi/OpenCode smoke tests и benchmark выполняются отдельно при наличии бинарника, credentials, модели и штатного валидатора.

## Границы изменений

Сохраняйте Takt компактным orchestration runtime. Собственный coding-agent tool loop, общий plugin framework, Web UI, сервер, БД и поддержка недоверенных workflow не входят в текущий scope. Параллельность остаётся частью scheduler: независимые простые узлы и `foreach.parallel` выполняются конкурентно, а переходы состояния и запись событий сериализуются.

### OpenCode adapter

- Use only `opencode run --format json`; never parse the TUI.
- Treat stdout as NDJSON events and stderr as diagnostics.
- A resumed attempt must return the requested Session ID.
- Sum per-attempt usage only from unique `step_finish` events.
- Treat an OpenCode `error` event as failure even when the process exits with code 0.
- Do not claim provider-side model routing is observable when the event stream exposes only the requested CLI model.
- Keep `auto_approve` explicit and limited to trusted workspaces.
