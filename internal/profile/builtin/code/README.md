# Code profile

Version: 0.2.1

This profile keeps the development plan in Markdown. Takt passes the original file path and content to the coding agent; no mandatory task AST is created.

Configure `.takt/config.yaml`, then run:

```bash
takt run code --input docs/plan.md
```

Set an explicit validation command when auto-detection is not appropriate:

```bash
TAKT_VALIDATE_COMMAND='go test ./... && go vet ./...' takt run code --input docs/plan.md
```

The bundled workflow composes reusable implementation and review subworkflows. It uses OpenCode for both phases so one installed assistant is sufficient. Change the `review` node or command frontmatter to `pi` when an independent assistant is desired.

Version 0.2.1 also fingerprints commands from the profile root when a composed workflow is stored in `workflows/`.
