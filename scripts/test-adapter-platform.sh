#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/work"
go build -o "$tmp/bin/takt" ./cmd/takt
go build -o "$tmp/bin/takt-fake-domain-adapter" ./cmd/takt-fake-domain-adapter
export PATH="$tmp/bin:$PATH"
export TAKT_FAKE_ADAPTER_STATE="$tmp/adapter-state"

cp examples/adapter-platform/config.yaml "$tmp/work/config.yaml"
cp examples/adapter-platform/workflow.yaml "$tmp/work/workflow.yaml"

"$tmp/bin/takt" adapter doctor tracker --workspace "$tmp/work" --config config.yaml --json > "$tmp/tracker.json"
"$tmp/bin/takt" adapter doctor scm --workspace "$tmp/work" --config config.yaml --json > "$tmp/scm.json"
grep -q 'item.get' "$tmp/tracker.json"
grep -q 'change.create' "$tmp/scm.json"

# Doctor must be an actionable health check: a configured reconcile mapping
# that the adapter does not declare produces a report and a non-zero exit.
python_bin="$(command -v python3 || command -v python || true)"
if [[ -z "$python_bin" ]]; then
  echo "python3 or python is required for adapter platform fixture mutation" >&2
  exit 1
fi
"$python_bin" - "$tmp/work/config.yaml" "$tmp/work/bad-config.yaml" <<'PY'
from pathlib import Path
import sys
s=Path(sys.argv[1]).read_text()
s=s.replace('      change.review: scm.change.review.reconcile', '      change.review: scm.change.review.reconcile\n      repository.get: scm.repository.get.reconcile')
Path(sys.argv[2]).write_text(s)
PY
if "$tmp/bin/takt" adapter doctor scm --workspace "$tmp/work" --config bad-config.yaml --json > "$tmp/bad-doctor.json" 2>/dev/null; then
  echo "adapter doctor accepted a broken configured reconcile capability" >&2
  exit 1
fi
grep -q '"status": "error"' "$tmp/bad-doctor.json"
grep -q 'configured reconcile operation not declared: repository.get' "$tmp/bad-doctor.json"

"$tmp/bin/takt" validate "$tmp/work/workflow.yaml" --config "$tmp/work/config.yaml"
"$tmp/bin/takt" run "$tmp/work/workflow.yaml" --config "$tmp/work/config.yaml" --workspace "$tmp/work" --json > "$tmp/run.json"
grep -q '"status": "completed"' "$tmp/run.json"
grep -q '"domain_operation"' "$tmp/run.json"
grep -q '"reconcile_status": "applied"' "$tmp/run.json"

echo "adapter platform contract: PASS"
