# Coding Agent Host Control

Host extensions make `/takt` a host operation rather than an instruction to the main LLM.

- `pi/`: extension pinned to the Pi `0.73.1` type contract;
- `opencode/`: OpenCode V2 beta plugin plus command files.

Both require `takt` on `PATH` and a local daemon. They persist a small fail-closed client cache in `.takt/host-client-state/`: losing the daemon does not silently return the session to unrestricted tools or send steering text to the main LLM.

## Enforcement level

The Go host-control API supports `advisory|guarded|strict`. `strict` is accepted only from a host that attests command interception, input interception, pre-execution tool blocking, completion blocking and session recovery.

The bundled Pi and OpenCode integrations declare **guarded**, not strict:

- Pi `0.73.1` has blocking input/tool hooks, but no verified fail-closed completion hook;
- OpenCode V2 context-abort behavior is not verified by a live smoke in this release and its package metadata contains `verified: false`.

Corporate rollout must pin the target host version and run a live contract suite before upgrading either adapter to strict.

## Commands

Common user operations:

```text
/takt <goal>
/takt-confirm
/takt-status
/takt-runs
/takt-attention
/takt-pause
/takt-resume
/takt-result
/takt-release
```

Pi registers native commands. OpenCode supplies files under `opencode/commands/`.

## Guard behavior

While a cached managed session is active:

- normal input becomes steering and is never allowed to fall through after a transport error;
- edit/write/shell/Git and unknown tools are denied before execution;
- Pi blocks direct user bash;
- OpenCode uses both `tool.execute.before` and `shell.hook("create.before")`;
- all user text is placed after `--` in Takt CLI calls.

The exact host guarantees and remaining limitations are documented in `docs/50-coding-agent-host-control-v0.1.36.md`. Autonomous run commands and notifications are documented in `docs/51-autonomous-run-operations-v0.1.37.md`.
