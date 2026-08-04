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
  "docs/03-specification.md|allow_failure" \
  "docs/03-specification.md|родительский \`loop_group\`" \
  "docs/03-specification.md|официальный RPC-режим Pi" \
  "docs/09-runtime-semantics.md|Store.Commit" \
  "docs/09-runtime-semantics.md|loop_group exhausted" \
  "docs/09-runtime-semantics.md|v0.1.17-alpha" \
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
  grep -Fq "$text" "$file" || {
    echo "documentation regression: '$text' is missing from $file" >&2
    exit 1
  }
done

echo 'documentation check: PASS'
