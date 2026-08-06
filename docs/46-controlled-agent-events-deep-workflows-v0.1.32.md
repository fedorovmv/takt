# Управляемые события агента и глубокие процессы разработки — v0.1.32-alpha

## Назначение среза

Релиз завершает два связанных направления:

1. нормализованный жизненный цикл сессий, инструментов, артефактов, usage и diagnostics;
2. перевод шести основных процессов профиля `code` из компактных каркасов в проверяемые инженерные процессы.

Реализация сохраняет одну модель исполнения. Tool control внешнего worker, встроенные adapters, governed child Runs, артефакты и workflow checkpoints записываются в обычный Run state/event log и используют существующие retry, hooks, fingerprints и recovery semantics.

## 1. Agent event protocol v2

Канонические события:

```text
assistant.session.started
assistant.session.resumed
assistant.message
assistant.tool.requested
assistant.tool.allowed
assistant.tool.denied
assistant.tool.started
assistant.tool.completed
assistant.artifact.declared
assistant.usage
assistant.diagnostic
assistant.completed
assistant.failed
```

Событие содержит `run_id`, `node_id`, `attempt`, `session_id`, sequence и временную метку на уровне Run event envelope. Tool events дополнительно используют устойчивый `call_id`; объявление артефакта может ссылаться на тот же `call_id`.

Raw stdout/stderr остаются отдельными полями результата. Нормализованные события не подменяют и не переформатируют исходный поток провайдера.

## 2. Capability declaration

Worker или adapter объявляет:

- версию event protocol;
- список общих capabilities;
- поддерживаемые типы событий;
- наличие session events;
- наличие tool events;
- возможность блокирующего tool control;
- artifact events;
- usage events.

Workflow с обязательным контролем инструмента требует `tool_control`, `tool_events` и `agent_events_v2`. Неподдерживаемая гарантия отклоняется до выполнения инструмента.

OpenCode и Pi публикуют наблюдательные события, но не заявляют перехват tool call до выполнения. Полный сохраняемый tool lifecycle реализован внешним executor. Process protocol `takt-assistant/v1alpha2` поддерживает двунаправленный запрос решения, когда вызывающая сторона передала `ToolController`.

## 3. Блокирующий tool lifecycle

Внешний worker выполняет следующий протокол:

1. `takt.node.tool.request` сохраняет запрос до запуска инструмента.
2. Takt проверяет effective node policy.
3. Запрещённый инструмент получает `assistant.tool.denied` без запуска.
4. При `tool_approval.mode: required` вызов переходит в `waiting_approval`.
5. `takt.node.tool.decide` фиксирует `allow` либо `deny`.
6. Worker может вызвать `takt.node.tool.start` только после `allow`.
7. `takt.node.tool.complete` фиксирует результат, failure либо подтверждённую отмену.
8. Внешний узел нельзя завершить, пока любой tool call находится в `requested`, `waiting_approval`, `allowed`, `running` или `cancel_requested`.

`cancel` отдельного вызова:

- до старта переводит его в `cancelled`;
- во время выполнения сохраняет `cancel_requested`;
- worker обязан остановить действие и подтвердить terminal-состояние через `tool.complete`.

Это предотвращает ложное завершение узла при ещё работающем или ожидающем инструменте.

## 4. Артефакты и call provenance

`takt.node.artifact.declare` принимает только файл внутри execution workspace либо каталога артефактов Run. Takt копирует файл в Run store и сохраняет:

- semantic type;
- MIME;
- SHA-256;
- размер;
- producer Run/Node/attempt;
- `call_id` породившего tool call.

После регистрации создаётся `assistant.artifact.declared`. Ссылка на артефакт остаётся доступной через state, CLI и MCP.

## 5. MCP worker plane

Локальный MCP публикует 22 инструмента. К прежнему control plane добавлены:

```text
takt.node.tool.request
takt.node.tool.decide
takt.node.tool.start
takt.node.tool.complete
takt.node.tool.get
takt.node.tool.cancel
takt.node.artifact.declare
```

Claim token и lease остаются обязательными для действий worker. Controller может принять решение и отменить tool call без владения claim token, поскольку эти операции представляют управляющую сторону Takt.

## 6. Точные входы шести процессов

Следующие workflow принимают JSON и проверяют его до создания Run:

- `fix-github-issue`;
- `idea-to-pr`;
- `plan-to-pr`;
- `smart-pr-review`;
- `piv-loop`;
- `ralph-dag`.

Контракты запрещают неизвестные поля, задают обязательные repository/issue/PR/plan/PRD параметры и проверяют непустые уникальные validation commands. Ошибка входа возникает до вызова модели и изменения Git workspace.

## 7. Специализированные фазы и обязательные артефакты

Профиль `code` 0.9.0 добавляет предметные команды для:

- Git preparation;
- issue intake, root cause, reproduction и fix plan;
- idea research и idea plan;
- plan intake и implementation;
- PR review intake, synthesis и fixes;
- PIV exploration, plan и implementation;
- Ralph backlog preparation, story execution и summary;
- validation, recovery, PR finalization и final summary.

Каждая значимая фаза возвращает строгий checkpoint JSON:

```json
{
  "status": "ready|blocked|failed",
  "code": "DOMAIN_CODE",
  "summary": "...",
  "evidence": ["..."],
  "artifact_path": "..."
}
```

И одновременно публикует типизированный артефакт. Среди типов: `issue-intake`, `investigation`, `reproduction`, `fix-plan`, `git-state`, `idea-research`, `plan-confirmation`, `implementation-report`, `validation-report`, `recovery-report`, `review-scope`, `review-report`, `pr-metadata`, `ralph-backlog`, `ralph-progress`, `workflow-summary`.

## 8. Предметные ошибки

Процессы не ограничиваются общими `failed`/`exit`. Checkpoint schema допускает только известные коды конкретной фазы, например:

```text
ISSUE_NOT_FOUND
ISSUE_NOT_REPRODUCED
ROOT_CAUSE_UNCONFIRMED
PLAN_STALE
PLAN_SCOPE_AMBIGUOUS
GIT_DIRTY
GIT_BRANCH_MISMATCH
VALIDATION_FAILED
RECOVERY_REQUIRES_HUMAN
PR_BASE_STALE
PR_DIFF_SCOPE_VIOLATION
REVIEW_EVIDENCE_INCOMPLETE
RALPH_STORY_BLOCKED
```

Код сохраняется в структурированном output и типизированном артефакте, поэтому downstream gate и recovery не разбирают свободный текст.

## 9. Git decision trees

`git-prepare` и `pr-finalize` задают явные решения:

- managed worktree использует текущую ветку без переключения;
- чистая base branch допускает создание рабочей ветки;
- dirty base, неверная ветка, отсутствующий remote/base блокируют процесс;
- push и PR выполняются только после успешной validation gate;
- stale base, конфликт, scope drift и отсутствие доказательств получают отдельные коды;
- служебные артефакты Takt не включаются в продуктовый diff.

## 10. Recovery semantics

`fix-github-issue`, `idea-to-pr`, `plan-to-pr`, `piv-loop` и `ralph-dag` используют одинаковую проверяемую схему:

```text
implement
→ validate
→ при failure: recover
→ revalidate
→ validation gate
```

Recovery обязан вернуть отдельный `recovery-report`. Если исправление выходит за scope либо требует решения человека, процесс завершает recovery checkpoint соответствующим предметным кодом, а не продолжает PR-фазу.

Review block также создаёт отдельные perspective reports, synthesis, optional fix report и validation evidence.

## 11. Сквозной Git/GitHub стенд

`scripts/test-deep-code-workflows.sh` создаёт:

- временный настоящий Git-репозиторий;
- bare remote;
- изолированный fake `gh`;
- детерминированный process adapter `takt-fake-code-agent`.

Контракт проверяет:

1. полный успешный `fix-github-issue` с созданием обязательных артефактов, коммитом, push и PR;
2. `plan-to-pr`, где первая validation предсказуемо падает, затем выполняются recovery и успешная повторная проверка;
3. интерактивный `idea-to-pr` с реальным approval/resume и созданием PR;
4. `smart-pr-review` существующего fake PR с динамическими governed reviewers;
5. интерактивный `piv-loop` с exploration, plan approval, implementation, validation, review и acceptance;
6. `ralph-dag` с backlog/story loop, validation, push и PR;
7. предметный checkpoint `blocked`, который останавливает downstream-фазы до implementation/validation/PR и не изменяет Git;
8. отклонение некорректного JSON input до исполнения;
9. сохранение evidence и предметных checkpoint codes.

Fake GitHub нужен только тесту и не является production adapter.

## 12. Ограничения

- Перехват tool call OpenCode/Pi до исполнения невозможен через их текущие CLI/RPC-контракты; они остаются observational adapters.
- `cancel_requested` требует подтверждения worker; Takt не может самостоятельно убить удалённый/внешний инструмент.
- Глубокие workflow проверяют локальную orchestration/Git семантику через fake `gh`; реальная GitHub-сеть остаётся внешней интеграцией.
- Формат checkpoint пока описывается повторяемыми JSON schemas workflow, а не отдельным глобальным `checkpoint` типом схемы.
