# Adapter Platform example

This workflow intentionally contains no GitHub, GitLab, Jira or CI-vendor operation names. It uses the neutral domains `tracker`, `ci` and `scm`; `config.yaml` binds those names to process or MCP transports.

The release contract script builds `takt-fake-domain-adapter`, validates capability discovery, executes the workflow and checks durable receipts/reconciliation state.
