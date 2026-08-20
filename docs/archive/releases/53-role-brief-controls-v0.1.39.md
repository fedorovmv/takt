# Role Contract, Brief Compiler и управляемые проверки — v0.1.39-alpha

## Назначение

`v0.1.39-alpha` реализует первый приоритет proposal 001 без усложнения обычного пользовательского сценария. Пользователь по-прежнему запускает задачу через `takt task start` или `/takt`; роли, brief, path scope и реакции проверок компилируются внутри Takt из доверенного пакета.

Срез одновременно закрывает критичные дефекты Autonomous Run Operations `v0.1.37-alpha` и компактного Task API `v0.1.38-alpha`.

## 1. Внутренний Role Contract

`BlockPackage` может объявлять `roles`. Роль задаёт:

- назначение;
- профиль модели;
- `fresh|shared` session;
- рецепт контекста и его верхнюю границу;
- области `expected|allowed|protected|forbidden`;
- дополнительную node policy, когда адаптер действительно способен её обеспечить.

Блок ссылается на роль по имени. Catalog проверяет существование роли и соответствие `model_profile`/`session` фактическому workflow блока. Это не создаёт отдельного типа агента в Pi/OpenCode/Codex/Qwen CLI: Takt формирует execution request для обычной worker-сессии.

Роли встроенного `code-core`:

- `baseline-observer`;
- `investigator`;
- `implementer`;
- `test-designer`;
- `validator`;
- `verifier`.

Read-only роли получают `sandbox.filesystem: read_only`. Для ролей, меняющих код, Takt не заявляет несуществующий path-level OS sandbox: текущий assistant policy умеет гарантировать read-only, но не ограниченную запись по маске. Поэтому write scope проверяется в два слоя: worker обязан вернуть структурированный `changed_files`, а Takt сверяет этот список с фактическим Git diff managed worktree. Незаявленный фактический файл блокирует принятие сегмента. Настоящий OS/path sandbox остаётся отдельным runtime/security направлением.

## 2. TaskBrief

Перед каждой trusted worker-фазой Takt компилирует новый `TaskBrief`:

```json
{
  "apiVersion": "takt/v1alpha1",
  "kind": "TaskBrief",
  "role": "implementer",
  "goal": "...",
  "objective": "...",
  "signals": ["..."],
  "scope": {
    "expected": ["..."],
    "allowed": ["**"],
    "protected": ["api/**"],
    "forbidden": [".git/**", ".takt/**"]
  },
  "context": {
    "prior_results": {},
    "signals": [],
    "scope": {}
  }
}
```

Brief компилируется для каждой worker-фазы и получает только данные, перечисленные `context.include`. Повторная attempt того же узла использует тот же immutable bounded brief в новой session согласно role contract; automatic repair создаёт новую worker-фазу и новый brief с конкретной диагностикой предыдущего check failure. `prior_results` обрезаются по `max_chars`; полный transcript основной сессии автоматически не наследуется.

`expected` может выводиться из явно названных путей задачи и служит диагностикой. `allowed` является фактической границей роли. `protected` не останавливает задачу само по себе, но сохраняет предупреждение и требует предусмотренных workflow проверок. `forbidden`, абсолютный путь и `..`-escape блокируют принятие результата.

Машиночитаемая схема: `schemas/task-brief.schema.json`.

## 3. Required/preferred checks и реакции

Trusted block может объявить структурированные checks:

```yaml
checks:
  - name: deterministic-validation
    path: passed
    level: required
    reaction: repair
```

Уровни:

- `required` — обязательный контроль;
- `preferred` — желательная дополнительная проверка; её отказ сохраняется как warning и не блокирует обычную задачу.

Реакции:

- `deny` — результат не принимается и процесс завершается ошибкой;
- `repair` — Takt самостоятельно запускает одну ограниченную repair-итерацию и повторяет независимые проверки;
- `warn` — замечание сохраняется, выполнение продолжается.

`code-core` использует `repair` для deterministic validation и independent review. Adversarial review является `preferred + warn`, чтобы усиленная проверка не превращала типовой процесс в частый источник блокировок.

## 4. Ограниченный automatic repair

При первом required-check failure с `reaction: repair` Takt:

1. сохраняет исходный failed check;
2. запускает trusted `implement` как `auto-repair-N` только с конкретной диагностикой;
3. продолжает работу в том же managed execution workspace, поэтому repair видит уже сделанные изменения вместо нового чистого checkout;
4. повторяет check-bearing блоки исходного сегмента отдельными fresh sessions;
5. возвращается к сохранённому хвосту исходного плана;
6. не расходует revision budget semantic replanner.

На один устойчивый ключ `block:check` допускается одна автоматическая repair-итерация. Повторный отказ переводит план в `waiting` и формирует один материальный вопрос: выбрать другой подход, явно принять остаточный риск либо остановить задачу.

История check results и число repair attempts сохраняются в Dynamic Plan record.

## 5. Исправления Autonomous Run Operations

Закрыты критичные дефекты `v0.1.37-alpha`:

- pause marker не уничтожается `Resume`/recovery до фиксации durable `paused`;
- ошибочный `ResumePaused` сначала проверяет статус и не меняет дерево;
- parent, запаузенный в `waiting(child_run)`, остаётся paused после ответа/завершения ребёнка и корректно продолжается только через явный resume;
- pause перепроверяется перед каждым sequential node и каждой новой attempt, поэтому external node не становится claimable после запроса pause;
- `question.required` имеет реального producer через approval с `capture_response`;
- notification dedup включает waiting kind/node/revision и не теряет повторные approval/question;
- desktop/process sinks исполняются с timeout;
- notification dispatch сериализован межпроцессным lock, первый запуск создаёт baseline без ретроспективного спама, inbox ограничен, IDs устойчивы к сбою между persist и snapshot;
- foreground `run recover` продолжает восстановленный Run до следующей durable границы вместо запуска фоновой goroutine, погибающей вместе с CLI;
- retry сохраняет `Executions` и artifacts предыдущих attempts;
- retry из `cancelled` начинает с первого незавершённого узла;
- recursive summary терпит краткое окно между link child ID и публикацией child state;
- ошибки persistence operator markers больше не маскируются;
- fork сохраняет source provenance и fingerprint.

## 6. Исправления Task API

Закрыты дефекты `v0.1.38-alpha`:

- `task respond <plan-id> --action answer` находит waiting Run и ожидающий node, а не отправляет ответ semantic replanner;
- `task stop <plan-id>` синхронно reconcile-ит plan в `abandoned` и без daemon;
- пустой `answer` отклоняется вместо подстановки `approved`;
- отсутствующий semantic router приводит к stable `simple-reliable` fallback;
- router fallback сохраняет диагностику и не поглощает cancellation контекста;
- `task start` читает файл только через явный `--file`;
- risk signals используют границы терминов и не принимают `author` за `auth`, `debug` за `bug`;
- `coding-agent` резолвится через единый `assistant.Factory.Resolve`;
- прямой `takt run` выполняет тот же capability preflight, что `validate`;
- пустой MCP surface теперь означает `agent`, тогда как `New()` явно сохраняет исторический `all` для внутренней совместимости;
- protocol docs различают single-result `v1alpha1` и event NDJSON `v1alpha2`.

## 7. Пользовательская сложность

Новые сущности не становятся обязательными командами или проектным YAML пользователя. Обычный проект после `takt init code` по-прежнему требует только описание задачи.

Пользователь видит:

```text
задача
→ короткий план
→ выполнение
→ автоматическое исправление технического сбоя, если безопасно
→ один вопрос только после исчерпания ограниченного repair либо при материальной развилке
→ результат
```

`TaskBrief`, role, checks, warnings и repair history доступны через `task explain` для диагностики.

## 8. Ограничения

- write scope для mutating coding-agent sessions проверяется по структурированному `changed_files` и фактическому Git diff managed worktree; это post-action integrity gate, а не OS sandbox;
- `expected` сейчас строится из role/package и явно названных путей; полноценное уточнение scope по evidence исследования остаётся будущим улучшением;
- acceptance-to-evidence mapping и candidate SHA binding относятся к следующему приоритету;
- bundled Pi/OpenCode host integrations остаются `guarded` до live smoke на реальных зафиксированных версиях;
- готовые wrappers Codex/Oh My Pi/Qwen CLI не входят в поставку, используется нейтральный `takt-assistant/v1alpha2` contract.
