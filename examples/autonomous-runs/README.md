# Autonomous Run operations

Copy `notifications.yaml` to `.takt/notifications.yaml`, start the daemon and inspect the durable inbox:

```bash
mkdir -p .takt
cp examples/autonomous-runs/notifications.yaml .takt/notifications.yaml
takt daemon start --workspace .
takt runs --active --workspace . --daemon
takt attention --workspace . --daemon
takt notify list --unread --workspace .
```

Pause is cooperative at node/fan-out boundaries. `run recover` starts a new attempt after a lost local executor; external side effects must be idempotent.
