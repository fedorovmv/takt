# Ревизия gaps и backlog Takt — 2026-08-21

Актуальный результат реализации волн 0–2 и приоритетный список находятся в
[`docs/14-backlog-v0.2.md`](../../14-backlog-v0.2.md). Реестр ниже сохраняет
исходный audit baseline до изменений; закрытые пункты отмечены в текущем
backlog, а live evidence gaps намеренно остаются открытыми.

## 1. Итог

**Проектный шлюз:** `BLOCKED` для promotion в `v1beta1` и claims о strict/stable внешних интеграциях.

Причина — не дефекты ядра, а незакрытые внешние доказательства: conformance на фактически закреплённых версиях Pi/OpenCode, production-shaped evaluation и release/contract convergence.

Кодовый контур на момент ревизии зелёный:

- `make check` — PASS;
- `go test ./... -count=1` — PASS;
- `go test -race ./... -count=1` — PASS;
- `go vet ./...` — PASS;
- `make journeys` — PASS;
- `./scripts/verify.sh` — PASS;
- рабочее дерево Git — чистое.

GitHub Issues не содержат открытых задач; backlog фактически ведётся в документах репозитория.

## 2. Карта и территория

### Подтверждённые факты

- На момент baseline-аудита код и implementation status относились к `v0.1.63-alpha`; после waves 0–2 текущий срез — `v0.1.64-alpha` ([`docs/05-implementation-status.md`](../../05-implementation-status.md)).
- Unified Run evaluation, immutable assessments, matrix branches и canonical `run status|stats|inspect|assessment` реализованы ([`docs/05-implementation-status.md:5-36`](../../05-implementation-status.md#L5)).
- Core runtime, durable state/events, retry/backoff, loops, child Runs, fan-out, artifacts, approval, local sandbox и trusted-local security реализованы ([`docs/05-implementation-status.md:197-264`](../../05-implementation-status.md#L197)).
- Pi/OpenCode adapters реализованы, но bundled host integrations остаются `guarded`; strict conformance не заявляется ([`docs/05-implementation-status.md:209-219`](../../05-implementation-status.md#L209)).
- Сохранённое live evidence относится к Pi `0.83.0` и OpenCode `1.18.14`; в текущем окружении обнаружены Pi `0.84.1` и OpenCode `1.18.18`.
- Compatibility matrix сама фиксирует, что host integration требует live conformance на pinned version ([`internal/tooling/compatibility/compatibility.go:86-92`](../../../internal/tooling/compatibility/compatibility.go#L86)).
- `make check` не запускает process-heavy E2E, journeys и полный race-контур ежедневно; это сделано намеренно ([`docs/05-implementation-status.md:48-61`](../../05-implementation-status.md#L48)). Полный контур и journeys прошли отдельно.
- CI запускает `make check` на Ubuntu и macOS, но не отдельные `make journeys`/`check-full` ([`.github/workflows/ci.yml`](../../../.github/workflows/ci.yml)).

### Выводы

- Главный bottleneck теперь — evidence и compatibility, а не расширение scheduler или новый architecture layer.
- Synthetic fixtures и production-shaped cases доказывают correctness измерительного контура, но не качество модели на production corpus ([`docs/05-implementation-status.md:289-305`](../../05-implementation-status.md#L289)).
- `workflow describe`, `task explain`, `run inspect` уже покрывают часть заявленного UX-gap; полный набор `workflow graph/explain/scaffold` и `plan explain` ещё не реализован ([`docs/14-backlog-v0.2.md:168-183`](../../14-backlog-v0.2.md#L168)).

### Противоречия карты

1. [`docs/06-roadmap.md`](../../06-roadmap.md) всё ещё объявлен как план после `v0.1.57-alpha`, хотя фактический статус уже `v0.1.63-alpha`.
2. [`docs/14-backlog-v0.2.md`](../../14-backlog-v0.2.md) смешивает закрытые исторические разделы и активные задачи.
3. `eval flow` уже использует authored ordinary Run, но `eval flow init` по-прежнему создаёт deprecated legacy scaffold ([`docs/05-implementation-status.md:32-36`](../../05-implementation-status.md#L32)).
4. После `v0.1.63-alpha` в `Unreleased` накопились изменения поведения; перед следующим versioned behavior merge требуется зарезервировать следующую alpha-версию по правилам [`AGENTS.md:63-79`](../../../AGENTS.md#L63).

## 3. Реестр неизвестных и gaps

| ID | Приоритет | Класс | Неизвестное / gap | Свидетельство | Критерий закрытия | Владелец | Статус |
|---|---:|---|---|---|---|---|---|
| `REL-001` | P0 | известное неизвестное | Какая версия является следующим versioned срезом после `0.1.63-alpha` | `CHANGELOG.md` содержит поведенческие изменения в `Unreleased`; `VERSION` остаётся `0.1.63-alpha` | Зарезервирована `0.1.64-alpha`; синхронизированы `VERSION`, `internal/version`, README, specification/status и release notes | maintainer | открыт |
| `HOST-001` | P0 | известное неизвестное | Можно ли честно заявить strict host control на закреплённых версиях | [`integrations/coding-agent-host-control/README.md:12-19`](../../../integrations/coding-agent-host-control/README.md#L12) требует command/input/tool/completion/recovery evidence | Live suite подтверждает все обязательные capabilities; только после этого `strict_allowed=true`, иначе сохраняется `guarded` с diagnostic | adapter/integration owner | открыт |
| `HOST-002` | P0 | неизвестное неизвестное | Поведение изменившихся Pi/OpenCode версий относительно сохранённого evidence | Текущее окружение: Pi `0.84.1`, OpenCode `1.18.18`; evidence: Pi `0.83.0`, OpenCode `1.18.14` | Версии pin-ятся и повторяется полный conformance, включая completion/tool blocking и recovery; результат сохраняется с version/fingerprint | integration owner | открыт |
| `EVAL-001` | P1 | известное неизвестное | Действительная полезность Takt на production corpus и реальных моделях | [`docs/05-implementation-status.md:495-501`](../../05-implementation-status.md#L495) и [`docs/14-backlog-v0.2.md:52-95`](../../14-backlog-v0.2.md#L52) | Route DSL, Go и Document corpus; минимум 3 repeat на case/strategy; validator; stable/unstable; success@1, final success, attempts-to-valid, time-to-valid, tokens/cost, manual corrections | product/evaluation owner | открыт |
| `EVAL-002` | P1 | известное неизвестное | Как завершить переход с legacy `eval flow init` на authored workflow authoring | [`docs/13-evaluation-plan.md:236`](../../13-evaluation-plan.md#L236) | New scaffold генерирует authored workflow либо legacy режим явно отделён и имеет срок compatibility window; документация и tests согласованы | evaluation owner | отложен до EVAL-001 |
| `DOC-001` | P1 | подтверждённый gap | Roadmap/backlog не отражают актуальный version/status и закрытые пункты | [`docs/06-roadmap.md:3`](../../06-roadmap.md#L3), [`docs/14-backlog-v0.2.md:3`](../../05-backlog-v0.2.md#L3) | Active backlog содержит только открытые задачи с owner, evidence, acceptance и dependency; completed history вынесена в archive | maintainer | открыт |
| `ADAPTER-001` | P1 | известное неизвестное | Работают ли reference Qwen/GitHub adapters с реальными credentials/provider | Public SDK и deterministic E2E закрыты, live smoke явно deferred ([`docs/05-implementation-status.md:233-242`](../../05-implementation-status.md#L233)) | Отдельный live smoke с redacted evidence; отсутствие credentials не превращается в unsupported claim | integration owner | открыт |
| `API-001` | P1 | известное неизвестное | Какие поля реально входят в `v1beta1` после production use | Migration policy намеренно отложена до evidence ([`docs/14-backlog-v0.2.md:112-138`](../../14-backlog-v0.2.md#L112) ) | Field decisions подтверждены реальными workflow/config/evaluation; выпущены migration guide и migrator только при необходимости | contract owner | заблокирован EVAL-001 |
| `CI-001` | P1 | скрытый критерий | Проверяется ли основной user journey на обеих поддерживаемых ОС в CI | CI запускает `make check`, а journeys являются отдельным target | Добавлен отдельный Linux/macOS journey gate либо опубликовано явное решение, почему он остаётся manual | CI/maintainer | открыт |
| `UX-001` | P2 | скрытый критерий | Нужны ли отдельные `workflow graph/explain/scaffold` и `plan explain` | Уже есть `workflow describe`, `task explain`, `run inspect` | Реальный user request + acceptance для конкретной команды; до этого не расширять CLI | product owner | отложен |
| `FLOW-001` | P2 | допущение | Нужен ли новый YAML `approval.on_reject`/static revise path | Существующие `loop_group` + approval + `when` уже выражают bounded review loop | Новый use case, который нельзя выразить текущим contract без потери governance | workflow owner | условный |
| `BUDGET-001` | P2 | неизвестное неизвестное | Можно ли enforce hard token/tool budgets на конкретных agents | Deferred до live capability proof ([`docs/09-runtime-semantics.md:457`](../../09-runtime-semantics.md#L457)) | Capability proof, fail-before-execution semantics и измеренная потребность | runtime + adapter owner | условный |
| `MERGE-001` | P2 | неизвестное неизвестное | Нужен ли mutating merge fan-out и как откатывать неизвестный side effect | Deferred до отдельного use case/threat model | Production multi-repo scenario, merge action, reconcile/rollback contract и deterministic tests | runtime/domain owner | условный |

## 4. Приоритетный план

### Волна 0 — release and backlog hygiene

1. Зарезервировать следующую alpha-версию для следующего behavior-bearing merge.
2. Обновить roadmap/status/backlog и убрать из active backlog уже закрытые архитектурные и тестовые пункты.
3. Для каждой внешней задачи добавить owner, pinned version, expected evidence и acceptance.

### Волна 1 — P0

1. Выбрать policy: pin текущих Pi/OpenCode или обновить integration contracts под `0.84.1`/`1.18.18`.
2. Выполнить полный live host conformance: `/takt`, input interception, tool blocking, completion blocking, recovery.
3. Оставить `guarded`, если хотя бы одна capability не доказана; не повышать статус по одному version probe.
4. Добавить Linux/macOS journeys в CI либо зафиксировать решение о manual gate.

### Волна 2 — P1

1. Собрать production evidence для Route DSL, Go и Document.
2. На основании evidence завершить `v1alpha1 → v1beta1` field audit и migration guide.
3. Провести live Qwen/GitHub adapter smoke.
4. Принять решение по authored replacement для `eval flow init`.

### Волна 3 — P2 только по фактической потребности

- UX-команды graph/explain/scaffold;
- static reject/revise;
- hard token/tool budgets;
- mutating merge fan-out;
- расширенные iteration/evidence projections в `run inspect`.

До появления use case не добавлять server, Web UI, database-backed store, remote workers, message adapters или multi-user auth/RBAC: они явно находятся вне текущего trusted-local scope ([`docs/14-backlog-v0.2.md:185-200`](../../14-backlog-v0.2.md#L185)).

## 5. Условия возврата к проектированию

Остановить реализацию и повторить `design-unknowns`, если обнаружится факт, который меняет:

- public Workflow/Config/protocol contract;
- durable state, assessment или migration semantics;
- trust boundary, secret handling или host enforcement;
- ownership между runtime, adapter и transport;
- критерий production acceptance.

Особенно нельзя скрытым изменением архитектуры решать расхождение между pinned host version и сохранённым conformance evidence.

## 6. Контракт передачи в реализацию

Соблюдай принятые решения, допущения и ограничители.

До начала новой feature work сначала закрой P0 либо получи явное решение оставить соответствующую capability `guarded/deferred`. Не принимай synthetic test pass за production evidence. Для P1 соблюдай owner, pinned environment и критерий закрытия из таблицы.

Если обнаружится факт, который меняет контракт, данные, безопасность или границы компонентов либо опровергает допущение, связанное с P0/P1, останови реализацию, зафиксируй отклонение и верни задачу на повторный `design-unknowns`.
