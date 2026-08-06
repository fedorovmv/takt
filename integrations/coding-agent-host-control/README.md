# Coding Agent Host Control

Native host extensions make `/takt` a host operation rather than an instruction to the main LLM.

- `pi/`: Pi extension. Install as `.pi/extensions/takt-host-control.ts` or load with `--extension`.
- `opencode/`: OpenCode V2 plugin. Add its path to `plugins` in `opencode.json` and copy `opencode/commands/*.md` into `.opencode/commands/`.

Both integrations require `takt` on `PATH` and use the local daemon. They declare strict enforcement only because they intercept commands/input before model dispatch, gate tools before execution, avoid final model completion while managed, and restore the durable host session.

The OpenCode V2 plugin API is beta. Pin a compatible OpenCode/plugin version and run a live smoke test in the target environment before corporate rollout.
