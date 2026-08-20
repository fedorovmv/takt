# Takt authoring skill

Скилл помогает создавать и проверять `takt/v1alpha1` workflow, config, Markdown-команды и профили. Он охватывает DAG, `output_format`, JSON-пути, retry/feedback, approval, loop/subworkflow/foreach/matrix composition, governed child Runs, immutable assessments, authored evaluation, managed Git worktree isolation и локальный MCP control plane.

Версия скилла — `0.43.0`. Он рассчитан на Takt `v0.1.64-alpha` и контракт `takt/v1alpha1`.

Проверка:

```bash
go test ./tests/e2e -run TestTaktSkillContract
```
