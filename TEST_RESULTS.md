# Результаты проверок v0.1.20-alpha

## Реализованный срез

- OpenCode сохраняет provider retry и connection diagnostics при timeout/cancellation;
- итоговая классификация остаётся `timed_out` или `cancelled`;
- raw stdout/stderr, logical output и per-attempt error сохраняют доступную первопричину;
- scheduler не заменяет специализированную context-ошибку OpenCode общим `node attempt`;
- Takt authoring skill обновлён до v0.2.1;
- версия скилла и поддерживаемая версия Takt проверяются автоматически.

## Контрактный сценарий provider timeout

Fake OpenCode:

1. пишет в stderr сообщение `provider endpoint unavailable; retrying request 2/3`;
2. публикует JSON `error` event с `connection refused`;
3. продолжает выполнение до parent timeout.

Adapter и runtime подтверждают:

- execution kind `timed_out`;
- оба сообщения присутствуют в `NodeState.error` и logical output;
- raw NDJSON остаётся в `NodeState.stdout`;
- stderr OpenCode остаётся в `NodeState.stderr`;
- запись `NodeState.executions[]` сохраняет ту же классификацию и диагностику.

## Полный рабочий прогон

Успешно выполнены:

```text
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
make check
./scripts/verify.sh
./scripts/test-fake-assistant.sh
./scripts/test-pi-adapter.sh
./scripts/test-opencode-adapter.sh
./scripts/test-route-dsl-e2e.sh
./scripts/test-route-dsl-eval.sh
./scripts/test-takt-skill.sh
./scripts/check-docs.sh
```

OpenCode contract suite теперь включает scheduler-level provider timeout. Обычные fresh/resume, usage, error event, cancellation, overflow и context-priority сценарии также прошли.

## Внешние проверки

Реальный OpenCode smoke для v0.1.20-alpha в среде сборки не запускался: внешний бинарник, credentials и provider endpoint не были доступны. Проверка остаётся opt-in через `TAKT_OPENCODE_SMOKE=1`.
