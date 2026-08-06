# Proposal 001. Простой и надёжный Takt для разных кодинг-агентов

**Статус:** принято к поэтапной реализации  
**Реализованные срезы:** `v0.1.38-alpha`, `v0.1.39-alpha`  
**Область:** пользовательский запуск задач, маршрутизация, роли, skills, адаптеры кодинг-агентов, MCP-поверхности и автономная эксплуатация

## 1. Контекст

Takt вырос из локального workflow runtime в систему с динамическими планами,
доверенными блоками, governed child Runs, host-control, восстановлением и
автономным управлением. Эти возможности полезны, но их прямое отображение во
внешний интерфейс создаёт два риска:

1. пользователь должен понимать слишком много внутренних сущностей;
2. основная модель видит десятки низкоуровневых инструментов и может выбирать
   неправильный уровень управления.

Одновременно анализ материалов о надёжных агентных конвейерах и проекта
KiroCrew подтвердил несколько полезных принципов:

- простые запросы должны оставаться простыми;
- границы стадий и переходы состояния принадлежат runtime, а не prompt;
- кодинг-агент является заменяемой исполнительной средой;
- динамический процесс должен компилироваться в один проверяемый runtime;
- skills содержат знания и процедуры, а действия выполняются через runtime или
  инструменты;
- постоянный daemon полезен как локальный Run Gateway, но Takt не должен
  превращаться в универсальный персональный агентный workspace.

Takt не зависит от Kiro CLI. Целевыми хостами могут быть Pi, OpenCode, Codex,
Oh My Pi, Qwen CLI и другие кодинг-агенты. Поддержка конкретного хоста
осуществляется адаптером, а не изменением workflow.

## 2. Цель

Пользовательский сценарий должен выглядеть так:

```text
/takt <задача>
→ короткий понятный план
→ Go / изменить направление / остановить
→ автономное выполнение
→ вопрос только при материальной развилке
→ итог и доказательства
```

Внутри Takt сохраняет строгие контракты, но пользователь не обязан знать про
`WorkflowPlan`, child Run, lease, revision, failure code, RoleDefinition или
EvidenceManifest.

## 3. Не цели

Этот proposal не вводит:

- зависимость от Kiro CLI;
- второй scheduler или второй формат состояния;
- обязательный Web UI;
- постоянную разговорную память пользователя;
- Slack, Telegram и другие каналы как часть ядра;
- автоматическое изменение доверенных пакетов;
- произвольный Python/JavaScript workflow как основной формат;
- отдельный глобально установленный агент для каждой внутренней роли.

## 4. Архитектурная граница

### 4.1. Кодинг-агент

Кодинг-агент отвечает за фактическое исполнение:

- LLM turn;
- локальную сессию;
- чтение и изменение файлов;
- запуск инструментов;
- поток provider events;
- физическую блокировку инструмента, когда API хоста это поддерживает;
- OS sandbox, когда он предоставляется хостом.

### 4.2. Takt Host Bridge

Тонкая интеграция хоста отвечает за:

- перехват `/takt` до основной модели;
- показ preview, состояния, вопросов и результата;
- создание worker-сессии по запросу Takt;
- передачу событий и tool requests;
- восстановление связи с активным Run;
- применение решения Takt перед tool call и завершением основной сессии.

Bridge не выбирает workflow, роли, skills или уровень проверки.

### 4.3. Takt

Takt владеет:

- семантикой задачи;
- выбором процесса;
- компиляцией плана;
- ролями и набором skills;
- политиками и бюджетами;
- state machine и scheduler;
- retry, replan, pause и recovery;
- validation и evidence;
- terminal transition.

### 4.4. Skills

Skills делятся на три уровня:

1. **Control skill** в основной сессии — кратко объясняет запуск и управление
   Takt. При наличии нативной команды он не является механизмом enforcement.
2. **Role/domain skills** в пакетах Takt — знания и процедуры для конкретной
   worker-сессии.
3. **Детерминированные действия** — команды, scripts, adapters и runtime blocks,
   а не skills-обёртки над CLI.

## 5. Нейтральный AgentSession-контракт

Каноническая граница Takt не должна называться именем конкретного продукта.
Внутренний интерфейс:

```go
type SessionAdapter interface {
    Run(context.Context, Request) (Result, error)
    Capabilities() []string
}
```

Встроенные адаптеры Pi и OpenCode реализуют этот контракт напрямую. Другие
кодинг-агенты подключаются процессной обёрткой по
`takt-assistant/v1alpha2`.

Процессный протокол поддерживает:

- fresh/resume session;
- capability declaration;
- сообщения, usage и diagnostics;
- блокирующий `tool.request → tool.decision`, если адаптер заявляет
  `tool_control`;
- ровно один terminal result.

Профиль использует логическое имя `coding-agent`. Конкретный исполнитель
выбирается в `.takt/config.yaml`:

```yaml
apiVersion: takt/v1alpha1
kind: Config
default_assistant: codex

assistants:
  codex:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [codex-takt-adapter]
```

Workflow не меняется при переходе между Codex, Oh My Pi, Qwen CLI, Pi или
OpenCode.

## 6. Task Router

### 6.1. Назначение

`Task Router` находится внутри Takt и является чистым этапом выбора:

```text
TaskRequest + Profile + Workflow Catalog + Trusted Blocks + capabilities
→ RouteDecision
```

Он не исполняет задачу и не выдаёт новые полномочия.

### 6.2. Гибридная маршрутизация

До вызова модели Takt собирает детерминированные факты:

- доступные workflow;
- подключённые доверенные блоки;
- сигналы публичного API, безопасности, миграции, регрессии и неясного scope;
- максимальные корпоративные ограничения.

Модель решает только семантическую часть:

- какой специализированный процесс соответствует задаче;
- достаточно ли стабильного базового шаблона;
- требуется ли task-specific dynamic composition;
- какие прогрессивные проверки оправданы.

После модели обычный код:

- проверяет JSON-схему;
- проверяет имя workflow и blocks;
- применяет минимальные обязательные controls;
- сужает бюджеты;
- компилирует результат в обычный Takt Workflow.

### 6.3. Три маршрута

Приоритет выбора:

```text
готовый специализированный workflow
→ stable template + параметры
→ bounded dynamic composition
```

#### `workflow`

Выбирается, когда опубликованный процесс точно соответствует требуемому
жизненному циклу: исправление issue, review PR, plan-to-PR и т. п.

#### `template`

`simple-reliable` является стандартным маршрутом обычной разработки:

```text
investigate → implement → validate → independent review
```

К нему прогрессивно добавляются:

- baseline;
- независимое проектирование тестов;
- усиленная adversarial-проверка;
- checkpoint после исследования;
- ограничение параллельности.

#### `dynamic`

Используется только когда специализированный процесс и стабильный шаблон не
выражают нужный fan-out, зависимости или контрольные точки. Dynamic Plan
по-прежнему выбирает только доверенные BlockPackage и компилируется в обычный
DAG Takt.

### 6.4. Отказ роутера

Семантический router не становится новой точкой недоступности. При ошибке
модели, протокола или схемы Takt выбирает:

```text
simple-reliable + inspect_first
```

Fallback фиксируется в durable route и виден в `explain`, но обычная задача не
останавливается только из-за отказа классификатора.

## 7. Прогрессивное усиление процесса

Takt начинает с минимально достаточного процесса. Дополнительные проверки
включаются по фактам, а не по желанию создать максимально строгую схему.

### 7.1. Сигналы до запуска

- публичный API;
- авторизация и безопасность;
- миграции и изменение данных;
- исправление регрессии;
- несколько репозиториев;
- защищённая область проекта;
- необратимое внешнее действие.

### 7.2. Сигналы во время выполнения

- scope существенно расширился;
- одинаковая ошибка повторилась;
- новая ошибка отличается от baseline;
- verifier не может доказать существенный результат;
- требуется изменить пользовательское поведение;
- состояние внешнего side effect неизвестно.

### 7.3. Реакции контроля

Проверка возвращает один из трёх классов:

- `deny` — действие физически запрещено;
- `repair` — Takt создаёт ограниченную попытку исправления;
- `warn` — выполнение продолжается, замечание попадает в итог.

Fail-closed обязателен для безопасности, полномочий, необратимых операций и
целостности state. Рекомендации качества не должны автоматически превращаться в
частые остановки.

### 7.4. Вопрос человеку

Takt спрашивает человека только когда требуется материальное решение:

- изменить публичное поведение;
- выйти в защищённую область;
- выполнить необратимое действие;
- выбрать между несовместимыми трактовками требования;
- принять риск после исчерпания ограниченных попыток.

Каждая остановка объясняет:

1. что произошло;
2. что уже сохранено;
3. какое решение требуется;
4. что произойдёт после ответа.

## 8. Роли и определения агентов

`RoleDefinition` является внутренним контрактом Takt, а не отдельным
пользовательским агентом:

```yaml
name: verifier
context: [task, diff, checks]
permissions:
  filesystem: read-only
  delegation: false
output: verifier-result
model_profile: review
skills: [verify-acceptance]
```

Takt преобразует RoleDefinition в `SessionRequest` для выбранного адаптера.
Временные agent configs допустимы как деталь адаптера, но пользователь не должен
устанавливать глобальный набор `planner`, `implementer`, `test-worker`,
`verifier`.

Ближайший срез не вводит полный RoleDefinition DSL. `v0.1.38` создаёт для него
архитектурную границу через логический `coding-agent`, выбранные команды,
проверяемые outputs и отдельные worker Runs.

## 9. MCP-поверхности

Общее число внутренних операций не является проблемой, если разные потребители
не видят чужой протокол.

```text
takt mcp --surface agent
takt mcp --surface host
takt mcp --surface worker
takt mcp --surface operator
takt mcp --surface all
```

### `agent`

Основная модель видит только:

```text
takt.task.start
takt.task.status
takt.task.respond
takt.task.stop
takt.task.explain
```

### `host`

Только интеграция хоста видит managed-session и guard API.

### `worker`

Только внешний исполнитель видит claim, events, tool lifecycle и terminal
submission.

### `operator`

CLI/сопровождение видит workflow, plans, Runs, blocks и notifications, но не
host/worker протоколы.

Поверхность задаётся конфигурацией подключения, а не аргументом отдельного tool
call, поэтому модель не может повысить свою роль.

## 10. Autopilot UX

Из KiroCrew принимается идея простого plan/approve/execute интерфейса и
runtime-owned stage loop. Не принимается разбор произвольного плана из текста
модели: Takt уже имеет структурированный RouteDecision и DAG.

Пользователь видит:

```text
Задача: исправить повторную отправку запроса
Маршрут: стандартная разработка
Этапы: исследование → изменение → проверки → ревью
Дополнительный контроль: baseline из-за регрессии

Go / изменить направление / отменить
```

`Go All` является свойством пользовательского интерфейса: оно разрешает
автоматически проходить обычные межстадийные паузы, но не отменяет
security/force approval.

## 11. Один orchestration runtime

Takt не повторяет архитектуру, где TaskRunner, Autopilot и Dynamic Workflows
имеют независимые state machines.

Все входы компилируются в один runtime:

```text
specialized workflow ─┐
simple template ──────┼→ takt/v1alpha1 Workflow → scheduler → Run
Dynamic WorkflowPlan ─┘
```

Это сохраняет единые:

- fingerprints;
- retries;
- hooks;
- budgets;
- pause/resume/recovery;
- events и artifacts;
- parent/child lifecycle;
- terminal semantics.

## 12. Что берётся из KiroCrew

Анализировался публичный репозиторий `kirodotdev/KiroCrew`, в частности:

- `README.md`;
- `docs/architecture/overview.md`;
- `docs/system-specs/modules/taskrunner.md`;
- `docs/system-specs/modules/autopilot.md`;
- `docs/system-specs/modules/workflows.md`;
- `docs/system-specs/modules/workflow-gates.md`;
- `docs/system-specs/modules/memory-skills-hooks.md`.

Полезные решения:

- разделение agent runtime и gateway;
- постоянные управляемые сессии;
- простой Autopilot UX;
- host-owned stage boundaries;
- bounded retries и escalation;
- typed event journal;
- capability ports;
- именованные conformance gates;
- skills on demand;
- staged skill proposals;
- удобное наблюдение за фоновыми задачами.

## 13. Что из KiroCrew не переносится

- зависимость от `kiro-cli` и ACP как единственного backend;
- несколько параллельных orchestration engines;
- координация, определяемая системным prompt основного агента;
- глобальная разговорная память и embeddings как обязательная часть Takt;
- messaging gateway и Apps platform;
- автоматическое включение сгенерированных skills;
- свободный Python workflow как основной путь.

ACP может стать одним из адаптеров `SessionAdapter`, но не каноническим
требованием Takt.

## 14. Conformance gates

Ключевые инварианты получают стабильные идентификаторы:

| Gate | Гарантия |
|---|---|
| `RT-01` | Любой маршрут компилируется в обычный Takt Workflow |
| `RT-02` | Неизвестный workflow или block отклоняется до выполнения |
| `RT-03` | Отказ semantic router приводит к inspect-first fallback |
| `PC-01` | Детерминированные сигналы могут только усилить controls |
| `AS-01` | `coding-agent` разрешается через явный default или безопасную совместимость |
| `AS-02` | Adapter заявляет только фактически обеспечиваемые capabilities |
| `MCP-01` | Agent surface содержит только пять task tools |
| `MCP-02` | Host/worker операции недоступны на agent surface |
| `RUN-01` | Только Takt переводит узел и Run в terminal state |
| `UX-01` | Остановка содержит понятное безопасное продолжение |

Gate считается реализованным только при наличии теста, который падает при
нарушении инварианта.

## 15. Authoring и routing evals

Надёжность роутера измеряется на реальных обезличенных задачах:

- доля правильного специализированного workflow;
- доля приемлемого template fallback;
- доля неоправданных Dynamic Plans;
- доля валидных RouteDecision с первой попытки;
- ложные усиления baseline/independent tests/enhanced review;
- пропущенные protected-сигналы;
- число вопросов пользователю;
- стоимость и время маршрутизации;
- итоговый success, а не только качество классификации.

Router не должен обучаться на собственных выводах без подтверждённого исхода.

## 16. Skill learning loop

Повторяющиеся успешные последовательности могут создавать предложение:

```text
несколько Run повторили один паттерн
→ candidate skill/block
→ provenance + примеры + ожидаемая польза
→ pending review
→ ручное принятие в пакет
```

Автоматическая публикация в trusted package запрещена. Скриптовые кандидаты
всегда требуют проверки. Неиспользуемые автоматически предложенные материалы
должны архивироваться, а не бесконечно накапливаться.

## 17. Реализация в v0.1.38-alpha

Первый срез включает:

- `TaskRoute` и гибридный Task Router;
- маршруты `workflow|template|dynamic`;
- стабильный `simple-reliable` template;
- controls `inspect_first|baseline|independent_tests|enhanced_review|max_parallel`;
- детерминированное монотонное усиление controls;
- безопасный template fallback при отказе router;
- новые trusted blocks `baseline` и `test-design`;
- компактный Task API в CLI, daemon и MCP;
- role-based MCP surfaces;
- логический `coding-agent` и `default_assistant`;
- процессный `takt-assistant/v1alpha2` как нейтральный seam для Codex,
  Oh My Pi, Qwen CLI и других внешних адаптеров;
- схему `TaskRoute` и сквозной contract test.

## 18. Реализация в v0.1.39-alpha

Второй срез реализует внутренний `RoleDefinition`, bounded `TaskBrief`, scope `expected|allowed|protected|forbidden`, required/preferred checks и реакции `deny|repair|warn`. Required technical failure получает одну автоматическую repair-итерацию с повторной независимой проверкой; повторный отказ переводит процесс в `waiting` с одним материальным вопросом. Read-only роли получают реальную adapter policy. Для mutating-ролей `changed_files` сверяется с фактическим Git diff managed worktree, а automatic repair продолжает тот же execution workspace; это даёт проверяемую границу результата без ложного обещания path-level OS sandbox.

Одновременно исправлены критичные pause/recovery/notification дефекты v0.1.37 и compact Task API v0.1.38. Полный механизм описан в `docs/53-role-brief-controls-v0.1.39.md`.

## 19. Реализация в v0.1.40-alpha

Третий срез добавляет внутренний `EvidenceManifest`: baseline provenance, stable failure fingerprints, check-to-evidence mapping и verdict, связанный с candidate content SHA-256. Изменение execution workspace после verdict переводит его в `stale`. Known baseline failure сохраняется как evidence и не запускает automatic repair; новая проблема идёт по обычному failure routing.

Материальная остановка получает `parked` с компактным failure code, owner, `safe_next_action` и `unsafe_to_repeat`; Task API и attention скрывают внутреннюю механику и показывают пользователю только необходимость решения. External executor получил `side_effect.mode: reconcile`: неизвестный исход внешней мутации блокирует blind retry до явного `not_applied|applied|unknown`, а подтверждённый `applied` требует receipt и завершает node через обычный submit path.

## 20. Следующие срезы

### 20.1. Agent + Domain Adapter SDK

- готовые wrappers для востребованных Codex/Oh My Pi/Qwen CLI;
- общий capability/session/tool-event conformance kit и live smoke на зафиксированных версиях;
- нейтральные SCM/tracker/CI capability contracts;
- typed inputs/results/errors, idempotency и capability discovery;
- GitHub/GitLab reference adapters и fake corporate backends без платформенных имён в workflow.

### 20.2. Skill proposals и eval loop

- анализ повторяющихся Run;
- pending proposals;
- измерение эффекта после принятия;
- отсутствие автоматической мутации доверенных пакетов.

## 21. Критерий принятия дальнейших функций

Новый механизм добавляется, когда:

1. предотвращает конкретный наблюдавшийся класс ошибок;
2. не требует настройки для обычной задачи;
3. не добавляет новый публичный tool без необходимости;
4. техническую проблему можно исправить без остановки пользователя;
5. остановка содержит одно понятное решение и безопасное продолжение;
6. механизм имеет conformance test и измеримую пользу;
7. его можно оставить предупреждением, если цена ошибки невысока.

Итоговая цель — не максимально сложный agent framework, а минимально заметный
control plane, который автоматически усиливает процесс там, где цена ошибки
этого требует.
