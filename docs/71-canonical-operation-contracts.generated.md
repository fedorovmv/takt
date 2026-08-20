# Canonical operation contracts

Generated from `internal/appapi` operation descriptors. Do not edit by hand.

## Stable

### `run.abandon` — Abandon Takt Run

- MCP tool: `takt.run.abandon`
- Stage: `stable`

Stop servicing a Run and active descendants while preserving history with an abandoned terminal state.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "reason": {
      "description": "Operator reason",
      "type": "string"
    },
    "run_id": {
      "description": "Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.answer` — Answer Takt approval

- MCP tool: `takt.run.answer`
- Stage: `stable`

Submit an approval response and continue the waiting child and parent Run chain.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "node_id": {
      "description": "Public approval node ID",
      "type": "string"
    },
    "run_id": {
      "description": "Root or direct child Run ID",
      "type": "string"
    },
    "value": {
      "description": "Approval response",
      "type": "string"
    }
  },
  "required": [
    "run_id",
    "node_id",
    "value"
  ],
  "type": "object"
}
```

### `run.artifacts` — List Takt artifacts

- MCP tool: `takt.run.artifacts`
- Stage: `stable`

List typed artifacts with checksum and provenance; optionally include bounded local file content.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "include_content": {
      "description": "Include bounded artifact content",
      "type": "boolean"
    },
    "max_bytes": {
      "description": "Maximum bytes per included artifact; defaults to 65536",
      "maximum": 1048576,
      "minimum": 1,
      "type": "integer"
    },
    "node_id": {
      "description": "Optional producer node filter",
      "type": "string"
    },
    "recursive": {
      "description": "Include descendant Runs",
      "type": "boolean"
    },
    "run_id": {
      "description": "Run ID",
      "type": "string"
    },
    "type": {
      "description": "Optional semantic type filter",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.assessment` — List Takt Run assessments

- MCP tool: `takt.run.assessment`
- Stage: `stable`

List immutable assessments where the Run is the target or assessor; stale target revisions are excluded by default.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "include_stale": {
      "description": "Include assessments pinned to an older target result revision",
      "type": "boolean"
    },
    "role": {
      "description": "Optional assessment role filter",
      "enum": [
        "primary",
        "advisory"
      ],
      "type": "string"
    },
    "run_id": {
      "description": "Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.attention` — List Runs requiring attention

- MCP tool: `takt.run.attention`
- Stage: `stable`

Return approvals, questions, tool approvals, failures and paused Runs that require an operator action.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {},
  "type": "object"
}
```

### `run.cancel` — Cancel Takt Run

- MCP tool: `takt.run.cancel`
- Stage: `stable`

Request durable cancellation of a Run and its active child tree.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "reason": {
      "description": "Cancellation reason",
      "type": "string"
    },
    "run_id": {
      "description": "Durable Takt Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.children` — List child Runs

- MCP tool: `takt.run.children`
- Stage: `stable`

List direct governed child Runs and fan-out item metadata.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "run_id": {
      "description": "Parent Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.events` — Read Takt Run events

- MCP tool: `takt.run.events`
- Stage: `stable`

Read events after a durable revision cursor. wait_ms enables bounded long polling for incremental monitoring.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "after_revision": {
      "description": "Return events with a greater revision",
      "maximum": 4294967295,
      "minimum": 0,
      "type": "integer"
    },
    "limit": {
      "description": "Maximum events, defaults to 200",
      "maximum": 1000,
      "minimum": 1,
      "type": "integer"
    },
    "run_id": {
      "description": "Run ID",
      "type": "string"
    },
    "wait_ms": {
      "description": "Long-poll wait, 0 to 30000 milliseconds",
      "maximum": 30000,
      "minimum": 0,
      "type": "integer"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.fork` — Fork Takt Run

- MCP tool: `takt.run.fork`
- Stage: `stable`

Create a new Run from the same workflow and options, or a new Dynamic Plan when the source belongs to Dynamic Takt.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "input": {
      "description": "Optional replacement input or Dynamic Plan goal",
      "type": "string"
    },
    "run_id": {
      "description": "Source Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.get` — Get Takt Run

- MCP tool: `takt.run.get`
- Stage: `stable`

Read the current public Run state, including waiting approval, nodes, usage and durable child links.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "run_id": {
      "description": "Durable Takt Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.inspect` — Inspect Takt Run

- MCP tool: `takt.run.inspect`
- Stage: `stable`

Build a deterministic case, cause, evidence and node view from durable Run state, events, artifacts and non-stale assessments.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "case_id": {
      "description": "Optional case filter",
      "type": "string"
    },
    "node_id": {
      "description": "Optional node filter",
      "type": "string"
    },
    "repeat": {
      "description": "Optional repeat filter",
      "maximum": 1000000,
      "minimum": 1,
      "type": "integer"
    },
    "run_id": {
      "description": "Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.list` — List Takt Runs

- MCP tool: `takt.run.list`
- Stage: `stable`

List durable local Runs with effective state, attention reason, current phase, usage and artifact counts.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "active_only": {
      "description": "Return only non-terminal Runs",
      "type": "boolean"
    },
    "attention_only": {
      "description": "Return only Runs requiring operator attention",
      "type": "boolean"
    },
    "limit": {
      "description": "Maximum number of Runs",
      "maximum": 10000,
      "minimum": 1,
      "type": "integer"
    },
    "root_only": {
      "description": "Exclude governed child Runs",
      "type": "boolean"
    },
    "status": {
      "description": "Optional status filter",
      "type": "string"
    }
  },
  "type": "object"
}
```

### `run.pause` — Pause Takt Run

- MCP tool: `takt.run.pause`
- Stage: `stable`

Request a safe pause at node boundaries for the Run and active descendants. Running attempts finish before the pause takes effect.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "run_id": {
      "description": "Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.recover` — Recover interrupted Runs

- MCP tool: `takt.run.recover`
- Stage: `stable`

Detect Runs whose executor process disappeared, mark active attempts as worker_lost and continue them from durable state.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {},
  "type": "object"
}
```

### `run.resume` — Resume Takt Run

- MCP tool: `takt.run.resume`
- Stage: `stable`

Resume a failed or otherwise resumable Run after external correction. Definitions and fingerprints are verified first.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "run_id": {
      "description": "Durable Takt Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.resume_paused` — Resume paused Takt Run

- MCP tool: `takt.run.resume_paused`
- Stage: `stable`

Clear pause requests and continue a paused Run. A Run paused while waiting returns to the same waiting state.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "run_id": {
      "description": "Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.retry` — Retry failed Takt node

- MCP tool: `takt.run.retry`
- Stage: `stable`

Reset one failed node and its dependent remainder, preserving completed prerequisites and operator retry history.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "node_id": {
      "description": "Failed node; defaults to the first failed node",
      "type": "string"
    },
    "run_id": {
      "description": "Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.start` — Start a Takt Run

- MCP tool: `takt.run.start`
- Stage: `stable`

Validate definitions and start a local Takt Run. Detached mode is the default and returns a durable run_id for polling.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "allow_dirty_worktree": {
      "description": "Allow a dirty control checkout and start from committed state",
      "type": "boolean"
    },
    "config_path": {
      "description": "Optional config override",
      "type": "string"
    },
    "detached": {
      "description": "Return after the Run is durably started; defaults to true",
      "type": "boolean"
    },
    "input": {
      "description": "Input text or a readable input file path",
      "type": "string"
    },
    "keep_worktree": {
      "description": "Keep a successful worktree",
      "type": "boolean"
    },
    "model_overrides": {
      "additionalProperties": {
        "description": "provider/model override by workflow alias",
        "type": "string"
      },
      "type": "object"
    },
    "model_preset": {
      "description": "Named model preset from the selected Config",
      "type": "string"
    },
    "selector": {
      "description": "Profile selector or workflow file path",
      "type": "string"
    },
    "worktree": {
      "description": "Force or disable managed Git worktree isolation",
      "type": "boolean"
    },
    "worktree_base": {
      "description": "Optional Git base revision",
      "type": "string"
    }
  },
  "required": [
    "selector"
  ],
  "type": "object"
}
```

### `run.stats` — Get Takt Run statistics

- MCP tool: `takt.run.stats`
- Stage: `stable`

Aggregate matrix denominator, attempts, usage, non-stale primary assessment outcomes and optional deterministic gates.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "check_gates": {
      "description": "Evaluate gates embedded in the Run input",
      "type": "boolean"
    },
    "run_id": {
      "description": "Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.status` — Get Takt Run status

- MCP tool: `takt.run.status`
- Stage: `stable`

Read technical Run state, matrix progress, usage, attempts and a compact assessment summary.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "run_id": {
      "description": "Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `run.summary` — Summarize Takt Run

- MCP tool: `takt.run.summary`
- Stage: `stable`

Return an operator-oriented result projection with progress, descendants, usage, artifacts, output and remaining attention.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "recursive": {
      "description": "Aggregate descendant Runs",
      "type": "boolean"
    },
    "run_id": {
      "description": "Run ID",
      "type": "string"
    }
  },
  "required": [
    "run_id"
  ],
  "type": "object"
}
```

### `workflow.describe` — Describe a Takt workflow

- MCP tool: `takt.workflow.describe`
- Stage: `stable`

Describe the public DAG of a profile selector before starting it.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "selector": {
      "description": "Profile selector such as code:plan-to-pr",
      "type": "string"
    }
  },
  "required": [
    "selector"
  ],
  "type": "object"
}
```

### `workflow.list` — List Takt workflows

- MCP tool: `takt.workflow.list`
- Stage: `stable`

List deterministic workflow selectors published by an installed Takt profile.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "profile": {
      "description": "Installed profile name, for example code",
      "type": "string"
    }
  },
  "required": [
    "profile"
  ],
  "type": "object"
}
```

## Extensions

### `block.describe` — Describe trusted Dynamic Takt block

- MCP tool: `takt.block.describe`
- Stage: `extension`

Describe one trusted block, its package scope, output paths, capabilities, integrations and policy.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "name": {
      "description": "Trusted block name",
      "type": "string"
    },
    "profile": {
      "description": "Installed profile, defaults to code",
      "type": "string"
    }
  },
  "required": [
    "name"
  ],
  "type": "object"
}
```

### `block.list` — List trusted Dynamic Takt blocks

- MCP tool: `takt.block.list`
- Stage: `extension`

List explicitly trusted block packages, governance limits, templates and blocks available to a profile.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "profile": {
      "description": "Installed profile, defaults to code",
      "type": "string"
    }
  },
  "type": "object"
}
```

### `notify.ack` — Acknowledge Takt notification

- MCP tool: `takt.notify.ack`
- Stage: `extension`

Mark one durable notification as acknowledged.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "id": {
      "description": "Notification ID",
      "type": "string"
    }
  },
  "required": [
    "id"
  ],
  "type": "object"
}
```

### `notify.dispatch` — Dispatch pending Takt notifications

- MCP tool: `—`
- Stage: `extension`

Dispatch pending durable notifications through configured sinks.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {},
  "type": "object"
}
```

### `notify.list` — List Takt notifications

- MCP tool: `takt.notify.list`
- Stage: `extension`

Read durable local notifications produced by autonomous Runs; supports an unread-only view for coding-agent hosts.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "limit": {
      "description": "Maximum notifications",
      "maximum": 10000,
      "minimum": 1,
      "type": "integer"
    },
    "unread_only": {
      "description": "Only unacknowledged notifications",
      "type": "boolean"
    }
  },
  "type": "object"
}
```

### `notify.test` — Test Takt notifications

- MCP tool: `takt.notify.test`
- Stage: `extension`

Create and deliver a local test notification through configured sinks.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "message": {
      "description": "Optional test message",
      "type": "string"
    }
  },
  "type": "object"
}
```

## Experimental

### `host.begin` — Begin managed Takt session

- MCP tool: `takt.host.begin`
- Stage: `experimental`

Bind a coding-agent host session to a Takt plan before the main LLM handles the task. Strict mode requires interception and recovery capabilities.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "candidate": {
      "description": "Optional externally proposed WorkflowPlan; Takt still validates it",
      "type": "object"
    },
    "capabilities": {
      "additionalProperties": false,
      "properties": {
        "command_interception": {
          "description": "Host intercepts /takt before the LLM",
          "type": "boolean"
        },
        "completion_blocking": {
          "description": "Host blocks premature completion",
          "type": "boolean"
        },
        "input_interception": {
          "description": "Host intercepts later input",
          "type": "boolean"
        },
        "session_recovery": {
          "description": "Host restores managed mode",
          "type": "boolean"
        },
        "tool_call_blocking": {
          "description": "Host blocks tools before execution",
          "type": "boolean"
        }
      },
      "type": "object"
    },
    "enforcement": {
      "enum": [
        "advisory",
        "guarded",
        "strict"
      ],
      "type": "string"
    },
    "goal": {
      "description": "User task",
      "type": "string"
    },
    "host": {
      "description": "Coding-agent host, for example pi or opencode",
      "type": "string"
    },
    "host_session_id": {
      "description": "Stable host session ID",
      "type": "string"
    },
    "profile": {
      "description": "Planning profile",
      "type": "string"
    }
  },
  "required": [
    "host",
    "host_session_id",
    "goal"
  ],
  "type": "object"
}
```

### `host.confirm` — Confirm managed Takt session

- MCP tool: `takt.host.confirm`
- Stage: `experimental`

Confirm preview and start the bound Takt plan.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "confirm": {
      "description": "Confirm preview and budgets",
      "type": "boolean"
    },
    "session_id": {
      "description": "Managed host session ID",
      "type": "string"
    }
  },
  "required": [
    "session_id",
    "confirm"
  ],
  "type": "object"
}
```

### `host.find` — Find managed Takt session

- MCP tool: `takt.host.find`
- Stage: `experimental`

Recover a durable managed session by coding-agent host and session ID.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "host": {
      "description": "Coding-agent host",
      "type": "string"
    },
    "host_session_id": {
      "description": "Stable host session ID",
      "type": "string"
    }
  },
  "required": [
    "host",
    "host_session_id"
  ],
  "type": "object"
}
```

### `host.get` — Get managed Takt session

- MCP tool: `takt.host.get`
- Stage: `experimental`

Read managed host session and bound plan state.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "session_id": {
      "description": "Managed host session ID",
      "type": "string"
    }
  },
  "required": [
    "session_id"
  ],
  "type": "object"
}
```

### `host.guard_completion` — Guard coding-agent completion

- MCP tool: `takt.host.guard_completion`
- Stage: `experimental`

Block a final response while the bound Takt plan is active.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "kind": {
      "enum": [
        "final",
        "status",
        "question"
      ],
      "type": "string"
    },
    "session_id": {
      "description": "Managed host session ID",
      "type": "string"
    }
  },
  "required": [
    "session_id",
    "kind"
  ],
  "type": "object"
}
```

### `host.guard_tool` — Guard coding-agent tool

- MCP tool: `takt.host.guard_tool`
- Stage: `experimental`

Fail closed on a host tool call while a Takt-managed workflow is active.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "read_only": {
      "description": "Host advisory read-only declaration; never overrides the Takt allowlist",
      "type": "boolean"
    },
    "session_id": {
      "description": "Managed host session ID",
      "type": "string"
    },
    "tool": {
      "description": "Host tool name",
      "type": "string"
    }
  },
  "required": [
    "session_id",
    "tool"
  ],
  "type": "object"
}
```

### `host.release` — Release managed Takt session

- MCP tool: `takt.host.release`
- Stage: `experimental`

Explicitly leave managed mode without cancelling the underlying Takt plan.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "session_id": {
      "description": "Managed host session ID",
      "type": "string"
    }
  },
  "required": [
    "session_id"
  ],
  "type": "object"
}
```

### `plan.create` — Plan with Dynamic Takt

- MCP tool: `takt.plan`
- Stage: `experimental`

Choose an existing workflow or create a bounded task-specific WorkflowPlan from approved blocks. Returns preview, budget and confirmation requirement.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "candidate": {
      "description": "Optional externally proposed WorkflowPlan; Takt still validates it",
      "type": "object"
    },
    "goal": {
      "description": "Natural-language engineering goal",
      "type": "string"
    },
    "profile": {
      "description": "Installed profile, defaults to code",
      "type": "string"
    }
  },
  "required": [
    "goal"
  ],
  "type": "object"
}
```

### `plan.execute` — Execute Dynamic Takt plan

- MCP tool: `takt.execute`
- Stage: `experimental`

Execute a previewed existing or planned workflow. Planned workflows require explicit confirm=true.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "confirm": {
      "description": "Confirm the displayed preview and hard limits",
      "type": "boolean"
    },
    "plan_id": {
      "description": "Durable plan ID",
      "type": "string"
    }
  },
  "required": [
    "plan_id"
  ],
  "type": "object"
}
```

### `plan.get` — Get Dynamic Takt plan

- MCP tool: `takt.plan.get`
- Stage: `experimental`

Read plan revisions, current phase segment, execution Runs, steering and promotion state.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "plan_id": {
      "description": "Durable plan ID",
      "type": "string"
    }
  },
  "required": [
    "plan_id"
  ],
  "type": "object"
}
```

### `plan.promote` — Promote successful dynamic plan

- MCP tool: `takt.plan.promote`
- Stage: `experimental`

Compile the latest successful plan revision into a validated project workflow under .takt/workflows/generated.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "force": {
      "description": "Replace an existing generated workflow",
      "type": "boolean"
    },
    "name": {
      "description": "Project workflow name",
      "type": "string"
    },
    "plan_id": {
      "description": "Completed plan ID",
      "type": "string"
    }
  },
  "required": [
    "plan_id",
    "name"
  ],
  "type": "object"
}
```

### `plan.steer` — Steer Dynamic Takt run

- MCP tool: `takt.run.steer`
- Stage: `experimental`

Queue an instruction for the next replanning checkpoint, or continue a plan waiting for user input.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "message": {
      "description": "Concrete steering instruction",
      "type": "string"
    },
    "plan_id": {
      "description": "Plan ID",
      "type": "string"
    },
    "run_id": {
      "description": "Any execution Run ID owned by the plan",
      "type": "string"
    }
  },
  "required": [
    "message"
  ],
  "type": "object"
}
```

### `task.explain` — Explain a managed task

- MCP tool: `takt.task.explain`
- Stage: `experimental`

Return detailed routing, controls, phases, child Runs and evidence only when deeper inspection is requested.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "reference": {
      "description": "Plan or Run ID",
      "type": "string"
    }
  },
  "required": [
    "reference"
  ],
  "type": "object"
}
```

### `task.respond` — Respond to a managed task

- MCP tool: `takt.task.respond`
- Stage: `experimental`

Approve, answer, steer, pause, resume, continue or retry a task without exposing the internal state machine.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "action": {
      "enum": [
        "go",
        "continue",
        "answer",
        "steer",
        "pause",
        "resume",
        "retry"
      ],
      "type": "string"
    },
    "message": {
      "description": "Answer or steering text when required",
      "type": "string"
    },
    "node_id": {
      "description": "Optional waiting or failed node",
      "type": "string"
    },
    "reference": {
      "description": "Plan or Run ID",
      "type": "string"
    }
  },
  "required": [
    "reference",
    "action"
  ],
  "type": "object"
}
```

### `task.start` — Start a managed Takt task

- MCP tool: `takt.task.start`
- Stage: `experimental`

Route a natural-language task or a configured structured task source to a specialized workflow, the stable simple-reliable template, or bounded dynamic composition. Provide goal, or source + source_ref. By default returns a preview; go=true confirms and starts it.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "go": {
      "description": "Confirm the preview and start immediately",
      "type": "boolean"
    },
    "goal": {
      "description": "Natural-language task; mutually exclusive with source",
      "type": "string"
    },
    "profile": {
      "description": "Installed profile, defaults to code",
      "type": "string"
    },
    "source": {
      "description": "Configured structured task source",
      "type": "string"
    },
    "source_ref": {
      "description": "External reference for source",
      "type": "string"
    }
  },
  "type": "object"
}
```

### `task.status` — Get managed task status

- MCP tool: `takt.task.status`
- Stage: `experimental`

Read a compact task view by plan_id or run_id, including whether user input is needed.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "reference": {
      "description": "Plan or Run ID",
      "type": "string"
    }
  },
  "required": [
    "reference"
  ],
  "type": "object"
}
```

### `task.stop` — Stop a managed task

- MCP tool: `takt.task.stop`
- Stage: `experimental`

Abandon a plan or Run while preserving its durable history.

Input schema:

```json
{
  "additionalProperties": false,
  "properties": {
    "reason": {
      "description": "Optional stop reason",
      "type": "string"
    },
    "reference": {
      "description": "Plan or Run ID",
      "type": "string"
    }
  },
  "required": [
    "reference"
  ],
  "type": "object"
}
```

