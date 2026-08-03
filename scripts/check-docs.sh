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
  "ARCHITECTURE_DECISIONS.md|ADR-017" \
  "docs/03-specification.md|allow_failure" \
  "docs/03-specification.md|родительский \`loop_group\`" \
  "docs/09-runtime-semantics.md|Store.Commit" \
  "docs/09-runtime-semantics.md|loop_group exhausted" \
  "docs/10-assistant-adapter-spec.md|takt-assistant/v1alpha1" \
  "docs/12-document-map.md|20-fake-assistant-contract-v0.1.6.md" \
  "docs/14-backlog-v0.2.md|TAKT-008. Fake assistant protocol suite — выполнено" \
  "docs/15-coding-agent-start.md|takt-assistant/v1alpha1" \
  "docs/20-fake-assistant-contract-v0.1.6.md|resume-failed"
do
  file="${check%%|*}"
  text="${check#*|}"
  grep -Fq "$text" "$file" || {
    echo "documentation regression: '$text' is missing from $file" >&2
    exit 1
  }
done

echo 'documentation check: PASS'
