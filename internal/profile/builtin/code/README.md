# Takt code profile 0.16.0

The `code` profile is a smart-routed catalog of development workflows for a trusted local repository. Run the profile without a suffix to let the router select a workflow, or select one explicitly with `code:<name>`.


## Role/Brief contract

Dynamic blocks are bound to internal functional roles. Takt compiles a fresh `TaskBrief` for every worker phase, applies bounded context, path scope and required/preferred checks, and performs one bounded automatic repair for recoverable required-check failures. Users do not install these roles as separate coding-agent definitions.

The profile also records baseline evidence and check results in an internal `EvidenceManifest`. Known baseline failures do not trigger repair; final evidence is bound to a candidate content SHA-256, and material failures park the plan with a safe next action instead of entering an unbounded retry loop.
```bash
takt init code
takt workflow list code
takt workflow describe code:piv-loop
takt run code --input "Fix GitHub issue #123 and open a PR"
takt run code:comprehensive-pr-review --input "Review the current PR"
```

The router is an ordinary root Run in the control checkout. It returns schema-validated JSON and starts exactly one selected workflow as a governed child Run. The child has its own state, events, artifacts, usage and worktree policy, while the parent preserves the routing decision and link. Use `takt children <run-id>` to inspect the selected process and `takt cancel <run-id>` to cancel the tree.

## Included workflows

| Workflow | Purpose |
|---|---|
| `assist` | General questions, debugging, exploration, CI diagnosis, and one-off work |
| `fix-github-issue` | Classify issue, investigate or plan, implement, validate, create PR, smart review, self-fix |
| `create-issue` | Gather context in parallel, investigate, reproduce, deduplicate, and create an issue |
| `issue-review-full` | Fix an issue and run the full five-perspective review pipeline |
| `piv-loop` | Interactive Plan-Implement-Validate with repeated human feedback and approvals |
| `idea-to-pr` | Research an idea, plan, implement, validate, create PR, full review, self-fix |
| `plan-to-pr` | Execute an existing plan through PR and full review |
| `feature-development` | Implement an existing plan, validate, and create PR |
| `adversarial-dev` | Build a large feature/application and repeat adversarial review and repair |
| `smart-pr-review` | Classify change complexity and run only relevant reviewers in parallel |
| `comprehensive-pr-review` | Always run code, errors, tests, docs, and simplicity reviewers in parallel |
| `validate-pr` | Compare deterministic validation on base and feature branches |
| `architect` | Architectural sweep, human approval, implementation, and review |
| `refactor-safely` | Baseline behavior, refactor, compare validation, and repair regressions |
| `interactive-prd` | Build and approve a PRD through guided conversation |
| `ralph-dag` | Convert PRD to stories and implement one validated story per fresh iteration |
| `workflow-builder` | Generate and repeatedly validate a Takt workflow package |
| `remotion-generate` | Plan, generate, render-check, and review Remotion compositions |
| `resolve-conflicts` | Analyze both conflict sides, resolve, validate, and finish safely |

The comprehensive review uses `foreach.parallel` to schedule five independent review perspectives concurrently. `foreach.parallel: true` uses the same scheduler for parallel fan-out. Interactive workflow loops can pause on approval and resume the active iteration; the approval answer is cleared before the next iteration so each round obtains new human input.

## Configuration

The installed `.takt/config.yaml` contains three model aliases:

- `routing` for classifiers and the workflow router;
- `implementation` for code-changing agents;
- `review` for investigation and review agents.

All aliases can point to the same provider/model. The split exists so projects can tune cost and reasoning independently.

Every bundled workflow uses the logical assistant `coding-agent`. Select the concrete host once with `default_assistant` in `.takt/config.yaml`. Built-in Pi/OpenCode adapters and compatible `takt-assistant/v1alpha2` process wrappers share the same workflow catalog; Takt has no Kiro CLI dependency.

The validation hook uses `TAKT_VALIDATE_COMMAND` when set, then `scripts/verify.sh`, `make check`, Go tests, or npm tests. Set a project-specific command when automatic detection is insufficient.

## Repository overrides

Files installed under `.takt/profiles/code/` are ordinary project files. Teams can edit and commit workflow or command overrides. Re-running `takt init code --force` replaces them with the bundled version.


## Worktree policy

Mutating workflows create a `takt/<workflow>/<run-id>` branch and execute in `.takt/worktrees/<run-id>`. Clean successful worktrees are removed; a branch is deleted only when it has no commits beyond the recorded base. Dirty or failed worktrees are retained and shown by `takt worktree list`. Current-PR review and conflict-resolution workflows intentionally use the live checkout.

## Node policies

Routing and decision nodes use explicit empty tool/skill allowlists. Review agents deny edit/write tools while fix nodes retain mutation access. The policies are checked against adapter capabilities before invocation and are persisted in Run state.
## Script and artifact usage

The review perspective list is produced by the deterministic `tools/review-perspectives` script and stored as the `review-perspectives` JSON artifact. PIV, idea-to-PR and interactive PRD workflows register their accepted plan/PRD files as typed artifacts. Inspect them with `takt artifacts <run-id> --recursive`; downstream governed runs receive artifact references while provenance remains attached to the producing child Run.

