# Работа с проектом Takt

## Назначение

Takt — Go-runtime, который снаружи оркестрирует готовых кодовых агентов, модели, детерминированные проверки, циклы и approval. Внутренний tool loop, файловые инструменты, MCP, LSP, история и сжатие контекста остаются внутри Pi, OpenCode или другого исполнителя.

Текущий scope — локальный однопользовательский trusted runtime. Не расширяйте его до server/untrusted режима без отдельной threat model, sandbox и политики секретов.

## Перед изменением

1. Прочитайте `docs/12-document-map.md` и `docs/05-implementation-status.md`.
2. Для runtime и local control plane изучите `docs/03-specification.md`, `docs/04-architecture.md`, `docs/09-runtime-semantics.md`, `docs/72-architecture-contracts-v0.1.57.md` и ADR.
3. Для assistants изучите `docs/10-assistant-adapter-spec.md` и соответствующие contract tests.
4. Для evaluation изучите `docs/13-evaluation-plan.md` и документы `docs/26–30`.
5. Зафиксируйте исходное состояние командой `make check`.

Расширенная инструкция находится в `docs/15-coding-agent-start.md`.
Пользовательский скилл для создания workflow и профилей находится в `skills/takt/SKILL.md`; его references должны соответствовать текущей спецификации.

## Инварианты

- `allow_failure` разрешает только ненулевой exit code.
- `timed_out` и `cancelled` имеют приоритет над производными ошибками и output overflow.
- Root DAG, `loop_group`, скомпилированные `subworkflow` и `foreach` используют одну scheduler-семантику; независимые готовые узлы выполняются параллельными волнами, а вложенные `loop_group` в `v1alpha1` запрещены.
- `output_format` является проверяемым контрактом JSON-вывода; обязательные шаблоны fail-closed, optional/default задаются явно, а authoring проверяет доступные JSON-пути до Run.
- Node policy проверяется до запуска assistant; explicit empty allowlist остаётся запретом, а неподдерживаемая capability не может молча игнорироваться.
- Governed child policy является верхней границей: allowlists пересекаются, deny/requirements объединяются, более строгий sandbox наследуется.
- Approval внутри `loop_group` сохраняет номер активной итерации и после `answer` продолжает ту же итерацию.
- Markdown-план не преобразуется в task AST; `foreach` принимает только явный список или будущий явно выбранный adapter.
- `takt-assistant/v1alpha1` принимает один строгий JSON envelope; OS exit code совпадает с `result.exit_code`.
- Pi adapter ждёт `agent_settled`, проверяет Session ID, считает usage delta и не заменяет неуспешный resume на fresh.
- Retry сохраняет отдельную execution record с assistant, версией, requested/resolved model и usage.
- Измеренный ноль сериализуется как `0`; недоступная метрика — как `null`.
- Validation envelope декодируется только из stdout при любом terminal status; stderr остаётся диагностикой. Benchmark-успех требует `quality_node_status=completed` и `valid=true`.
- Текст агента не считается доказательством успеха: завершение подтверждает детерминированная проверка.
- Ошибки persistence всегда возвращаются вызывающему коду.
- CLI, MCP и local daemon используют stable use cases (`internal/application`, `internal/externalworker`) и canonical `internal/appapi`; единственный production composition root — `internal/bootstrap`. Не реализуйте второй executor, transport-specific Run semantics или прямой обход use-case boundary к runtime/store.
- Domain adapters (`scm|tracker|ci`) являются обычными Node actions единого scheduler; provider/transport details живут в config/adapter, а `side_effect: reconcile` запрещает blind retry после неизвестного внешнего эффекта.
- Retry/backoff является частью durable runtime state: scheduler обязан сохранять точный `not_before`, а resume не должен сбрасывать задержку или diagnostic fingerprint.
- Секреты передавайте через `secret://ENV_NAME`. Перед persistence state/events и текстовых artifacts применяется общий redactor; бинарный artifact с известным секретом должен fail-closed. Публичный control/CLI после исполнения возвращает состояние, повторно загруженное из Store, а не живой in-memory state.
- `sandbox.enforcement` — отдельный локальный OS слой только для deterministic `bash/script`; assistant sandbox остаётся adapter capability. `required` обязан fail-before-execution, если OS backend недоступен; `optional` сохраняет degraded decision.
- Early-exit fan-out не маскируйте под пользовательскую отмену: ненужные siblings получают `cancel_reason=fanout_result_decided`. Внутренний `NodePath` остаётся канонической адресацией вложенных узлов, публичный node ID — совместимым интерфейсом.
- `always_run` не скрывает failure основного графа; `idle_timeout` измеряет отсутствие нормализованной активности, а не полный timeout.
- Daemon локальный и однопользовательский: Unix socket не является новой security boundary и не должен публиковаться в сеть.
- `cmd/takt` остаётся launcher; parsing/output живёт в `internal/cli`. Stable Run use cases находятся в `internal/application`/`internal/externalworker`, расширения — в `internal/extensions`, экспериментальные flow/host/learning — в `internal/experimental`, evaluation/compatibility — в `internal/tooling`; concrete wiring находится только в `internal/bootstrap`.
- Stable use cases зависят от persistence через consumer-owned ports; `internal/runcontrol` содержит только общие lock/redaction/durable-reload helpers. Transport packages не создают `store.FS`, `runtime.Runner` или notification dispatcher напрямую.
- Общие MCP/daemon операции добавляются один раз в canonical `internal/appapi` registry. Новый transport-specific business switch запрещён.
- Конституция workflow: **YAML координирует. Код вычисляет. Агент принимает решения.** Новое YAML-поле допускается только если runtime должен видеть его для governance, оно является декларативными данными и существующий script/command/prompt escape hatch не решает задачу без потери governance-свойств. `when` остаётся ограничен `==`, `!=`, `&&`, `||`; не добавляйте скобки, функции, regex, арифметику или новые операторы по одному.
- Concrete assistant extensions только объявляют `ProviderRegistration`. Production immutable registry собирается ровно один раз в `internal/bootstrap`; package-global registries, `init()`-регистрация и скрытая мутация provider set запрещены.
- Canonical application operation описывается один раз через `internal/appapi.OperationDescriptor`: ID/stage/MCP name/title/description/InputSchema/annotations. Schema проверяет вход до typed decode; MCP и generated docs используют те же descriptors. Не дублируйте canonical MCP schemas/metadata в transport package.
- Closed-world switch node actions допустим и предпочтительнее generic plugin framework; новую абстракцию добавляйте только при двух фактических реализациях/потребителях или подтверждённой внешней extension boundary.
- Product correctness принадлежит Go `*_test.go`; black-box contracts живут в `tests/e2e`. Новый `scripts/test-*.sh` допускается только как явно обоснованный внешний smoke boundary и должен быть добавлен в allowlist `internal/architecture`.

## Порядок изменения

1. Сначала добавьте регрессионный unit/contract/E2E тест.
2. Внесите минимальное изменение без параллельного расширения scope.
3. Изменение внешнего YAML/JSON-контракта сопровождайте обновлением `docs/03-specification.md` и `schemas/*.json`.
4. Изменение runtime/protocol semantics сопровождайте contract tests, `docs/09-runtime-semantics.md` или `docs/10-assistant-adapter-spec.md` и ADR.
5. Обновите `docs/05-implementation-status.md`, changelog и рабочие планы, когда меняется фактическое состояние.
6. Сохраняйте infrastructure contract suites отдельно от quality benchmark.

## Версионирование

- Корневой `VERSION` описывает текущий код, а не последний исторический релиз,
  и обязан совпадать с `internal/version.Value`. Расхождение блокирует проверки.
- Первый после versioned-среза merge, который меняет пользовательское поведение,
  CLI, Config/Workflow/schema/protocol contract, сохраняемую семантику или
  evaluation identity, резервирует следующую alpha-версию. Для текущей линии
  `0.1.x` по умолчанию увеличивается patch. Остальные изменения этого среза
  накапливаются под той же версией; повышать версию на каждый коммит не нужно.
- При завершении среза перенесите записи из `Unreleased` под заголовок версии и
  синхронно обновите `VERSION`, `internal/version.Value`, текущую версию в
  `README.md`, `docs/03-specification.md` и `docs/05-implementation-status.md`.
- Чистые тесты, документация и внутренний refactoring без изменения поведения
  или контракта не требуют новой версии продукта.
- Профили и skills версионируются независимо. Изменение устанавливаемого
  содержимого профиля или authoring-контракта skill требует повышения их
  собственного `VERSION`, документации совместимости и contract tests.

## Проверка

Минимум перед завершением:

```bash
gofmt -w cmd internal sdk reference tests
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
```

Полный релизный шлюз:

```bash
make check
./scripts/verify.sh
```

Реальные Pi/OpenCode smoke tests и benchmark выполняются отдельно при наличии бинарника, credentials, модели и штатного валидатора.

## Границы изменений

Сохраняйте Takt компактным orchestration runtime. Собственный coding-agent tool loop, общий plugin framework, Web UI, удалённый/многопользовательский сервер, БД и поддержка недоверенных workflow не входят в текущий scope. Локальный `takt daemon` является частью scope и использует файловый Store. Параллельность остаётся частью scheduler: независимые простые узлы и `foreach.parallel` выполняются конкурентно, а переходы состояния и запись событий сериализуются.

### OpenCode adapter

- Use only `opencode run --format json`; never parse the TUI.
- Treat stdout as NDJSON events and stderr as diagnostics.
- A resumed attempt must return the requested Session ID.
- Sum per-attempt usage only from unique `step_finish` events.
- Treat an OpenCode `error` event as failure even when the process exits with code 0.
- Do not claim provider-side model routing is observable when the event stream exposes only the requested CLI model.
- Keep `auto_approve` explicit and limited to trusted workspaces.
