# Outcome-gated `feature-development`: дизайн

Статус: **UPDATED AFTER BASELINE; CONDITIONAL design gate; implementation not started**
Дата: 2026-08-17
Обновлено: 2026-08-18

## 1. Заключение

`code:feature-development` должен оставаться полным delivery flow: реализация,
durable evidence, независимая проверка, публикация PR и итоговый handoff. Flow
ограничивает coding-agent через bounded attempts, deterministic gates и проверяемые
side effects, но не предписывает модели порядок исследования или редактирования.

Изменение не требует новых runtime primitives. Baseline текущего неизменённого
flow на исправленной Pi model registry исключил fresh-retry topology: output limit
не повторился. Вместо этого baseline обнаружил разрыв `review -> repair`: review
вернул `NOT PASS`, но свободный Markdown не был control signal, поэтому flow
опубликовал PR и внешний validator принял неполный продукт.

Выбранная topology переиспользует существующие assistant nodes, ограниченный
`when`, deterministic bash nodes, durable artifacts и eval-fixture SCM gate.
Review выдаёт строгий verdict, отдельный parser fail-closed превращает его в
node output, одна conditional repair-попытка исправляет findings, а независимая
revalidation допускает публикацию только после `PASS`.

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

Обязательный baseline после исправления registry выполнен:

- run: `.takt/evals/feature-development/20260817T210622.749715000Z`;
- effective limits: `contextWindow=262144`, `maxTokens=64000`;
- результат evaluator: `outcome=true_accept`, `valid=true`, все шесть узлов
  завершились с одной попытки, `retry_scheduled=0`, `truncated_nodes=0`;
- четыре assistant execution суммарно использовали `106342` output tokens, поэтому
  прежний предел `16384` фактически не действовал;
- `validation.md` завершился verdict `NOT PASS (repair needed)` и перечислил F1-F4:
  cross-argument hardlink dedup, проглатывание traversal errors, latent отсутствие
  directory `st_blocks` и bare `--` без default `.`;
- prompt `validate-change` прямо оставил broader changes для repair step, которого
  в `feature-development` нет;
- после `NOT PASS` узлы `validate`, `create-pr`, `pr-effect-gate` и `summary`
  завершились успешно, а внешний oracle вернул `valid=true`.

Это четвёртый исход, отсутствовавший в исходной матрице §8.1: **oracle
`true_accept` при review `NOT PASS`**. Он одновременно доказывает два пробела:
review decision не управляет DAG, а oracle version 2 не покрывает часть заявленного
контракта. Fresh retry этим baseline не обоснован.

В том же assistant attempt `create-pr` fixture `scm/calls.log` сохранил два
`pr create --draft` с разными body/URL. Текущий `require-pr` проверяет наличие
create-effect, а не его единственность, поэтому `pr-effect-gate` принял duplicate.
Это не workflow retry, но тот же unknown-side-effect risk внутри одной попытки.

Runtime уже поддерживает требуемую семантику:

- stdout deterministic bash node доступен как node output для downstream `when`;
- ограниченный `when` уже поддерживает требуемые `==`, `!=`, `&&` и `||`;
- `trigger_rule: all_done` позволяет acceptance gate увидеть completed, skipped и
  failed dependencies и вынести единое fail-closed решение;
- разные assistant nodes создают отдельные sessions без явного resume;
- execution records сохраняют каждую review/repair/revalidation стадию отдельно.

Кроме того, production profile уже использует conditional recovery/revalidation,
ограниченный `when` и deterministic acceptance gates в `plan-to-pr` и
`review-block`; новая runtime-абстракция для review verdict не нужна.

## 3. Цель и не-цели

### Цель

Flow считается успешным только когда подтверждены все результаты процесса:

1. workspace содержит реализацию;
2. implementation evidence сохранён;
3. deterministic repository validation проходит;
4. review/validation evidence сохранён, strict verdict равен `PASS` либо после
   единственной repair-попытки независимая revalidation вернула `PASS`;
5. в flow evaluation PR side effect подтверждён fixture SCM state;
6. PR evidence и финальный summary сохранены;
7. внешний eval validator независимо принимает продукт и process evidence.

### Не-цели

- сравнивать Takt с прямым интерактивным Pi;
- убирать стандартные стадии разработки или Markdown evidence;
- раскрывать агентам hidden oracle evaluation corpus;
- считать текст агента доказательством корректности продукта;
- повышать Pi `maxTokens`, менять модель или добавлять model-specific prompting;
- добавлять новый scheduler, retry kind, YAML expression или transport behavior;
- добавлять fresh retry для output-limit без нового подтверждённого failure case;
- превращать `feature-development` в полный multi-perspective adversarial review.

При этом корректные `maxTokens`/`contextWindow` являются обязательной
предпосылкой eval. Их исправление выполняется вне workflow change и меняет
условия измерения, поэтому требует нового baseline поколения.

## 4. Роли результатов

| Результат | Назначение | Авторитетная проверка |
|---|---|---|
| Git workspace/diff | фактическая реализация | repository validation и внешний validator |
| `implementation.md` | durable отчёт об изменениях, проверках и отклонениях | non-empty regular file; не доказательство качества кода |
| `validation.md` | initial review, команды, findings и строгая строка `verdict: PASS|REPAIR|BLOCKED` | fail-closed parser; verdict управляет веткой, но не доказывает качество |
| `review-fixes.md` | resolution table единственной repair-попытки | обязателен только на ветке `REPAIR`; non-empty regular file |
| `revalidation.md` | независимая проверка после repair и строгая строка verdict | fail-closed parser; только `PASS` допускает final validate |
| `pr.md` | durable описание публикации | non-empty regular file плюс SCM side-effect gate |
| `pr-url.txt` | фактический URL PR | non-empty regular file плюс SCM side-effect gate |
| `summary.md` | итоговый handoff по всему flow | non-empty regular file и согласованность с terminal gates |

Artifacts являются обязательными продуктами стадий. Их наличие подтверждает
полноту процесса, но не заменяет executable validation, Git state или SCM state.
`verdict` является agent decision/control signal, а не доказательством качества:
его подтверждают независимая revalidation, repository validation и внешний oracle.

## 5. Выбранная topology

Baseline закрыл ветку fresh retry. Production diff добавляет только отсутствующий
review/repair control flow и result gates:

```text
implement -> implementation artifact + repository gate
        |
        v
validate-agent -> validation.md
        |
        v
initial-verdict (strict deterministic parser)
        |
        +-- PASS -----> review-acceptance-gate -------------------+
        |                                                         |
        +-- BLOCKED --> review-acceptance-gate -> FAIL before PR  |
        |                                                         |
        `-- REPAIR --> repair (one fresh node/session)             |
                           |                                      |
                           v                                      |
                     revalidate-agent -> revalidation.md           |
                           |                                      |
                           v                                      |
                     revalidation-verdict                          |
                           |                                      |
                           +-- PASS -------------------------------+
                           `-- REPAIR|BLOCKED -> FAIL before PR
                                                                     |
                                                                     v
                                                   deterministic validate
                                                                     |
                                                                     v
                                                   create-pr (allowed exit, no retry)
                                                                     |
                                                                     v
                                                   eval PR exactly-once gate
                                                                     |
                                                                     v
                                                   summary + artifact gate
```

### 5.1. Implementation

`implement` и его существующий bounded repository/artifact hook сохраняются без
fresh-retry изменения. Baseline завершил stage с одной попытки и не подтвердил,
что `allow_failure` либо новая retry topology улучшат результат. Prompt по-прежнему
задаёт цель, workspace boundary и обязательный `implementation.md`, но не порядок
исследования, tool choices или token budget.

Отдельная schema/authoring валидация существующего
`hook.on_failure.session: fresh|resume` остаётся contract-hardening, но новый
feature flow на неё не опирается.

### 5.2. Strict review verdict

`validate-agent` независимо проверяет diff, запускает доступные проверки и пишет
`validation.md`. Artifact обязан содержать ровно одну отдельную строку:

```text
verdict: PASS
```

Допустимы только `PASS`, `REPAIR` и `BLOCKED`:

- `PASS` — review не оставил valid actionable findings;
- `REPAIR` — найдены исправимые в текущем scope дефекты;
- `BLOCKED` — безопасное исправление требует недоступной инфраструктуры,
  продуктового решения или расширения scope.

Отдельный deterministic bash node читает файл, требует ровно одну строку по
anchored contract `^verdict: (PASS|REPAIR|BLOCKED)$` и печатает только token как
node output. Missing, duplicate и unknown verdict fail-closed. Downstream `when`
сравнивает только parser output; ни один downstream LLM не интерпретирует
`validation.md` как свободный control text.

Parser имеет `trigger_rule: all_done`, чтобы missing artifact после assistant
failure также дал явный fail-closed result. Repair condition записывается без
новой expression semantics: `when: $initial-verdict.output == "REPAIR"`.

Verdict остаётся решением review-agent, а не proof. `PASS` разрешает лишь переход
к executable gates; качество подтверждают repository validation и внешний oracle.

### 5.3. Единственная repair-попытка

При initial `REPAIR` запускается ровно один новый `repair` assistant node. Новый
node естественно получает fresh session, читает original request и
`validation.md`, перепроверяет findings, исправляет только подтверждённые дефекты,
добавляет regression tests и пишет `review-fixes.md`. Retry или loop отсутствуют.

После repair всегда запускается отдельный review-role `revalidate-agent` с новой
сессией. Он не доверяет repair narrative, повторно проверяет current workspace и
пишет `revalidation.md` с тем же strict verdict contract. Второй deterministic
parser принимает только корректный token и также имеет `trigger_rule: all_done`.
Revalidation nodes существуют только на ветке initial `REPAIR`.

Терминальные ветви фиксированы:

- initial `PASS` — repair и revalidation skipped;
- initial `REPAIR` + revalidation `PASS` — flow продолжает работу;
- initial `BLOCKED` — fail до PR;
- initial `REPAIR` + revalidation `REPAIR|BLOCKED` — fail до PR;
- protocol/assistant failure, missing artifact или invalid verdict на любой стадии
  — fail до PR.

`review-acceptance-gate` имеет `trigger_rule: all_done` и принимает только initial
`PASS` либо пару initial `REPAIR` + revalidation `PASS` при наличии обязательных
artifacts. Затем отдельный `.takt/profiles/code/tools/validate` остаётся финальным
deterministic checkpoint перед SCM.

Внешний mini-du oracle не запускается внутри workflow и не передаёт hidden
feedback агенту. Он остаётся независимой post-run оценкой generalization.

### 5.4. PR publication

`create-pr` не получает blind retry: внешний эффект мог произойти до потери
assistant response. Обычный assistant `exit` допускается только до отдельного
all-done `pr-effect-gate`; timeout, cancellation, protocol и internal failure
остаются terminal. В flow evaluation gate требует fixture SCM state, non-empty
`pr.md`/`pr-url.txt` и ровно один успешный `pr create` для run. Zero либо duplicate
create fail-closed.

Существующий `.takt/profiles/code/tools/require-pr` остаётся fixture-only и в
обычном production workspace является no-op. Exactly-once здесь является
evaluation contract/detection, а не заявлением production exactly-once delivery.
Настоящий provider-independent receipt/reconciliation остаётся вне scope;
будущий production retry допустим только как explicit reconcile operation.

### 5.5. Summary

`summary` сохраняется как отдельная стадия и обязательный `summary.md`, поскольку
задача flow включает handoff, а не только код. Summary gate принимает результат
только после успешных review, deterministic validation и PR effect gates. Retry
для summary baseline не обосновал, поэтому новая retry policy не добавляется.

## 6. Prompt boundary

Prompt каждой assistant стадии содержит только:

- цель стадии;
- user request и доступный deterministic feedback;
- текущий execution workspace и разрешённый artifacts directory;
- обязательные outputs и ограничения безопасности/SCM.

Review и revalidation дополнительно получают закрытый список допустимых verdict и
обязаны записать строгую строку в свой artifact. Repair получает exact initial
review artifact как findings input. Это outcome contract, а не инструкция по
методу работы модели.

Prompt не должен указывать модели, сколько исследовать, когда редактировать,
какими tools пользоваться или как распределять token budget. Bounded behavior
принадлежит runtime policy и gates, а не persuasive prompt text.

## 7. Failure semantics and observability

- Initial/revalidation verdict, parser result и acceptance-gate result являются
  разными фактами и сохраняются отдельно.
- Свободный Markdown вне strict verdict line не управляет DAG. Repair читает его
  как evidence/findings, но branching использует только deterministic parser output.
- Missing, duplicate или unknown verdict является parser failure, а не `BLOCKED`
  и тем более не `PASS`.
- Repair имеет одну execution record; retry/resume и loop отсутствуют.
- `time_to_valid_ms` остаётся доступен только при `valid: true`.
- Eval report/evidence должны позволять отличить: initial `PASS`, repair branch,
  revalidation `PASS`, repeated `REPAIR`, `BLOCKED`, verdict protocol failure,
  deterministic validation failure, missing SCM effect и duplicate PR effect.
- Измерительное поколение **v0** включает 18 прогонов со старой registry.
  Baseline `20260817T210622.749715000Z` с исправленной registry и validator version
  2 является единственным прогоном поколения **v1**.
- Добавление oracle-сценариев и bump mini-du validator `2 -> 3` создаёт поколение
  **v2**. Trend и агрегированное сравнение `v0 <-> v1 <-> v2` запрещены до
  включения registry/oracle identity в machine-readable strategy identity.

## 8. Проверки реализации

### 8.1. Baseline до workflow change — выполнен

Один `implement-basic` выполнен на **текущем неизменённом**
`code:feature-development`, той же модели и исправленной registry. Это не
сравнение с прямым Pi: проверялся production flow, который является предметом eval.

```bash
EVAL_PRESET=qwen38 make eval-feature-smoke
```

Evidence подтвердил `contextWindow=262144`, `maxTokens=64000`, отсутствие retry и
truncation. Исходная матрица решений содержала три ветви:

- `true_accept`: fresh retry для output-limit не обоснован этим case; scope
  сужается до enum-валидации hook session, outcome-observability и только тех
  gates, недостаточность которых показывает baseline;
- повтор прежнего over-investigation/output-limit pattern при исправленных
  limits: fresh restart не считается доказанным лечением; основной вариант
  меняется на explicit repair-role node с теми же result contracts;
- иная terminal причина: обновляются unknowns и topology до production edits.

Фактический результат образовал четвёртую ветвь:

- evaluator сообщил `true_accept`, но durable `validation.md` сообщил `NOT PASS`,
  перечислил actionable defects и прямо передал их отсутствующему repair step;
  одновременно fixture принял два `pr create` внутри одной create-pr попытки.

Эта ветвь выбирает strict verdict -> one repair -> independent revalidation и
exactly-once eval PR gate из §5. Fresh retry исключён из production diff.

### 8.2. Regression tests

Регрессии добавляются до production workflow change.

1. schema и `internal/workflow` отклоняют неизвестное
   `hook.on_failure.session`; разрешены только `fresh|resume`, причём session
   допустима только для `action: retry`;
2. verdict parser принимает ровно одну строку `verdict: PASS|REPAIR|BLOCKED` и
   fail-closed отклоняет missing, unknown, malformed и duplicate verdict;
3. initial `PASS` skips repair/revalidation и допускает final deterministic validate;
4. initial `BLOCKED` safe-stops до PR;
5. initial `REPAIR` запускает ровно один fresh repair node и одну независимую
   revalidation;
6. revalidation `PASS` допускает final deterministic validate;
7. revalidation `REPAIR` и `BLOCKED` safe-stop до PR без второй repair-попытки;
8. repair assistant failure, missing `review-fixes.md`, missing
   `revalidation.md` или invalid second verdict safe-stop до PR;
9. отсутствие PR effect safe-stops без повторного create;
10. assistant `exit` после одного подтверждённого create и корректных PR artifacts
    принимается result gate без повторного SCM call;
11. два `pr create` в одном fixture run отклоняются exactly-once gate;
12. один fixture `pr create` плюс non-empty `pr.md`/`pr-url.txt` принимается;
13. missing/empty/directory artifacts отклоняются соответствующим stage gate;
14. полный fake-agent E2E сохраняет пять стандартных feature artifacts и
    conditional repair/revalidation artifacts, когда выбрана repair branch;
15. mini-du validator version 3 содержит regression scenarios для cross-argument
    hardlink dedup и bare `--`; focused validator tests показывают, что прежний
    baseline patch ими отклоняется;
16. profile compatibility tests фиксируют новую profile version и materialized
    workflow/commands;
17. focused Go suites, `go test ./... -count=1`, race, vet, `make check` и
    `scripts/verify.sh` проходят.

После implementation выполняется реальный Pi smoke поколения v2. Критерий:
flow либо выдаёт validator-v3-accepted результат после initial/revalidation
`PASS`, либо завершает bounded safe-stop с точной terminal причиной. Duplicate
PR effect, свободная интерпретация Markdown verdict и вторая repair-попытка
недопустимы.

## 9. Реестр неизвестных

| ID | Неизвестное | Приоритет | Решение/ограничитель | Статус |
|---|---|---|---|---|
| U-01 | Являются ли artifacts частью продукта flow | P0 | Да: подтверждено workflow commands, production E2E и пользователем | закрыт |
| U-02 | Должен ли assistant exit определять качество workspace | P0 | Нет: executable gates авторитетны; non-exit terminal classes остаются fail-closed | закрыт |
| U-03 | Нужна ли fresh retry topology после исправления registry | P1 | Нет: baseline прошёл без retry/truncation; обнаруженная причина — отсутствующий review->repair transition | закрыт baseline evidence |
| U-04 | Нужно ли семантически парсить Markdown evidence | P1 | Только закрытый control field: deterministic parser читает ровно одну strict verdict line; остальной Markdown не управляет DAG | закрыт дизайном |
| U-05 | Нужно ли передавать hidden oracle feedback внутрь flow | P0 | Нет: это загрязнит benchmark; oracle остаётся post-run | закрыт |
| U-06 | Нужен ли blind retry PR publication | P0 | Нет: неизвестный side effect требует reconcile, текущий срез safe-stops | закрыт |
| U-07 | Сравнимы ли прогоны разных registry/oracle generations | P0 | Нет: 18 старых runs = v0, corrected-registry/validator-2 baseline = v1, validator-3 runs = v2; cross-generation trend запрещён | закрыт процедурой |
| U-08 | Проверяет ли текущий SCM gate production side effect | P1 | Нет, только eval fixture; production receipt/reconcile остаётся явно вне scope | закрыт с ограничителем |
| U-09 | Валидируется ли `hook.on_failure.session` fail-closed | P0 | Пока нет; schema и authoring validation входят в обязательный implementation delta | закрыт дизайном |
| U-10 | Может ли review `NOT PASS` молча продолжить flow | P0 | Нет: strict verdict parser и all-done acceptance gate; `REPAIR/BLOCKED` имеют явные terminal branches | закрыт дизайном |
| U-11 | Достаточно ли покрытие mini-du oracle version 2 | P0 | Нет: baseline выявил cross-argument hardlink и bare `--` gaps; validator 3 добавляет scenarios и создаёт eval generation v2 | закрыт дизайном |
| U-12 | Допустимы ли duplicate `pr create` внутри одной assistant attempt | P0 | Нет в eval: fixture gate требует ровно один successful create; production receipt/reconcile остаётся вне scope | закрыт с ограничителем |

## 10. Слепые зоны и опровержение

- Если downstream branch может получить verdict, не прошедший deterministic parser,
  изменение останавливается до исправления fail-closed contract.
- Если skipped conditional repair/revalidation не позволяет all-done gate надёжно
  отличить `PASS`, `REPAIR` и `BLOCKED`, topology возвращается на review.
- Если repair запускается больше одного раза либо revalidation выполняется в той же
  assistant session, implementation не соответствует bounded design.
- Если PR fixture gate может принять fabricated artifact без SCM effect, сначала
  исправляется SCM gate; artifacts сами по себе недостаточны.
- Если PR fixture gate принимает zero или duplicate create, publication contract
  считается нарушенным независимо от `pr.md`/`pr-url.txt`.
- Если validator 3 не отклоняет сохранённый baseline patch на новых deterministic
  scenarios, oracle change возвращается на пересмотр.
- Если live smoke после repair снова возвращает `REPAIR`, flow обязан safe-stop;
  нельзя скрыто расширять bounded topology второй попыткой или model-specific prompt.

Изменение не требует migration durable run state: новые Runs используют новый
workflow fingerprint, старые evidence/report остаются читаемыми. Откат — возврат
workflow/command profile version; persistence schema не меняется.

## 11. Проектный шлюз

**Статус: CONDITIONAL. Уверенность: высокая.**

Открытых P0 нет. Baseline выполнен, U-03 закрыт отказом от fresh retry, а U-10-U-12
получили fail-closed contracts. Условие перехода к regression tests и production
diff — повторный review этой обновлённой spec. После implementation Pi smoke
поколения v2 закрывает условие перед финальным commit либо фиксирует safe rollback.

## 12. Контракт передачи в реализацию

Соблюдать принятые границы: artifacts и стандартные стадии сохраняются; verdict
является control signal, но не заменяет deterministic proof; hidden oracle не
входит в repair loop; repair выполняется максимум один раз; PR нельзя blind-retry;
новые runtime/YAML semantics не добавляются.

Если обнаружится факт, который меняет эти contracts, persistence, side-effect
semantics или опровергает U-03/U-10/U-11/U-12, остановить реализацию, зафиксировать evidence и
вернуть задачу на `design-unknowns`. Локальные изменения prompts/tests допустимы
только когда они реализуют описанные output contracts и не предписывают модели
метод работы.

Фактический implementation delta:

- workflow schema: `hookDecision.session` получает enum `fresh|resume`;
- workflow authoring: `internal/workflow/validate.go` отклоняет неизвестную
  session и session при `action != retry`;
- schema/authoring contracts: добавляются focused tests для JSON Schema и Go
  validation, а `docs/03-specification.md` фиксирует допустимые значения;
- `implement`: topology baseline сохраняется; fresh/allow-failure retry change не
  добавляется, обязательный `implementation.md` и repository hook остаются;
- `validate-agent`: пишет `validation.md` со strict verdict line;
- `initial-verdict`: новый deterministic parser выдаёт только
  `PASS|REPAIR|BLOCKED` и fail-closed отклоняет иной artifact;
- `repair`: новый conditional implementation node, запускаемый только при
  initial `REPAIR`, без attempts/retry/loop; пишет `review-fixes.md`;
- `revalidate-agent` и `revalidation-verdict`: запускаются только после repair,
  используют новую review session и тот же strict contract в `revalidation.md`;
- `review-acceptance-gate`: all-done принимает initial `PASS` либо
  `REPAIR -> repair -> revalidation PASS`; все остальные ветви fail до PR;
- `validate`: остаётся отдельным финальным deterministic checkpoint после
  review-acceptance-gate;
- `create-pr`: получает `allow_failure: true`, но не retry; только обычный `exit`
  передаёт решение result gate, остальные terminal classes остаются fail-closed;
- `pr-effect-gate`: all-done требует fixture SCM state, ровно один successful
  `pr create`, `pr.md` и `pr-url.txt`; остаётся fixture-only, production receipt
  не заявляется;
- `summary`: получает обязательный non-empty regular `summary.md` gate без новой
  retry policy;
- code profile: при фактическом profile change версия повышается с `0.18.0` до
  `0.19.0`, синхронно обновляются profile README, compatibility/core contract,
  implementation status и changelog; schema-only change до baseline profile
  version не повышает;
- mini-du validator: добавляются cross-argument hardlink и bare `--` scenarios;
  все mini-du suite descriptors, использующие общий validator
  (`feature-development`, `review`, `architect`), повышаются `2 -> 3`, обновляются
  validator contracts/tests и changelog;
- evaluation docs: фиксируются v0/v1/v2 generations и запрет cross-generation
  trend; сохранённые evidence не переписываются.

Не добавлять fresh retry, второй repair, semantic Markdown parser сверх strict
verdict line, model-specific prompting или production SCM reconciliation. Эта
обновлённая spec должна пройти повторный review gate до regression tests и
production edits.
