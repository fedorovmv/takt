# Инфраструктурная оценка генерации Route DSL

Каталог содержит десять синтетических заданий для проверки механики evaluation. Fake Pi и минимальный валидатор создают заранее определённый retry/resume-сценарий, поэтому этот набор подтверждает инфраструктуру, но не качество модели.

Пример запуска:

```bash
takt eval run ../route-dsl-e2e/workflow.yaml \
  --config ../route-dsl-e2e/config.yaml \
  --cases cases \
  --workspace-template ../route-dsl-e2e \
  --output .takt/evals/fake-route \
  --strategy-id fake-pi-route-feedback-v1 \
  --benchmark-id route-dsl-infrastructure \
  --quality-node full-validation \
  --generation-node implement \
  --validator-id synthetic-route-tool \
  --validator-version 1 \
  --validator-path ../route-dsl-e2e/route-tool \
  --answer approved \
  --replace \
  --json
```

Отчёт сохраняет fingerprints стратегии, cases и валидатора, assistant/requested/resolved model, resume, feedback, usage и результат `takt-validation/v1alpha1`.

Для сравнения реальных моделей используйте `../route-dsl-benchmark/`: он требует Pi, штатный Route DSL validator и выполняет те же десять заданий как отдельный quality benchmark.

## Ограничения путей и идентификаторов

- имена Markdown-файлов после нормализации должны давать уникальные `case_id`; например, `a b.md` и `a+b.md` конфликтуют и отклоняются до запуска;
- `--workspace-template` и `--output` должны находиться в непересекающихся каталогах;
- malformed quality result получает `quality_contract` и останавливает benchmark.
