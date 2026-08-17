# Outcome-gated `feature-development`: дизайн

Статус: **REVIEWED DIRECTION; CONDITIONAL design gate; implementation not started**
Дата: 2026-08-17

## 1. Заключение

`code:feature-development` должен оставаться полным delivery flow: реализация,
durable evidence, независимая проверка, публикация PR и итоговый handoff. Flow
ограничивает coding-agent через bounded attempts, deterministic gates и проверяемые
side effects, но не предписывает модели порядок исследования или редактирования.

Изменение не требует новых runtime primitives. Оно переиспользует существующие
`allow_failure`, `after_node`, hook retry с `session: fresh`, durable artifacts
и eval-fixture SCM gate. До workflow change обязателен baseline текущего
неизменённого flow на исправленной Pi model registry. Его результат определяет,
нужна ли fresh-retry topology вообще либо scope следует сузить или пересмотреть.

## 2. Проблема и подтверждённые факты

Прогон `.takt/evals/feature-development/20260817T181103.183410000Z` завершил
`implement` после трёх `Pi agent reached model output limit`. Первая попытка
выполнила 20 read/bash probes, две последующие resume-попытки не выполнили ни
одного tool call, `diff.patch` остался пустым, artifacts отсутствовали.

Это корректный reject, но текущая retry topology повторно использует сессию,
которая уже дважды продолжила незавершённый длинный ответ. Одновременно assistant
exit завершает action до `after_node`, поэтому deterministic validation не может
оценить частичный workspace и решить, достаточен ли фактический результат.

После этого прогона Pi model registry была исправлена: прежние `maxTokens=16384`
и `contextWindow=128000` не соответствовали фактическим возможностям модели.
Корректная registry-конфигурация является предусловием интерпретируемого eval, а
не workflow tuning. Baseline считается пригодным только если сохранённый Pi
`get_state` подтверждает ожидаемые effective limits.

Все 18 сохранённых прогонов в `.takt/evals/feature-development/` до исправления
registry относятся к **baseline v0**. Их нельзя включать в один trend или
агрегированное сравнение с новыми прогонами: requested/resolved model ID совпадает,
но внешняя Pi registry не входит в текущий strategy/config fingerprint.

Runtime уже поддерживает требуемую семантику:

- `allow_failure: true` разрешает только execution kind `exit`; timeout,
  cancellation, protocol, start и internal errors остаются terminal failures;
- allowed exit продолжает attempt через `after_node` и `before_complete` hooks;
- failed hook может повторить node и явно очистить Session ID через
  `on_failure.session: fresh`;
- отдельные execution records сохраняют исходный assistant failure, даже когда
  deterministic gate принимает итог attempt.

## 3. Цель и не-цели

### Цель

Flow считается успешным только когда подтверждены все результаты процесса:

1. workspace содержит реализацию;
2. implementation evidence сохранён;
3. deterministic repository validation проходит;
4. review/validation evidence сохранён и после review проверки проходят;
5. в flow evaluation PR side effect подтверждён fixture SCM state;
6. PR evidence и финальный summary сохранены;
7. внешний eval validator независимо принимает продукт и process evidence.

### Не-цели

- сравнивать Takt с прямым интерактивным Pi;
- убирать стандартные стадии разработки или Markdown evidence;
- раскрывать агентам hidden oracle evaluation corpus;
- считать текст агента доказательством корректности продукта;
- повышать Pi `maxTokens`, менять модель или добавлять model-specific prompting;
- добавлять новый scheduler, retry kind, YAML expression или transport behavior.

При этом корректные `maxTokens`/`contextWindow` являются обязательной
предпосылкой eval. Их исправление выполняется вне workflow change и меняет
условия измерения, поэтому требует нового baseline поколения.

## 4. Роли результатов

| Результат | Назначение | Авторитетная проверка |
|---|---|---|
| Git workspace/diff | фактическая реализация | repository validation и внешний validator |
| `implementation.md` | durable отчёт об изменениях, проверках и отклонениях | non-empty regular file; не доказательство качества кода |
| `validation.md` | команды, результаты, риски и review handoff | non-empty regular file плюс реальный deterministic check |
| `pr.md` | durable описание публикации | non-empty regular file плюс SCM side-effect gate |
| `pr-url.txt` | фактический URL PR | non-empty regular file плюс SCM side-effect gate |
| `summary.md` | итоговый handoff по всему flow | non-empty regular file и согласованность с terminal gates |

Artifacts являются обязательными продуктами стадий. Их наличие подтверждает
полноту процесса, но не заменяет executable validation, Git state или SCM state.

## 5. Условная candidate topology

Эта topology реализуется только если baseline из §8.1 подтверждает потребность
в outcome-gated retries. Сам baseline выполняется на текущем workflow без
описанных ниже изменений.

```text
implement attempt (assistant exit разрешён только до gate)
        │
        ▼
implementation gate: implementation.md + repository validation
        │ fail: bounded fresh retry с точным gate feedback
        ▼
validate-agent attempt
        │
        ▼
review gate: validation.md + repository validation
        │ fail: bounded fresh retry с точным gate feedback
        ▼
final deterministic validate
        ▼
create-pr attempt (без blind retry)
        ▼
eval PR gate: fixture SCM effect + pr.md + pr-url.txt
        ▼
summary attempt
        ▼
summary gate: summary.md + подтверждённые upstream gates
```

### 5.1. Implementation

`implement` сохраняет исходную задачу, workspace boundary и обязательный
`implementation.md`, но prompt не задаёт порядок исследования, tool choices или
момент первого edit.

Node получает:

- `allow_failure: true`, чтобы штатный assistant `exit`, включая model output
  exhaustion, не был авторитетнее результата workspace;
- не более трёх attempts;
- `after_node` gate, который требует non-empty regular `implementation.md` и
  запускает штатный `.takt/profiles/code/tools/validate`;
- `on_failure: {action: retry, session: fresh}` для result-gate failures.

Текущие `attempts.retry_on: [exit]` и `attempts.retry_session: reuse` удаляются.
После `allow_failure: true` обычный `exit` перестаёт быть ошибкой до hooks, поэтому
эта policy стала бы мёртвой и вводила бы автора workflow в заблуждение. Всеми
result retries владеет только `after_node.on_failure`; provider availability
retry остаётся отдельной runtime-семантикой.

Если assistant завершился с `exit`, но code, artifact и validation корректны,
stage принимается. Исходный failed execution остаётся в report. Если результат
неполон, следующая fresh session получает текущий workspace, user request и
точный stdout/stderr gate как feedback. Timeout, cancellation и protocol errors
не разрешаются и не маскируются gate.

### 5.2. Review and validation

`validate-agent` остаётся отдельной review-role стадией. Она проверяет diff,
может исправить найденные дефекты и обязана сохранить `validation.md`.

Для неё применяется тот же result-first принцип: обычный `exit` допускает запуск
gate, но gate требует `validation.md` и успешную repository validation. Safe
fresh retry ограничен двумя attempts. После принятия review отдельный
deterministic `validate` остаётся финальным fail-closed checkpoint перед SCM.

Внешний mini-du oracle не запускается внутри workflow и не передаёт hidden
feedback агенту. Он остаётся независимой post-run оценкой generalization.

### 5.3. PR publication

`create-pr` не получает blind retry: внешний эффект мог произойти до потери
assistant response. Обычный `exit` может быть разрешён только для перехода к
`pr-effect-gate`, который в flow evaluation проверяет fixture SCM call/state и
non-empty `pr.md`/`pr-url.txt`.

Существующий `.takt/profiles/code/tools/require-pr` остаётся fixture-only и в
обычном production workspace является no-op. Настоящий provider-independent
production receipt/reconciliation не добавляется этим изменением. В eval, если
fixture side effect подтверждён, flow продолжает работу независимо от terminal
assistant narrative; если эффект или evidence отсутствует, flow safe-stops.
Автоматический повтор публикации не добавляется; будущий production retry
допустим только как explicit reconcile operation.

### 5.4. Summary

`summary` сохраняется как отдельная стадия и обязательный `summary.md`, поскольку
задача flow включает handoff, а не только код. Summary gate принимает результат
только после успешных upstream validation и PR gates. Safe fresh retry разрешён,
так как запись summary не создаёт неизвестного внешнего эффекта, и ограничен
двумя attempts.

## 6. Prompt boundary

Prompt каждой assistant стадии содержит только:

- цель стадии;
- user request и доступный deterministic feedback;
- текущий execution workspace и разрешённый artifacts directory;
- обязательные outputs и ограничения безопасности/SCM.

Prompt не должен указывать модели, сколько исследовать, когда редактировать,
какими tools пользоваться или как распределять token budget. Bounded behavior
принадлежит runtime policy и gates, а не persuasive prompt text.

## 7. Failure semantics and observability

- Assistant `exit` и принятие stage gate являются двумя разными фактами; оба
  сохраняются в execution/node evidence.
- Gate feedback становится единственным repair feedback. Предыдущий свободный
  assistant narrative не считается validation result.
- Attempts exhaustion завершает node и downstream safe-stop.
- `time_to_valid_ms` остаётся доступен только при `valid: true`.
- Eval report должен позволять отличить: assistant exit accepted by gate,
  gate-triggered fresh retry, attempts exhausted, deterministic validation
  failure и missing SCM effect.
- Experiment notes обязаны фиксировать поколение registry (`v0` до исправления,
  `v1` после) и observed Pi `contextWindow`/`maxTokens`. До появления этих полей
  в machine-readable strategy identity сравнение поколений запрещено процедурой.

## 8. Проверки реализации

### 8.1. Baseline до workflow change

Сначала выполняется один `implement-basic` на **текущем неизменённом**
`code:feature-development`, той же модели и исправленной registry. Это не
сравнение с прямым Pi: проверяется именно production flow, который является
предметом eval.

```bash
EVAL_PRESET=qwen38 make eval-feature-smoke
```

Перед интерпретацией результата evidence должен подтвердить effective
`contextWindow=262144` или иное точное значение исправленной registry и
`maxTokens > 16384`. Возможные решения:

- `true_accept`: fresh retry для output-limit не обоснован этим case; scope
  сужается до enum-валидации hook session, outcome-observability и только тех
  gates, недостаточность которых показывает baseline;
- повтор прежнего over-investigation/output-limit pattern при исправленных
  limits: fresh restart не считается доказанным лечением; основной вариант
  меняется на explicit repair-role node с теми же result contracts;
- иная terminal причина: обновляются unknowns и topology до production edits.

Только после разбора baseline выбирается и фиксируется фактический workflow diff.

### 8.2. Regression tests

Регрессии добавляются до выбранного workflow change. Сначала unit contracts:

1. `AllowFailure + after_node` на одном узле: обычный `exit` сохраняется в
   execution record, hook выполняется и может принять node;
2. `hook.on_failure.session: fresh` действительно очищает Session ID и следующая
   assistant request использует fresh mode;
3. timeout вместе с `allow_failure: true` остаётся `timed_out`, а hooks не
   превращают его в допустимый exit;
4. schema и `internal/workflow` отклоняют неизвестное
   `hook.on_failure.session`; разрешены только `fresh|resume`, причём session
   допустима только для `action: retry`.

Затем profile/contract/E2E проверки:

5. assistant пишет корректный workspace и artifact, затем возвращает `exit` —
   implementation gate принимает stage и downstream продолжается;
6. первая попытка возвращает `exit` без результата, fresh retry создаёт корректный
   результат — Session ID не переиспользуется;
7. все attempts оставляют неполный результат — node failed, downstream skipped;
8. validate-agent `exit` после корректных fixes/evidence принимается только после
   deterministic validation;
9. PR assistant `exit` после подтверждённого fixture create принимается gate;
10. отсутствующий PR effect safe-stops без повторного create;
11. missing/empty/directory artifacts отклоняются соответствующим stage gate;
12. полный fake-agent E2E сохраняет все пять feature artifacts и SCM call;
13. profile compatibility tests фиксируют новую profile version и materialized
    workflow/commands;
14. focused Go suites, `go test ./... -count=1`, race, vet, `make check` и
   `scripts/verify.sh` проходят.

Если baseline обосновал workflow change, реальный Pi smoke изменённого flow
выполняется отдельно. Критерий smoke: flow либо выдаёт validator-accepted
результат, либо завершает bounded safe-stop с точной terminal причиной; повтор
одной output-limit session без новых tool calls недопустим.

## 9. Реестр неизвестных

| ID | Неизвестное | Приоритет | Решение/ограничитель | Статус |
|---|---|---|---|---|
| U-01 | Являются ли artifacts частью продукта flow | P0 | Да: подтверждено workflow commands, production E2E и пользователем | закрыт |
| U-02 | Должен ли assistant exit определять качество workspace | P0 | Нет: executable gates авторитетны; non-exit terminal classes остаются fail-closed | закрыт |
| U-03 | Нужна ли fresh retry topology после исправления registry | P1 | Сначала unchanged-flow baseline; topology выбирается только по его terminal evidence | открыт с обязательной проверкой |
| U-04 | Нужно ли семантически парсить Markdown evidence | P1 | Нет в этом срезе: presence означает handoff completeness, качество доказывают code/SCM gates | закрыт с ограничителем |
| U-05 | Нужно ли передавать hidden oracle feedback внутрь flow | P0 | Нет: это загрязнит benchmark; oracle остаётся post-run | закрыт |
| U-06 | Нужен ли blind retry PR publication | P0 | Нет: неизвестный side effect требует reconcile, текущий срез safe-stops | закрыт |
| U-07 | Сравнимы ли прогоны до и после Pi registry fix | P0 | Нет: 18 старых прогонов маркируются baseline v0, новые — v1; cross-generation trend запрещён | закрыт процедурой |
| U-08 | Проверяет ли текущий SCM gate production side effect | P1 | Нет, только eval fixture; production receipt/reconcile остаётся явно вне scope | закрыт с ограничителем |
| U-09 | Валидируется ли `hook.on_failure.session` fail-closed | P0 | Пока нет; schema и authoring validation входят в обязательный implementation delta | закрыт дизайном |

## 10. Слепые зоны и опровержение

- Если `allow_failure` не сохранит исходный failed execution при принятом gate,
  изменение останавливается и возвращается к runtime contract review.
- Если fresh retry теряет execution workspace или deterministic feedback, дизайн
  возвращается на пересмотр до изменения production workflow.
- Если PR fixture gate может принять fabricated artifact без SCM effect, сначала
  исправляется SCM gate; artifacts сами по себе недостаточны.
- Если unchanged-flow baseline проходит после registry fix, fresh retry не
  добавляется как страховочная абстракция без второго подтверждённого failure case.
- Если live smoke показывает повторное exhaustive restart без progress, нельзя
  лечить это model-specific prompt. Следующий вариант — explicit repair-role node
  с теми же outcome contracts.

Изменение не требует migration durable run state: новые Runs используют новый
workflow fingerprint, старые evidence/report остаются читаемыми. Откат — возврат
workflow/command profile version; persistence schema не меняется.

## 11. Проектный шлюз

**Статус: CONDITIONAL. Уверенность: высокая.**

Открытых P0 нет. U-03 остаётся P1 с дешёвой обязательной проверкой. До baseline
текущего flow код, schema и production profile не изменяются. После baseline spec
обновляется выбранной ветвью, и только затем начинаются regression tests и
соответствующий production diff. Если workflow change нужен, Pi smoke изменённого
flow закрывает условие перед финальным commit либо фиксирует safe rollback.

## 12. Контракт передачи в реализацию

Соблюдать принятые границы: artifacts и стандартные стадии сохраняются; text не
заменяет deterministic proof; hidden oracle не входит в repair loop; PR нельзя
blind-retry; новые runtime/YAML semantics не добавляются.

Если обнаружится факт, который меняет эти contracts, persistence, side-effect
semantics или опровергает U-03, остановить реализацию, зафиксировать evidence и
вернуть задачу на `design-unknowns`. Локальные изменения prompts/tests допустимы
только когда они реализуют описанные output contracts и не предписывают модели
метод работы.

После baseline фактический implementation delta фиксируется явно:

- workflow schema: `hookDecision.session` получает enum `fresh|resume`;
- workflow authoring: `internal/workflow/validate.go` отклоняет неизвестную
  session и session при `action != retry`;
- schema/authoring contracts: добавляются focused tests для JSON Schema и Go
  validation, а `docs/03-specification.md` фиксирует допустимые значения;
- `implement`: при выборе outcome-gating добавляются `allow_failure` и fresh hook
  retry, удаляются `retry_on: [exit]`/`retry_session: reuse`;
- `validate-agent`: сейчас не имеет attempts/hooks; при подтверждённой topology
  получает максимум две попытки, `allow_failure` и validation artifact gate;
- `validate`: остаётся отдельным финальным deterministic checkpoint;
- `create-pr`: не получает retry; allowed exit, artifact checks и fixture SCM
  effect принимаются только отдельным all-done gate;
- `pr-effect-gate`: расширяется проверками `pr.md`/`pr-url.txt`, но остаётся
  fixture-only; production receipt не заявляется;
- `summary`: сейчас не имеет gate; получает максимум две safe attempts и
  обязательный summary artifact gate;
- code profile: при фактическом profile change версия повышается с `0.18.0` до
  `0.19.0`, синхронно обновляются profile README, compatibility/core contract,
  implementation status и changelog; schema-only change до baseline profile
  version не повышает;
- evaluation docs: фиксируются registry generation и запрет сравнения baseline
  v0/v1; сохранённые evidence не переписываются.

Не реализовывать topology-пункты, которые unchanged-flow baseline сделал
необоснованными. Если baseline меняет выбранную ветвь, сначала обновить эту spec
и повторить review gate.
