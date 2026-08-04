# Subworkflow and foreach

This example composes one reusable workflow directly and then executes the same workflow for two explicit items.

```bash
takt validate examples/composition/workflow.yaml \
  --config examples/composition/config.yaml \
  --workspace examples/composition

takt run examples/composition/workflow.yaml \
  --config examples/composition/config.yaml \
  --workspace examples/composition
```

`foreach.items` is explicit data from the workflow. Takt does not parse Markdown plans into tasks. The public nodes `prepare` and `batch` remain valid dependencies; generated child nodes use stable `__`-prefixed namespaces in Run state.
