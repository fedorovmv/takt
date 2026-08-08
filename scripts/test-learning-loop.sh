#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
WORK="$TMP/work"
mkdir -p "$ROOT/bin" "$WORK/.takt/runs/run-learning-a" "$WORK/.takt/runs/run-learning-b"
go build -o "$ROOT/bin/takt" "$ROOT/cmd/takt"
for id in run-learning-a run-learning-b; do
  cat > "$WORK/.takt/runs/$id/state.json" <<JSON
{
  "id": "$id",
  "status": "failed",
  "workflow_path": "workflow.yaml",
  "config_path": "config.yaml",
  "workspace": "$WORK",
  "nodes": {
    "validate": {
      "status": "failed",
      "diagnostic": {
        "code": "VALIDATION",
        "kind": "quality",
        "message": "same durable validation failure",
        "fingerprint": "sha256:learning-repeat"
      }
    }
  },
  "approvals": {},
  "revision": 0
}
JSON
done

"$ROOT/bin/takt" learn scan --workspace "$WORK" --min-runs 2 --json > "$TMP/scan.json"
"$ROOT/bin/takt" learn propose --workspace "$WORK" \
  --pattern diagnostic:sha256:learning-repeat \
  --kind skill --name repeated-validation \
  --benefit 'prevent recurrence of the observed validation failure' \
  --json > "$TMP/propose.json"

cat > "$TMP/id.go" <<'GO'
package main
import("encoding/json";"fmt";"os")
func main(){b,err:=os.ReadFile(os.Args[1]);if err!=nil{panic(err)};var v struct{Result struct{ID string `json:"id"`;Status string `json:"status"`;Pattern struct{Count int `json:"count"`} `json:"pattern"`} `json:"result"`};if err:=json.Unmarshal(b,&v);err!=nil{panic(err)};if v.Result.ID==""||v.Result.Status!="pending_review"||v.Result.Pattern.Count!=2{panic("invalid learning proposal")};fmt.Print(v.Result.ID)}
GO
ID="$(cd "$ROOT" && go run "$TMP/id.go" "$TMP/propose.json")"

if "$ROOT/bin/takt" learn stage "$ID" --workspace "$WORK" --json >/dev/null 2>&1; then
  echo 'learning proposal staged before human review/evaluation' >&2
  exit 1
fi
"$ROOT/bin/takt" learn review "$ID" --workspace "$WORK" --decision accept --reason 'candidate is reusable' --json > "$TMP/review.json"
if "$ROOT/bin/takt" learn stage "$ID" --workspace "$WORK" --json >/dev/null 2>&1; then
  echo 'learning proposal staged before evaluation' >&2
  exit 1
fi

cat > "$TMP/evaluation.json" <<'JSON'
{
  "report_version": "takt-evaluation-matrix/v1alpha1",
  "matrix_fingerprint": "sha256:learning-matrix-fixture",
  "benchmark_id": "learning-regression",
  "passed": true,
  "gates": [
    {"strategy": "candidate", "passed": true, "message": "no regression"}
  ]
}
JSON
"$ROOT/bin/takt" learn evaluate "$ID" --workspace "$WORK" --report "$TMP/evaluation.json" --json > "$TMP/evaluate.json"
"$ROOT/bin/takt" learn stage "$ID" --workspace "$WORK" --json > "$TMP/stage.json"

READY="$WORK/.takt/learning/ready/$ID/SKILL.md"
test -f "$READY"
grep -q 'diagnostic:sha256:learning-repeat' "$READY"
test ! -e "$WORK/.takt/packages/repeated-validation"

cat > "$TMP/assert.go" <<'GO'
package main
import("encoding/json";"os")
func main(){b,err:=os.ReadFile(os.Args[1]);if err!=nil{panic(err)};var v struct{Result struct{Status string `json:"status"`;ReadyPath string `json:"ready_path"`;Review *struct{Decision string `json:"decision"`} `json:"review"`;Evaluation *struct{Passed bool `json:"passed"`;BenchmarkID string `json:"benchmark_id"`;MatrixFingerprint string `json:"matrix_fingerprint"`} `json:"evaluation"`} `json:"result"`};if err:=json.Unmarshal(b,&v);err!=nil{panic(err)};r:=v.Result;if r.Status!="ready"||r.ReadyPath==""||r.Review==nil||r.Review.Decision!="accept"||r.Evaluation==nil||!r.Evaluation.Passed||r.Evaluation.BenchmarkID==""||r.Evaluation.MatrixFingerprint==""{panic("invalid staged learning proposal")}}
GO
(cd "$ROOT" && go run "$TMP/assert.go" "$TMP/stage.json")

echo 'human-reviewed learning loop: PASS'
