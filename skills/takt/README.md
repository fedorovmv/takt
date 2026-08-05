# Takt authoring skill

Скилл помогает создавать и проверять `takt/v1alpha1` workflow, config, Markdown-команды и профили. Он охватывает параллельный DAG, `output_format`, JSON-пути, retry/feedback, approval внутри циклов, subworkflow, последовательный/параллельный foreach и именованные каталоги workflow с роутером и managed Git worktree isolation.

Версия скилла — `0.8.0`. Он рассчитан на Takt `v0.1.26-alpha` и контракт `takt/v1alpha1`.

Проверка:

```bash
./scripts/test-takt-skill.sh
```
