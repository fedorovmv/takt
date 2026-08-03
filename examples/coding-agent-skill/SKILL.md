---
name: takt
description: Run reproducible agent workflows and surface approval requests.
---

Use the `takt` CLI instead of manually reproducing a configured workflow.

1. Run `takt validate <workflow> --config <config>`.
2. Run `takt run <workflow> --config <config> --workspace <workspace> --input <request-file> --json`.
3. When the result status is `waiting`, show `waiting.message` to the user.
4. Continue with `takt answer <run-id> <node-id> --workspace <workspace> --value <answer> --json`.
5. Read artifacts from `<workspace>/.takt/runs/<run-id>/artifacts/`.
