# Reference external adapters

`v0.1.49-alpha` includes two reference implementations that consume only the
public Takt SDK contracts:

- `qwen-takt-adapter` — Qwen Code CLI headless wrapper for
  `takt-assistant/v1alpha2`;
- `takt-github-scm-adapter` — GitHub/`gh` implementation of the neutral SCM
  domain contract.

Build them together with Takt:

```bash
go build ./cmd/qwen-takt-adapter
go build ./cmd/takt-github-scm-adapter
```

The Qwen wrapper runs the upstream `qwen` binary in headless `stream-json`
mode, preserves exact `--resume <session-id>`, forwards model selection and
normalizes session/message/usage/terminal events. It deliberately advertises
only `agent_events_v2`, `session_events` and `usage_events`. It does **not**
claim Takt `tool_policy`, selected skills/MCP projection, `tool_control` or
sandbox enforcement. Use it only for nodes whose effective policy does not
require those capabilities.

The GitHub adapter invokes the authenticated `gh` CLI. Repository resolution
prefers the exact execution/repository workspace and its `origin`, then falls
back to an explicit `[HOST/]OWNER/REPO` or `GH_REPO`. Mutating
`change.create`, `change.comment` and `change.review` operations support Takt
reconciliation. The idempotency key is never published verbatim: the adapter
stores a SHA-256-derived marker in the PR/comment/review body and uses it for
recovery after an ambiguous transport failure.

Both binaries are reference implementations, not provider logic inside the
runtime. Corporate Git/SCM and other coding agents should implement the same
public SDK contracts without changing workflow definitions.
