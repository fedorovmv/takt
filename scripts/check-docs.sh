#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# Hashes of documents accidentally restored from v0.1.1 during the v0.1.4 build.
# A current release must never reproduce those exact files.
for entry in \
  "ARCHITECTURE_DECISIONS.md|43125bc2ad1b823aabf35ed5bdf9e20703d9be7fae6fa3540ff8235b94d6eca7" \
  "DEVELOPMENT.md|f2f9c13cbb57e33282bd1513d73b0acece4662326525149efd8b020a4e8ab026" \
  "docs/03-specification.md|903a3b41506c4996c2d2f89d851f60d4a2fff01c698b7a00ba45a138d59f0b83" \
  "docs/08-target-v0.2.md|9f8f74e228fd473e1a08559bc3aecd9ec6a3133eaa90706fde84e9e921142326" \
  "docs/10-assistant-adapter-spec.md|715f491a0e953b49664a773a62c83f7fd0f5355af0eb216485d202254c041d4f" \
  "docs/12-document-map.md|8d03b24932e4c61df792ce2f475af4691be77d2f6ae05f888055c73ef90d8f14" \
  "docs/15-coding-agent-start.md|7a49baec4724a41db4e88e4206b6bcfe19a57a0eb551dcf35921c4b4ce764bea"
do
  file="${entry%%|*}"
  forbidden="${entry#*|}"
  [[ -f "$file" ]] || { echo "missing documentation file: $file" >&2; exit 1; }
  actual="$(sha256_file "$file")"
  if [[ "$actual" == "$forbidden" ]]; then
    echo "documentation regression: $file equals the v0.1.1 baseline" >&2
    exit 1
  fi
done

for check in \
  "AGENTS.md|Takt — Go-runtime" \
  "AGENTS.md|quality_node_status=completed" \
  "AGENTS.md|make check" \
  "AGENTS.md|skills/takt/SKILL.md" \
  "README.md|Скилл для настройки Takt" \
  "skills/takt/SKILL.md|name: takt" \
  "skills/takt/README.md|Takt authoring skill" \
  "skills/takt/VERSION|0.19.0" \
  "skills/takt/SKILL.md|Узел определяет ровно одно действие" \
  "skills/takt/SKILL.md|takt validate" \
  "skills/takt/references/configuration.md|Приоритет настроек" \
  "skills/takt/references/workflows.md|Approval, \`subworkflow\`, \`foreach\` и governed \`workflow\`" \
  "skills/takt/references/patterns.md|валидатор → feedback → retry" \
  "skills/takt/references/troubleshooting.md|Workflow валиден, но не запускается" \
  "skills/takt/assets/validated-agent-profile/.takt/workflows/basic.yaml|model: deep" \
  "skills/takt/assets/validated-agent-profile/.takt/workflows/validated.yaml|action: retry" \
  "docs/32-takt-authoring-skill-v0.1.18.md|scripts/test-takt-skill.sh" \
  "README.md|Краткие правила для кодовых агентов" \
  "DEVELOPMENT.md|\`AGENTS.md\`" \
  "docs/12-document-map.md|Правила работы кодовых агентов" \
  "ARCHITECTURE_DECISIONS.md|ADR-019" \
  "ARCHITECTURE_DECISIONS.md|ADR-020" \
  "ARCHITECTURE_DECISIONS.md|ADR-021" \
  "ARCHITECTURE_DECISIONS.md|ADR-022" \
  "ARCHITECTURE_DECISIONS.md|ADR-023" \
  "ARCHITECTURE_DECISIONS.md|ADR-024" \
  "ARCHITECTURE_DECISIONS.md|ADR-025" \
  "ARCHITECTURE_DECISIONS.md|ADR-026" \
  "ARCHITECTURE_DECISIONS.md|ADR-027" \
  "ARCHITECTURE_DECISIONS.md|ADR-028" \
  "ARCHITECTURE_DECISIONS.md|ADR-029" \
  "ARCHITECTURE_DECISIONS.md|ADR-030" \
  "ARCHITECTURE_DECISIONS.md|ADR-031" \
  "docs/33-opencode-adapter-v0.1.19.md|opencode run --format json" \
  "docs/34-opencode-provider-diagnostics-v0.1.20.md|OpenCode diagnostics" \
  "docs/35-profile-packages-code-v0.1.21.md|Markdown-планом" \
  "ARCHITECTURE_DECISIONS.md|ADR-032" \
  "ARCHITECTURE_DECISIONS.md|ADR-033" \
  "docs/36-workflow-composition-v0.1.22.md|Последовательный foreach" \
  "docs/37-composition-hardening-v0.1.23.md|Публичная проекция Run" \
  "ARCHITECTURE_DECISIONS.md|ADR-034" \
  "ARCHITECTURE_DECISIONS.md|ADR-035" \
  "ARCHITECTURE_DECISIONS.md|ADR-037" \
  "ARCHITECTURE_DECISIONS.md|ADR-038" \
  "docs/39-git-worktree-isolation-v0.1.25.md|Router-aware isolation" \
  "ARCHITECTURE_DECISIONS.md|ADR-039" \
  "ARCHITECTURE_DECISIONS.md|ADR-040" \
  "ARCHITECTURE_DECISIONS.md|ADR-041" \
  "ARCHITECTURE_DECISIONS.md|ADR-042" \
  "ARCHITECTURE_DECISIONS.md|ADR-043" \
  "docs/44-local-mcp-control-plane-v0.1.30.md|takt.run.events" \
  "docs/45-agent-events-external-executor-v0.1.31.md|takt.node.claim" \
  "docs/45-agent-events-external-executor-v0.1.31.md|events.idx" \
  "docs/06-roadmap.md|Выполнено в v0.1.31-alpha" \
  "scripts/test-external-executor.sh|external node executor contract: PASS" \
  "schemas/workflow.schema.json|executor" \
  "schemas/run-state.schema.json|externalExecutionState" \
  "skills/takt/references/mcp.md|takt.node.complete" \
  "ARCHITECTURE_DECISIONS.md|ADR-044" \
  "ARCHITECTURE_DECISIONS.md|ADR-045" \
  "ARCHITECTURE_DECISIONS.md|ADR-046" \
  "ARCHITECTURE_DECISIONS.md|ADR-047" \
  "ARCHITECTURE_DECISIONS.md|ADR-048" \
  "ARCHITECTURE_DECISIONS.md|ADR-049" \
  "ARCHITECTURE_DECISIONS.md|ADR-050" \
  "ARCHITECTURE_DECISIONS.md|ADR-051" \
  "ARCHITECTURE_DECISIONS.md|ADR-052" \
  "ARCHITECTURE_DECISIONS.md|ADR-055" \
  "ARCHITECTURE_DECISIONS.md|ADR-056" \
  "docs/51-autonomous-run-operations-v0.1.37.md|takt run pause" \
  "docs/51-autonomous-run-operations-v0.1.37.md|takt.notify.list" \
  "docs/06-roadmap.md|Выполнено в v0.1.37-alpha" \
  "scripts/test-autonomous-runs.sh|autonomous run operations contract: PASS" \
  "schemas/run-state.schema.json|abandoned" \
  "skills/takt/references/mcp.md|takt.run.attention" \
  "README.md|Autonomous Run Operations v0.1.37" \
  "schemas/notification-config.schema.json|Takt local notification configuration" \
  "examples/autonomous-runs/notifications.yaml|NotificationConfig" \
  "integrations/coding-agent-host-control/README.md|guarded" \
  "docs/50-coding-agent-host-control-v0.1.36.md|bundled Pi extension" \
  "docs/49-trusted-block-packages-v0.1.35.md|BlockPackage" \
  "docs/49-trusted-block-packages-v0.1.35.md|takt block validate" \
  "docs/06-roadmap.md|Выполнено в v0.1.35-alpha" \
  "schemas/block-package.schema.json|Takt trusted block package" \
  "schemas/profile.schema.json|block_packages" \
  "internal/profile/builtin/code/profile.yaml|block_packages" \
  "internal/profile/builtin/code/workflows/blocks/package.yaml|code-core" \
  "examples/corporate-block-package/package.yaml|corporate-engineering" \
  "scripts/test-block-packages.sh|trusted block package contract: PASS" \
  "skills/takt/SKILL.md|Доверенные пакеты блоков" \
  "skills/takt/references/mcp.md|takt.block.list" \
  "docs/48-dynamic-takt-v0.1.34.md|WorkflowPlan" \
  "docs/48-dynamic-takt-v0.1.34.md|takt.run.steer" \
  "docs/06-roadmap.md|Выполнено в v0.1.34-alpha" \
  "skills/takt/SKILL.md|Dynamic Takt из кодинг-агента" \
  "skills/takt/references/mcp.md|takt.plan.promote" \
  "schemas/workflow.schema.json|validation" \
  "schemas/workflow-plan.schema.json|WorkflowPlan" \
  "scripts/test-dynamic-takt.sh|dynamic Takt contract: PASS" \
  "docs/47-authoring-local-daemon-v0.1.33.md|takt daemon start" \
  "docs/47-authoring-local-daemon-v0.1.33.md|${path:-default}" \
  "scripts/test-authoring.sh|authoring contract: PASS" \
  "scripts/test-daemon.sh|daemon contract: PASS" \
  "schemas/workflow.schema.json|always_run" \
  "schemas/workflow.schema.json|idle_timeout" \
  "skills/takt/SKILL.md|--warnings-as-errors" \
  "SECURITY.md|Локальный daemon" \
  "docs/46-controlled-agent-events-deep-workflows-v0.1.32.md|assistant.tool.requested" \
  "docs/46-controlled-agent-events-deep-workflows-v0.1.32.md|scripts/test-deep-code-workflows.sh" \
  "docs/06-roadmap.md|Выполнено в v0.1.33-alpha" \
  "docs/06-roadmap.md|Приоритет 1. Domain Adapter SDK" \
  "scripts/test-deep-code-workflows.sh|deep code workflows: PASS" \
  "scripts/test-mcp.sh|takt.node.tool.request" \
  "schemas/assistant-protocol.schema.json|takt-assistant/v1alpha2" \
  "schemas/workflow.schema.json|tool_approval" \
  "schemas/workflow.schema.json|uniqueItems" \
  "internal/profile/builtin/code/workflows/fix-github-issue.yaml|ISSUE_NOT_REPRODUCED" \
  "internal/profile/builtin/code/workflows/plan-to-pr.yaml|recovery-report" \
  "docs/03-specification.md|server/discover" \
  "skills/takt/references/mcp.md|takt.run.start" \
  "scripts/test-mcp.sh|local MCP contract: PASS" \
  "README.md|Локальное управление через MCP и daemon" \
  "docs/43-script-nodes-typed-artifacts-v0.1.29.md|Script-узел" \
  "docs/43-script-nodes-typed-artifacts-v0.1.29.md|takt artifacts" \
  "scripts/test-script-artifacts.sh|script and typed artifact contract: PASS" \
  "schemas/workflow.schema.json|output_type" \
  "schemas/run-state.schema.json|producer_run_id" \
  "README.md|Script-узлы и типизированные артефакты" \
  "docs/12-document-map.md|43-script-nodes-typed-artifacts-v0.1.29.md" \
  "docs/42-governed-child-fanout-v0.1.28.md|child_run.fan_out.linked" \
  "docs/42-governed-child-fanout-v0.1.28.md|max_parallel" \
  "scripts/test-child-fanout.sh|governed child fan-out contract: PASS" \
  "schemas/workflow.schema.json|fan_out" \
  "schemas/run-state.schema.json|child_runs" \
  "internal/profile/builtin/code/VERSION|0.12.0" \
  "README.md|Динамический fan-out v0.1.28" \
  "docs/12-document-map.md|42-governed-child-fanout-v0.1.28.md" \
  "docs/41-node-capability-policies-v0.1.27.md|Capability negotiation" \
  "docs/41-node-capability-policies-v0.1.27.md|allowed_tools: []" \
  "scripts/test-policies.sh|node capability policy contract: PASS" \
  ".github/workflows/ci.yml|macos-latest" \
  "schemas/run-state.schema.json|inherited_policy" \
  "schemas/assistant-protocol.schema.json|tools_restricted" \
  "README.md|Политики возможностей узла" \
  "docs/40-governed-child-runs-v0.1.26.md|Governed child Runs" \
  "docs/40-governed-child-runs-v0.1.26.md|takt children" \
  "scripts/test-worktree.sh|git worktree contract: PASS" \
  "scripts/test-child-runs.sh|governed child run contract: PASS" \
  "README.md|takt children <run-id>" \
  "docs/03-specification.md|governed child Run" \
  "schemas/run-state.schema.json|parent_run_id" \
  "README.md|takt worktree list/remove/prune" \
  "docs/38-archon-workflow-catalog-v0.1.24.md|19 типовых процессов" \
  "docs/03-specification.md|output_format" \
  "docs/03-specification.md|one_success" \
  "docs/03-specification.md|parallel: true" \
  "internal/profile/builtin/code/profile.yaml|Evidence-driven catalog of 19" \
  "docs/03-specification.md|Подключает отдельный \`takt/v1alpha1 Workflow\`" \
  "docs/03-specification.md|отдельного YAML/JSON-файла" \
  "skills/takt/references/workflows.md|## Subworkflow" \
  "skills/takt/assets/validated-agent-profile/.takt/workflows/composition.yaml|foreach:" \
  "examples/composition/workflow.yaml|items_from:" \
  "scripts/test-composition.sh|workflow composition: PASS" \
  "README.md|takt init code" \
  "schemas/profile.schema.json|Takt Profile" \
  "skills/takt/SKILL.md|Готовые профили" \
  "scripts/test-code-profile.sh|code profile catalog contract: PASS" \
  "docs/10-assistant-adapter-spec.md|OpenCode adapter реализован" \
  "docs/03-specification.md|### OpenCode assistant" \
  "skills/takt/references/configuration.md|Assistant opencode" \
  "skills/takt/assets/validated-agent-profile/.takt/workflows/opencode.yaml|assistant: opencode" \
  "examples/opencode-smoke/workflow.yaml|assistant: opencode" \
  "examples/authoring-daemon/workflow.yaml|always_run: true" \
  "examples/authoring-daemon/README.md|takt daemon start" \
  "scripts/test-opencode-adapter.sh|OpenCode adapter contract suite: PASS" \
  "docs/03-specification.md|allow_failure" \
  "docs/03-specification.md|родительский \`loop_group\`" \
  "docs/03-specification.md|официальный RPC-режим Pi" \
  "docs/09-runtime-semantics.md|Store.Commit" \
  "docs/09-runtime-semantics.md|loop_group exhausted" \
  "docs/09-runtime-semantics.md|v0.1.37-alpha" \
  "docs/10-assistant-adapter-spec.md|takt-assistant/v1alpha1" \
  "docs/10-assistant-adapter-spec.md|Pi adapter реализован как \`type: pi\`" \
  "docs/10-assistant-adapter-spec.md|Request.Metadata\` является optional" \
  "docs/10-assistant-adapter-spec.md|adapter ждёт \`agent_settled\`" \
  "docs/10-assistant-adapter-spec.md|per-attempt usage delta" \
  "docs/10-assistant-adapter-spec.md|приоритет \`timed_out\`/\`cancelled\`" \
  "docs/12-document-map.md|21-protocol-hardening-v0.1.7.md" \
  "docs/12-document-map.md|22-pi-adapter-v0.1.8.md" \
  "docs/12-document-map.md|23-pi-rpc-alignment-v0.1.9.md" \
  "docs/12-document-map.md|24-pi-context-usage-hardening-v0.1.10.md" \
  "docs/12-document-map.md|25-route-dsl-e2e-v0.1.11.md" \
  "docs/12-document-map.md|26-evaluation-runner-v0.1.12.md" \
  "docs/12-document-map.md|27-evaluation-isolation-report-v0.1.13.md" \
  "docs/12-document-map.md|28-benchmark-identity-quality-v0.1.14.md" \
  "docs/12-document-map.md|29-benchmark-metric-semantics-v0.1.15.md" \
  "docs/12-document-map.md|30-quality-envelope-semantics-v0.1.16.md" \
  "docs/12-document-map.md|31-quality-stdout-separation-v0.1.17.md" \
  "docs/12-document-map.md|32-takt-authoring-skill-v0.1.18.md" \
  "docs/12-document-map.md|33-opencode-adapter-v0.1.19.md" \
  "docs/12-document-map.md|34-opencode-provider-diagnostics-v0.1.20.md" \
  "docs/14-backlog-v0.2.md|TAKT-008. Fake assistant protocol suite — выполнено" \
  "docs/14-backlog-v0.2.md|TAKT-009. Specialized Pi adapter — выполнено" \
  "docs/15-coding-agent-start.md|takt-assistant/v1alpha1" \
  "docs/20-fake-assistant-contract-v0.1.6.md|OS exit code" \
  "docs/21-protocol-hardening-v0.1.7.md|обязаны совпадать всегда" \
  "docs/22-pi-adapter-v0.1.8.md|pi --mode rpc" \
  "docs/23-pi-rpc-alignment-v0.1.9.md|agent_settled" \
  "docs/23-pi-rpc-alignment-v0.1.9.md|attempt_delta" \
  "docs/24-pi-context-usage-hardening-v0.1.10.md|timeout + output overflow" \
  "docs/24-pi-context-usage-hardening-v0.1.10.md|исчез после \`agent_settled\`" \
  "docs/25-route-dsl-e2e-v0.1.11.md|Pi → validator → feedback → retry/resume" \
  "docs/25-route-dsl-e2e-v0.1.11.md|Result.Truncated = true" \
  "docs/26-evaluation-runner-v0.1.12.md|takt eval run" \
  "docs/26-evaluation-runner-v0.1.12.md|context.WithTimeout" \
  "docs/27-evaluation-isolation-report-v0.1.13.md|case_id" \
  "docs/27-evaluation-isolation-report-v0.1.13.md|diagnostic_output" \
  "docs/28-benchmark-identity-quality-v0.1.14.md|takt-validation/v1alpha1" \
  "docs/28-benchmark-identity-quality-v0.1.14.md|strategy.fingerprint" \
  "docs/28-benchmark-identity-quality-v0.1.14.md|responseModel" \
  "docs/28-benchmark-identity-quality-v0.1.14.md|workspace template" \
  "docs/29-benchmark-metric-semantics-v0.1.15.md|execution identity" \
  "docs/29-benchmark-metric-semantics-v0.1.15.md|amortized_end_to_end_ms_per_valid" \
  "docs/29-benchmark-metric-semantics-v0.1.15.md|completed" \
  "docs/30-quality-envelope-semantics-v0.1.16.md|completed && valid=true" \
  "docs/30-quality-envelope-semantics-v0.1.16.md|valid: false + exit 1" \
  "docs/31-quality-stdout-separation-v0.1.17.md|декодируется только из \`stdout\`" \
  "docs/31-quality-stdout-separation-v0.1.17.md|validator cache is cold" \
  "DEVELOPMENT.md|make route-benchmark" \
  "SECURITY.md|models.*.params" \
  "examples/route-dsl-eval/README.md|takt eval run" \
  "examples/route-dsl-benchmark/README.md|реального Pi" \
  "examples/route-dsl-e2e/workflow.yaml|validate-generated-route" \
  "scripts/test-route-dsl-e2e.sh|Route DSL end-to-end: PASS" \
  "scripts/test-route-dsl-eval.sh|takt eval run" \
  "schemas/run-state.schema.json|resumed" \
  "schemas/run-state.schema.json|assistant_version" \
  "schemas/run-state.schema.json|resolved_model" \
  "schemas/run-state.schema.json|executions" \
  "schemas/run-state.schema.json|stdout" \
  "schemas/run-state.schema.json|stderr" \
  "schemas/validation-result.schema.json|takt-validation/v1alpha1" \
  "schemas/evaluation-report.schema.json|takt-evaluation/v1alpha1" \
  "schemas/evaluation-report.schema.json|workspace_fingerprint" \
  "schemas/evaluation-report.schema.json|by_assistant_version" \
  "schemas/evaluation-report.schema.json|usage_by_execution_identity" \
  "schemas/evaluation-report.schema.json|amortized_end_to_end_ms_per_valid" \
  "schemas/evaluation-report.schema.json|mixed_execution_identity" \
  "schemas/evaluation-report.schema.json|stdout" \
  "schemas/evaluation-report.schema.json|stderr"
do
  file="${check%%|*}"
  text="${check#*|}"
  grep -Fq -- "$text" "$file" || {
    echo "documentation regression: '$text' is missing from $file" >&2
    exit 1
  }
done

for check in \
  "docs/50-coding-agent-host-control-v0.1.36.md|Coding Agent Host Control" \
  "docs/50-coding-agent-host-control-v0.1.36.md|strict" \
  "docs/12-document-map.md|50-coding-agent-host-control-v0.1.36.md" \
  "ARCHITECTURE_DECISIONS.md|ADR-053" \
  "ARCHITECTURE_DECISIONS.md|ADR-054" \
  "integrations/coding-agent-host-control/pi/index.ts|pi.registerCommand" \
  "integrations/coding-agent-host-control/opencode/index.ts|ctx.session.hook" \
  "scripts/test-host-control.sh|coding-agent host control contract: PASS"
do
  file="${check%%|*}"
  text="${check#*|}"
  grep -Fq -- "$text" "$file" || {
    echo "documentation regression: '$text' is missing from $file" >&2
    exit 1
  }
done

echo 'documentation check: PASS'
