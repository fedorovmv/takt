# Human-reviewed learning loop

Learning loop работает на существующей `.takt/runs` истории и не требует нового workflow/runtime.

```bash
# Найти pattern, встретившийся хотя бы в двух разных Run.
takt learn scan --workspace . --min-runs 2

# Создать immutable candidate snapshot.
takt learn propose --workspace . \
  --pattern diagnostic:sha256:... \
  --kind skill \
  --name validation-recovery \
  --candidate ./candidate/SKILL.md \
  --benefit "reduce repeated validation failures"

# Решение человека обязательно.
takt learn review learn-... --workspace . \
  --decision accept --reason "reusable and scoped"

# Используется обычный matrix report с regression gates.
takt learn evaluate learn-... --workspace . \
  --report ./evaluation/benchmark.json

# Только staging; trusted package/skill configuration не меняется.
takt learn stage learn-... --workspace .
```

Подробный контракт: [`../../docs/archive/releases/65-human-reviewed-learning-loop-v0.1.51.md`](../../docs/archive/releases/65-human-reviewed-learning-loop-v0.1.51.md).
