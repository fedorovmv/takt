# Результаты проверок v0.1.19-alpha

## Реализованный срез

- специализированный OpenCode adapter через `opencode run --format json`;
- model, agent, variant и auto mapping;
- fresh/resume с проверкой Session ID;
- assistant version, requested/resolved model и per-attempt usage/cost;
- OpenCode error events, stderr diagnostics, timeout/cancel и output limit;
- fake OpenCode contract suite и opt-in real smoke;
- OpenCode-профиль в Takt authoring skill v0.2.0.

## Полный рабочий прогон

Успешно выполнены:

```text
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
make check
./scripts/verify.sh
./scripts/test-fake-assistant.sh
./scripts/test-pi-adapter.sh
./scripts/test-opencode-adapter.sh
./scripts/test-route-dsl-e2e.sh
./scripts/test-route-dsl-eval.sh
./scripts/test-takt-skill.sh
./scripts/check-docs.sh
takt validate examples/opencode-smoke/workflow.yaml
```

Проверены все JSON Schema. `examples/opencode-smoke/config.yaml` и config стартового skill-профиля проходят Draft 2020-12 schema validation.

## OpenCode contract suite

Покрыты:

- prompt/workspace/model/agent/variant/auto/env mapping;
- version probe;
- fresh и подтверждённый resume;
- resume mismatch;
- текст, tool events, per-step usage и cost;
- предупреждения в stderr;
- OpenCode error event при OS exit 0;
- ненулевой OS exit;
- malformed JSON, missing и negative usage;
- timeout, cancellation и общий output overflow;
- приоритет parent timeout/cancel над overflow;
- запрет переопределения управляемых CLI flags;
- runtime retry с сохранением Session ID и отдельными execution records.

## Внешние проверки

Реальный OpenCode smoke в среде сборки не запускался: бинарник OpenCode не установлен. Он остаётся opt-in через `TAKT_OPENCODE_SMOKE=1`, `TAKT_OPENCODE_SMOKE_PROVIDER` и `TAKT_OPENCODE_SMOKE_MODEL`.
