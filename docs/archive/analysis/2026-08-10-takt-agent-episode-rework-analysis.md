# Takt: disruptive analysis of flow authoring and agent episodes

Date: 2026-08-10

Status: **CONDITIONAL — working analysis, not an approved implementation specification**

Purpose: preserve the current conclusions before conversation compaction and provide a concrete basis for the next design discussion.

## 1. Problem statement

Takt started as a way to make generation of a schema-constrained YAML artifact from a textual specification reproducible. The intended product boundary later broadened: the same runtime must support arbitrary processes such as:

- generating and repairing a schema-constrained configuration artifact;
- implementing a feature from a tracker ticket and publishing a change request;
- reviewing a pull request;
- analysing vulnerabilities and applying/remediating findings;
- preparing and approving technical documents;
- other domain-specific flows supplied by a project or organization.

The target is therefore not a domain-specific runtime and not a reduced linear pipeline. Takt must keep general DAG/process capabilities while making ordinary authoring substantially simpler.

The desired product properties are:

1. Flow structure is deterministic and owned by the user or organization.
2. An agent cannot silently skip required boundaries merely because a prompt asked it not to.
3. Each agent step can have a real, enforced set of tools and skills.
4. Deterministic checks can give feedback during an agent episode, not only after a whole black-box node returns.
5. Users can connect existing MCP tools, executables and scripts without writing a Go adapter or a provider-specific integration.
6. Domain knowledge remains outside Takt core.
7. Pi, OpenCode and another coding-agent host keep their own model/tool loop; Takt controls the session contract and lifecycle around that loop.
8. Alpha compatibility is not a constraint. Unsuccessful contracts may be deleted and replaced instead of preserved.

## 2. Confirmed current-state problems

### 2.1 Multiple overlapping authoring models

Takt currently exposes several ways to represent closely related work:

- raw `Workflow`;
- `WorkflowPlan` and Dynamic Takt phases;
- profile workflows and router-selected workflows;
- `BlockPackage` blocks;
- `workflow` child nodes;
- compile-time `subworkflow`;
- command/prompt nodes plus script, adapter and internal actions.

The user must choose between `takt run`, `takt task start`, `takt plan`, profiles, blocks and direct workflow files. These are implementation layers, but they appear as competing product models.

### 2.2 Runtime language leaks into normal authoring

The current YAML exposes fields needed by internal execution and proof mechanics:

- verbose `output_format` objects;
- `output_type`, `output_path` and artifact references;
- domain status/code enums;
- hand-written acceptance gates;
- `script.runtime: command|python|node|go|validation`;
- BlockPackage `output_paths`, `checks`, `level` and `reaction`;
- explicit `workflow` versus `subworkflow` lifecycle choices.

This makes an author describe runtime internals instead of the development or business process.

### 2.3 Validation is represented in too many places

Validation can currently be described in:

- a Markdown plan;
- `input.validation_commands`;
- an inline `bash` node;
- `script.runtime: validation`;
- an after-node hook;
- BlockPackage checks;
- structured output from an assistant;
- a final acceptance gate.

There is no single obvious source of truth for an ordinary flow author.

`runtime: validation` itself knows no project commands. It reads an array of shell strings from Run input and executes them through `sh -lc`. It is a domain operation placed in the same enum as execution backends, which is a conceptual mismatch.

### 2.4 Current agent nodes remain black boxes

For bundled Pi/OpenCode node execution, Takt generally controls:

- the prompt;
- the selected assistant/model/session;
- initial tool/skill/MCP policy;
- timeout and cancellation;
- node-level hooks before or after the complete assistant invocation;
- final output and artifacts.

It does not yet have a generally proven strict boundary for:

- pre-execution approval of every worker tool call;
- changing the actual tool schema during the same provider episode;
- deterministic processing immediately after a tool result;
- rejecting model completion and continuing the same episode with feedback.

OpenCode/Pi observational events are not sufficient. Enforcement requires a host/plugin or external worker that can block before execution and before completion.

## 3. Evidence from external domain-integration experiments

The underlying experiments were performed outside this repository. Only
provider-neutral conclusions that affect general agent-loop behavior are
retained here; private project names, paths and domain details are excluded.

### 3.1 Optional tools do not create a process

Adding `write_test` and `run_route_tests` to an existing Builder tool set did not cause the model to use them. The model continued to search and edit without creating the desired fast RED/GREEN loop.

Conclusion: making a tool available is not equivalent to making its use part of the process.

### 3.2 Rejecting a visible tool does not redirect the model

When `edit_file` remained in the provider tool schema but its handler returned `route_test_required`, Qwen repeated the forbidden call eleven times and never selected the available test tools.

Conclusion: if a tool is not valid in the current process state, it should normally be absent from the episode schema. Handler-level rejection is a safety fallback, not the main transition mechanism.

### 3.3 Separate episodes with different tool surfaces worked

The successful correction split the work into:

- a Test Author episode where mutation tools were absent;
- a Runtime Repair episode where mutation tools became available only after executable RED evidence existed;
- deterministic checks before Docker acceptance.

Conclusion: an agent episode is a real process boundary. A fresh episode is justified when the role, prompt or tool surface changes materially.

### 3.4 Transactional mutations worked

The effective repair loop wrapped route mutations in a controller-owned transaction:

```text
edit/write
  -> static checks
  -> locked test
  -> retain candidate on GREEN
  -> rollback and return feedback on RED
```

The feedback remained connected to the exact model edit and native provider history. Rejected candidates did not become the baseline for unrelated later hypotheses.

Conclusion: a mutation tool may need atomic domain semantics. A generic `edit_file` plus an optional later validator is weaker than one transaction whose commit condition is deterministic.

### 3.5 File-native bounded evidence worked

Large runtime traces remained as immutable files with digests. Model-facing results contained bounded projections, focused failures and paths to full evidence. Copying large trace payloads into tool results caused context and payload failures.

Conclusion: evidence belongs in an artifact store. Tool results should carry bounded projections and references.

### 3.6 Independent final gates remained necessary

Even after an inner repair loop returned GREEN, the controller needed to rerun the locked canonical test independently before Docker. Final Docker assertions and same-digest repeat remained separate authorities.

Conclusion: immediate feedback improves convergence but does not replace independent acceptance.

### 3.7 Provider irregularities require bounded protocol recovery

The experiments encountered text-only turns where a native tool call was required, unknown/hidden tool requests, provider EOF and transient gateway failures. Some could be retried safely only before any tool side effect. Others required a bounded tool-result correction or a fresh episode.

Conclusion: agent integration needs explicit failure classes and side-effect boundaries. A generic retry around an opaque episode is insufficient.

## 4. Rejected architectural extremes

### 4.1 More fields and gates in workflow YAML

Rejected because it moves internal proof mechanics to every flow author and still cannot control the agent between node start and node completion.

### 4.2 Pure Archon-style black-box commands as the only mode

Useful as an ergonomic authoring model, but insufficient when the process promises that required checks, artifacts or tool restrictions actually occurred.

### 4.3 A domain-specific workflow runtime

Rejected because Takt must also support code development, review, security, documentation and future domain flows.

### 4.4 Takt owning a universal LLM tool loop

Rejected as the default core boundary. It would duplicate Pi/OpenCode and reintroduce an agent framework, filesystem tools, conversation management and provider behavior into Takt.

A direct model loop may exist inside a domain experiment or external worker, but it should speak the common Takt agent-host protocol rather than redefine runtime semantics.

## 5. Proposed target model

The target has five public concepts:

1. **Flow** — the process graph.
2. **Agent profile** — prompt, session, toolset, skills and episode contract.
3. **Action** — deterministic code, executable or MCP operation.
4. **Agent episode** — one bounded coding-agent session controlled by a tool/completion protocol.
5. **Evidence** — durable artifacts, receipts and check results.

### 5.1 Flow remains general

A flow must still support the capabilities required by arbitrary processes:

- DAG dependencies and parallel branches;
- conditions based on deterministic results;
- bounded loops and retry;
- approval/HITL;
- reusable processes;
- isolated worktrees and child execution;
- deterministic actions and external side effects;
- agent steps with different sessions, tools and skills.

The goal is not to remove those capabilities. The goal is to stop requiring every author to spell out their internal implementation.

A sketch of the desired authoring level:

```yaml
apiVersion: takt/v2alpha1
kind: Flow

metadata:
  name: issue-to-pr

steps:
  - id: intake
    agent: issue.analyst

  - id: implement
    agent: code.builder
    after: [intake]

  - id: verify
    action: project.validate
    after: [implement]

  - id: review
    agent: code.reviewer
    after: [verify]

  - id: publish
    action: scm.create_change
    after: [review]
```

The exact schema is intentionally not approved yet. The important boundary is that `agent` and `action` reference reusable contracts. The flow does not reproduce output schemas, hook scripts and status enums for those contracts.

### 5.2 One composition concept

The current public distinction between `workflow` and `subworkflow` is likely unnecessary.

The target should expose one reusable process reference. Whether it executes inline or as an isolated child Run is an implementation/policy property of the referenced process, with an explicit override only when a caller truly needs it.

The runtime may continue to use child Runs, compiled graph expansion and worktrees internally.

### 5.3 Agent profile

An agent profile is reusable across flows and declares:

- command/prompt template;
- assistant/model selection or logical model class;
- fresh/resume policy;
- allowed toolset and skills;
- filesystem/network policy;
- episode budget;
- required milestones and artifacts;
- completion policy;
- optional automatic checks around tool calls.

Examples:

```text
issue.analyst
code.builder
code.reviewer
security.analyst
route.builder
route.repair
document.writer
```

The flow author selects a name instead of copying the profile fields into every step.

### 5.4 Agent episode

An agent episode is not one unrestricted black-box process call. It is a bounded session contract:

```text
AgentEpisode
  input references
  current process state
  actual tool schema
  allowed/forbidden capabilities
  required milestones
  tool lifecycle
  completion gate
  mutation/side-effect policy
  feedback and retry policy
  budgets
```

Tool availability may change by creating a new episode with a different profile/toolset. A rejected tool that remains visible is not considered an adequate process transition.

### 5.5 Host protocol

Takt should control the episode without implementing the provider's tool loop. The host/adapter protocol needs at least:

```text
session.started | session.resumed
tool.requested
tool.allowed | tool.denied
tool.started
tool.completed | tool.failed
completion.requested
completion.accepted | completion.rejected
session.completed | session.failed
```

Strict execution requires real capabilities:

- pre-execution tool control;
- observable tool results or atomic domain tools;
- completion blocking;
- exact session recovery when resume is required.

If a host cannot prove them, the episode is guarded/advisory and must not claim strict guarantees.

## 6. User-facing tools and skills without custom adapters

Users should not implement a Takt adapter for each tool. Takt should ship three generic integration paths.

### 6.1 Existing MCP server

The user configures how to start the server. Takt performs MCP discovery and obtains tool names and schemas.

Conceptual configuration:

```yaml
tools:
  domain:
    type: mcp
    command: [domain-tool, serve]
```

### 6.2 Executable/process tool

The user supplies an executable that accepts one standard JSON request and returns one standard JSON result. Takt owns timeout, cancellation, cwd, environment, redaction and evidence capture.

```yaml
tools:
  project-validator:
    type: process
    command: [./tools/project-validator]
```

An optional Go/TypeScript SDK can make this protocol convenient, but it is not required.

### 6.3 Deterministic shell/action

For a simple check or operation, an executable or shell script is enough:

```yaml
actions:
  project.validate:
    command: [./scripts/validate.sh]
```

There are no separate Python/Node/Go runtime kinds. A script chooses its interpreter through its executable/shebang.

### 6.4 Logical toolsets

Flows should reference agent profiles or named toolsets rather than provider-specific tool names. The host integration maps logical tools/capabilities to the actual Pi/OpenCode surface.

Built-in profiles cover common scenarios. Projects may define simple named sets such as:

```text
repository-read
code-write
review-read-only
tracker-read
scm-publish
security-scan
route-authoring
```

## 7. How pre/post tool handlers should work for a domain-tool developer

This is an open P1 design area. An external domain-integration prototype demonstrates the required behavior but not an acceptable reusable developer experience.

Current code frequently:

- builds `[]ToolDef` and `map[string]ToolHandler` separately;
- filters both collections manually for each phase;
- replaces handlers with wrappers such as `wrapDirectBuilderRuntimeRepairMutationHandlers`;
- stores episode flags on a large controller object;
- returns ad-hoc JSON status strings and `terminal` booleans;
- tests wrapper combinations through large integration fixtures.

That pattern should not be copied into Takt or imposed on a domain-tool developer.

### 7.1 Responsibility split

There are two kinds of pre/post behavior and they should not be conflated.

#### Tool-owned invariants

Owned by the domain tool provider:

- valid domain artifact path and schema;
- expected candidate digest;
- atomic write semantics;
- static domain validation;
- route-test execution;
- domain-specific transaction commit/rollback;
- domain-specific evidence projection.

These belong inside an atomic domain tool or its tool-side middleware.

#### Flow/episode policy

Owned by Takt:

- whether the tool is visible in this episode;
- whether the call is allowed now;
- budget and timeout;
- approval requirements;
- generic scope/policy checks;
- durable event/evidence capture;
- whether completion is allowed;
- which episode follows the result.

Takt must not implement domain processor or artifact rules.

### 7.2 Proposed Tool SDK shape

A domain developer should define a tool as one object rather than maintaining parallel schema/handler/wrapper maps.

Conceptual Go API:

```go
tool := takttool.Define("route.write", writeInputSchema, writeRoute)

tool.Before(checkWorkspace, checkExpectedDigest)
tool.After(captureCandidate, validateRoute)
tool.Transaction(routeFileTransaction)
```

Conceptual TypeScript equivalent:

```ts
defineTool({
  name: "route.write",
  input: writeInputSchema,
  handle: writeRoute,
  before: [checkWorkspace, checkExpectedDigest],
  after: [captureCandidate, validateRoute],
  transaction: routeFileTransaction,
})
```

This is not a provider adapter. It is a convenience SDK for publishing an MCP/process tool with a standard lifecycle.

The minimum internal call pipeline is:

```text
decode and validate input
  -> before middleware
  -> begin transaction if declared
  -> tool handler
  -> after middleware
  -> commit or rollback
  -> normalized result and evidence
```

The SDK should provide standard middleware for common concerns:

- workspace containment;
- expected digest/CAS checks;
- atomic file replacement;
- stdout/stderr capture;
- artifact registration;
- redaction;
- timeout/cancellation;
- process execution;
- commit/rollback;
- bounded result projection.

Domain-specific middleware such as route validation remains in the domain package.

### 7.3 Declarative composition without a new programming language

The exact lifecycle should be defined next to the tool implementation, not repeated in every flow.

A flow references:

```text
agent: route.builder
```

The `route.builder` profile includes the `route.write` tool. The tool itself already owns its atomic invariants. Takt automatically supplies policy and durable lifecycle around it.

An organization may attach additional named Takt actions around a toolset, but ordinary users should not write arbitrary pre/post expressions in workflow YAML.

### 7.4 Debugging model

Every tool call should have one durable trace with explicit stages:

```text
requested
authorized | denied
before.started
before.completed | failed
handler.started
handler.completed | failed
after.started
after.completed | failed
transaction.committed | rolled_back
milestone.updated
```

The trace stores:

- run, step, episode and call IDs;
- tool/profile version and fingerprint;
- bounded input/output previews plus full artifact references;
- decision/rejection reason;
- before/after results;
- candidate digest before and after;
- transaction result;
- elapsed time and diagnostic fingerprint.

The desired debugging commands are conceptually:

```text
takt tool inspect route.write
takt tool call route.write --input input.json --trace
takt episode explain <episode-id>
takt episode events <episode-id>
```

`tool call --trace` must execute the same decode/before/handler/after/transaction pipeline used inside an agent episode. A developer can therefore debug the tool without invoking a model.

SDK contract tests should allow a fake call sequence:

```text
given episode state S
when route.write(input)
expect before decisions
expect handler result
expect validation result
expect commit or rollback
expect next milestone/toolset
```

### 7.5 Flow integration

The flow does not encode the implementation of the handlers. It sees only the normalized result and process transition:

```text
agent episode
  -> tool transaction result
  -> episode milestone/completion
  -> deterministic action or next agent step
```

If a post-check returns recoverable product feedback, the current episode can continue only when its tool schema and role remain valid. If the required tool surface changes, Takt starts a fresh episode with the next profile. This incorporates domain evidence without hardcoding domain rules into core.

## 8. Cross-domain examples

### 8.1 Feature from tracker ticket

```text
tracker.load
  -> code.analyst (tracker/repository read-only)
  -> code.builder (repository write + test tools)
  -> project.validate
  -> code.reviewer (read-only)
  -> scm.create_change (receipt/reconcile)
```

### 8.2 Pull-request review

```text
scm.load_change
  -> code.reviewer (read-only tools)
  -> optional security/lint actions in parallel
  -> review.synthesize
  -> scm.publish_review
```

### 8.3 Vulnerability analysis

```text
security.scan
  -> security.analyst
  -> approval when remediation changes behavior
  -> code.builder with scoped write tools
  -> security.rescan
  -> report.publish
```

### 8.4 Schema-constrained artifact generation

```text
requirements intake
  -> route.analyst
  -> route.builder with authority/read/write tools
  -> route.validate
  -> acceptance-derived local test
  -> route.repair episode when RED
  -> Docker acceptance
  -> same-digest repeat
```

These flows use different domain packs but the same Flow, AgentEpisode, Action, Tool and Evidence mechanics.

## 9. Candidate deletion/rewrite scope

No compatibility layer is required. Subject to the next design decision, the rewrite may delete or replace:

- public `WorkflowPlan`;
- public `workflow` versus `subworkflow` distinction;
- `script.runtime` backend enum and special validation runtime;
- BlockPackage checks/output-path duplication;
- low-level `plan-to-pr` JSON contract;
- assistant-owned domain status/code enums used as gates;
- duplicated raw flow/block authoring surfaces;
- host-control and external-worker protocols that overlap without one strict episode contract.

Implementation mechanisms such as scheduler, worktree, Store, events, redaction, adapter process management and deterministic validators should be retained only where they support the new boundary cleanly.

## 10. Design unknowns

| ID | Priority | Unknown | Why it matters | Proposed closure |
|---|---|---|---|---|
| U-01 | P0 | Can the selected Pi/OpenCode execution path provide real pre-tool and pre-completion control for worker episodes? | Without it, toolsets and completion gates are advisory. | Live minimal host experiment with one denied tool, one post-tool result and one rejected completion. |
| U-02 | P0 | What is the canonical AgentEpisode protocol after removing overlap between session adapter, external worker and host-control? | Determines the new core boundary and what can be deleted. | Design one request/event/decision protocol and validate it with a fake host before product code. |
| U-03 | P1 | Which tool lifecycle belongs to provider SDK versus Takt policy? | Incorrect ownership either leaks domain logic into Takt or forces every tool developer to implement orchestration. | Prototype one domain tool and one generic code tool through the same Tool SDK pipeline. |
| U-04 | P1 | How are named toolsets/skills mapped to provider-specific tool names? | Determines whether profiles remain portable across Pi/OpenCode. | Capability-mapping prototype plus fail-closed mismatch test. |
| U-05 | P1 | How does one reusable process reference select inline versus child/worktree execution? | Replaces workflow/subworkflow without hiding important isolation. | Compare always-child, target-owned policy and caller override on three representative flows. |
| U-06 | P1 | What exact result/assurance vocabulary is visible to users? | Run completion and accepted product result must not be conflated. | Design CLI preview/status/result examples before schemas. |
| U-07 | P1 | Which parts of the existing scheduler/store survive the rewrite? | Prevents unnecessary deletion of proven durability while avoiding compatibility-driven complexity. | Trace new Flow/AgentEpisode lifecycle through current Runner and list mismatches before implementation. |
| U-08 | P2 | Should simple process tools require JSON stdin/stdout or can exit-code-only commands be first-class? | Affects ease of custom tool integration. | Two small authoring prototypes and developer feedback. |

## 11. Recommended next design discussion

The next discussion should focus on the domain-tool developer experience for one concrete tool without making the overall architecture domain-specific.

Recommended example:

```text
route.write
```

Questions to resolve:

1. What exact code/config does a domain-tool developer write?
2. Which before checks are generic SDK middleware?
3. Which after checks are domain-owned?
4. How is the transaction defined and tested?
5. How does Takt expose the tool only in selected episodes?
6. What events and artifacts are visible when the tool is denied, rolled back or accepted?
7. How can the developer run the identical lifecycle locally without an LLM?

Only after this vertical contract is understandable should the overall Flow schema and implementation plan be finalized.

## 12. Design gate

**Status: CONDITIONAL.**

The direction is supported by code and external domain-integration evidence:

- general Flow remains;
- strictness moves into reusable agent/action/tool contracts;
- tool availability and completion are enforced at the host boundary;
- immediate feedback is attached to tool execution;
- domain tools may own transactions;
- users connect MCP/process/script tools without provider adapters.

Implementation is not ready until U-01 and U-02 are closed and the U-03 developer experience is demonstrated with a small executable prototype.

## 13. Transfer contract for later implementation

Do not begin by adding more fields to the existing Workflow schema.

First validate the AgentEpisode and Tool lifecycle boundaries with a fake host and one domain tool. If an implementation fact changes host capabilities, tool ownership, durable state or the Flow/AgentEpisode boundary, stop and return to design analysis instead of silently adapting the old architecture.
