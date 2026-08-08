# Structured Task Source example

Соберите reference source adapter:

```bash
go build -o ./bin/takt-github-task-source ./cmd/takt-github-task-source
```

Добавьте его в `PATH` или укажите абсолютный путь в `task_sources.github.argv`, затем:

```bash
takt task start \
  --config examples/task-sources/config.yaml \
  --workspace . \
  --profile code \
  --source github \
  --source-ref owner/repository#42
```

GitHub Issue преобразуется в normalized Task до Router. `source.revision` сохраняется в plan; replan/resume использует ту же ревизию и не перечитывает issue автоматически.
