# Human-reviewed Skill/Block Learning Loop — v0.1.51-alpha

`v0.1.51-alpha` открывает P3 отдельным control-plane контуром обучения поверх уже накопленной истории Run. Runtime, scheduler и workflow DSL не получают нового типа узла.

## Цель

Повторяющийся устойчивый сигнал из нескольких Run можно превратить в проверяемое предложение reusable skill или block:

```text
Run history
  ↓
takt learn scan
  ↓
repeated diagnostic / repeated successful workflow
  ↓
immutable candidate snapshot + provenance
  ↓
human review
  ↓
existing evaluation matrix + regression gates
  ↓
staged ready candidate
  ↓
explicit manual installation / adoption
```

Takt не публикует и не устанавливает learned candidate автоматически.

## Durable contract

Proposal хранится в:

```text
.takt/learning/proposals/<proposal-id>/
  proposal.json
  candidate/
  evaluation/report.json
```

После успешного review и evaluation кандидат копируется в:

```text
.takt/learning/ready/<proposal-id>/
```

Формат `proposal.json` — `takt-learning/v1alpha1` / `LearningProposal`; схема опубликована как `schemas/learning-proposal.schema.json`.

Proposal фиксирует:

- вид и fingerprint повторяемого pattern;
- список supporting Run IDs и их количество;
- snapshot кандидата и SHA-256;
- ожидаемую пользу;
- решение человека и обоснование;
- SHA-256 evaluation report, `matrix_fingerprint`, `benchmark_id`, число gates и итог PASS/FAIL;
- staged path после успешного gate.

## Поиск повторяемых сигналов

```bash
takt learn scan --workspace . --min-runs 2
```

В `v0.1.51` scan намеренно консервативный. Он группирует только уже имеющиеся устойчивые идентификаторы:

1. одинаковый durable `DiagnosticState.fingerprint` в разных Run;
2. одинаковый `workflow_fingerprint` у успешно завершённых Run.

Один Run не может сам создать learning proposal: `min-runs` не меньше `2`, а внутри одного Run одинаковый diagnostic fingerprint считается один раз.

## Proposal

Для skill можно передать готовый `SKILL.md` либо создать нейтральный draft из provenance:

```bash
takt learn propose \
  --workspace . \
  --pattern diagnostic:sha256:... \
  --kind skill \
  --name validation-recovery \
  --benefit "reduce repeated validation failures"
```

Готовый skill:

```bash
takt learn propose \
  --pattern diagnostic:sha256:... \
  --kind skill \
  --name validation-recovery \
  --candidate ./candidate/SKILL.md \
  --benefit "reduce repeated validation failures"
```

`SKILL.md` должен иметь frontmatter с непустыми `name`/`description`; `name` обязан совпадать с именем proposal.

Для block требуется существующий `BlockPackage` `package.yaml`. Пакет проверяется текущим block catalog loader, должен содержать ровно один block с указанным именем, затем целиком snapshot-ится в proposal. Symlink и non-regular files в snapshot запрещены.

## Human review

```bash
takt learn review learn-... \
  --decision accept \
  --reason "pattern is reusable and candidate is scoped"
```

или:

```bash
takt learn review learn-... \
  --decision reject \
  --reason "case-specific behavior"
```

Без `accept` staging невозможен. Решение и rationale становятся durable частью proposal.

## Evaluation gate

Принятый candidate должен пройти существующий workflow-level или task-level matrix benchmark:

```bash
takt learn evaluate learn-... --report ./evaluation/benchmark.json
```

Допустимы только:

- `takt-evaluation-matrix/v1alpha1`;
- `takt-task-evaluation-matrix/v1alpha1`.

Report должен содержать `matrix_fingerprint`, `benchmark_id` и хотя бы один gate. `passed=true` недостаточно: каждый gate тоже обязан быть `passed=true`.

Evaluation report snapshot-ится рядом с proposal и хешируется. FAIL переводит proposal в `evaluation_failed`; candidate можно доработать как новый proposal либо повторно провести осмысленное human review перед новой evaluation.

## Staging и trust boundary

```bash
takt learn stage learn-...
```

Перед staging Takt повторно считает SHA-256 candidate snapshot. Любое изменение после proposal/review блокирует staging.

`stage` не меняет:

- `.takt/packages`;
- `package-lock`;
- profile `block_packages`;
- global/corporate package scopes;
- assistant skill configuration.

Он создаёт только reviewed/evaluated candidate в `.takt/learning/ready/<proposal-id>`. Его дальнейшее подключение остаётся отдельным явным действием пользователя/команды.

## CLI

```text
takt learn scan
takt learn list
takt learn get <proposal-id>
takt learn propose ...
takt learn review <proposal-id> ...
takt learn evaluate <proposal-id> --report ...
takt learn stage <proposal-id>
```

JSON-вывод использует общий стабильный CLI envelope `{"ok":true,"result":...}`.

## Исправления correctness после v0.1.50

Релиз также закрывает замечания ревью `v0.1.50-alpha`:

- fork Dynamic Plan сохраняет исходный structured `task_source` и immutable source provenance;
- resume после durable `loop.iteration.completed` с `satisfied=true` завершает loop без новой итерации и повторных side effects, включая boundary `MaxIterations`;
- legacy loop state без `loop_iterations` проверяется реальным resume-тестом;
- MCP Domain Adapter использует bounded default timeout и текущую версию Takt в handshake;
- Task Source README больше не использует отсутствующий `task start --config`;
- compatibility mini-validator fail-closed на неизвестных schema keywords и использует JSON numeric equality для `uniqueItems`;
- meta-schema `schema-subset-v1.schema.json` сравнивается с `schemasubset.Description()` contract-тестом;
- `verify-manifest.sh` входит в `make check`;
- пустой assistant `event_types` закреплён как deny-all;
- GitHub SCM repository discovery имеет bounded timeout;
- reference GitHub Task Source принимает URL только `github.com`, вместо потери GitHub Enterprise host в provenance;
- CLI JSON envelope закреплён отдельной schema и тестом;
- Task Source получил unit-test на control boundary.

## Release gate

Новый самостоятельный gate:

```bash
./scripts/test-learning-loop.sh
```

Он проверяет полный путь `scan → propose → human accept → evaluation → stage`, запрет staging до gates и отсутствие автоматической установки trusted package.
