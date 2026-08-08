# Takt authoring skill

Скилл помогает создавать и проверять `takt/v1alpha1` workflow, config, Markdown-команды и профили. Он охватывает параллельный DAG, `output_format`, JSON-пути, retry/feedback, approval внутри циклов, subworkflow, последовательный/параллельный foreach и именованные каталоги workflow с роутером, managed Git worktree isolation и локальный MCP control plane.

Версия скилла — `0.27.0`. Он рассчитан на Takt `v0.1.45-alpha` и контракт `takt/v1alpha1`.

Проверка:

```bash
./scripts/test-takt-skill.sh
```
