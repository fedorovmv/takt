# OpenCode smoke profile

This profile runs one non-interactive OpenCode node through `opencode run --format json`.

1. Authenticate/configure OpenCode normally.
2. Replace `provider` and `id` in `config.yaml` with an OpenCode model name.
3. Validate and run:

```bash
../../bin/takt validate workflow.yaml --config config.yaml --workspace ../.. --json
../../bin/takt run workflow.yaml --config config.yaml --workspace ../.. --json
```

`agent` maps to OpenCode `--agent`. Model parameter `variant` maps to `--variant`.
`auto_approve: true` maps to OpenCode `--auto` and should only be used in a trusted workspace.
Takt owns the outer retry/session policy; OpenCode owns its internal coding-agent loop and tools.
