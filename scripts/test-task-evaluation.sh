#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$ROOT/bin"
go build -o "$ROOT/bin/takt" "$ROOT/cmd/takt"
go build -o "$ROOT/bin/takt-fake-code-agent" "$ROOT/cmd/takt-fake-code-agent"

make_template() {
  local dir="$1"
  local force_template="$2"
  "$ROOT/bin/takt" init code --dir "$dir" --json >/dev/null
  cat > "$dir/.takt/config.yaml" <<CFG
apiVersion: takt/v1alpha1
kind: Config
default_assistant: fixture
models:
  routing:
    provider: fixture
    id: routing
  implementation:
    provider: fixture
    id: implementation
  review:
    provider: fixture
    id: review
assistants:
  fixture:
    type: process
    argv:
      - $ROOT/bin/takt-fake-code-agent
CFG
  if [ "$force_template" = yes ]; then
    cat >> "$dir/.takt/config.yaml" <<'CFG'
    env:
      FAKE_TASK_ROUTER_FORCE_TEMPLATE: "1"
CFG
  fi
  cat >> "$dir/.takt/config.yaml" <<'CFG'
    capabilities:
      - tool_policy
      - skills
      - mcp
      - sandbox_filesystem
CFG
  git -C "$dir" init -q
  git -C "$dir" config user.email fixture@example.com
  git -C "$dir" config user.name Fixture
  printf 'fixture\n' > "$dir/README.fixture"
  cat >> "$dir/.git/info/exclude" <<'EXCLUDE'
.takt/plans/
.takt/runs/
.takt/locks/
.takt/host-sessions/
.takt/notifications/
EXCLUDE
  git -C "$dir" add .
  git -C "$dir" commit -qm 'task evaluation fixture'
}

make_template "$TMP/baseline" yes
make_template "$TMP/candidate" no

cat > "$TMP/cases.yaml" <<'YAML'
apiVersion: takt/evaluation/v1alpha1
kind: TaskCaseManifest
cases:
  ordinary:
    goal: Implement an ordinary repository change
    expected_route: template
    expected_template: simple-reliable
    expected_status: completed
    labels:
      category: routing
  dynamic:
    goal: fixture dynamic audit
    expected_route: dynamic
    expected_status: completed
    labels:
      category: dynamic
  replan:
    goal: fixture dynamic replan
    expected_route: dynamic
    expected_status: completed
    min_plan_revisions: 2
    labels:
      category: replan
YAML

cat > "$TMP/matrix.yaml" <<'YAML'
apiVersion: takt/evaluation/v1alpha1
kind: TaskEvaluationMatrix
metadata:
  name: task-dynamic
benchmark:
  id: task-dynamic-v1
  baseline_strategy: force-template
  cases: cases.yaml
  repeat: 2
  profile: code
strategies:
  - id: force-template
    workspace_template: baseline
  - id: semantic-router
    workspace_template: candidate
gates:
  - strategy: semantic-router
    route_accuracy_min: 1
    final_success_rate_min: 1
    replan_expectation_rate_min: 1
    unexpected_needs_input_max: 0
    router_fallbacks_max: 0
YAML

"$ROOT/bin/takt" eval task-benchmark "$TMP/matrix.yaml" --output "$TMP/out" --replace --json > "$TMP/result.json"
"$ROOT/bin/takt" eval report "$TMP/out" --json > "$TMP/report.json"

cat > "$TMP/assert.go" <<'GO'
package main

import (
  "encoding/json"
  "fmt"
  "os"
)

type summary struct {
  Total int `json:"total"`
  RouteAccuracy float64 `json:"route_accuracy"`
  FinalSuccessRate float64 `json:"final_success_rate"`
  ReplanExpectationRate float64 `json:"replan_expectation_rate"`
}
type run struct {
  CaseID string `json:"case_id"`
  Repeat int `json:"repeat"`
  PlanRevisions int `json:"plan_revisions"`
}
type strategy struct {
  ID string `json:"id"`
  Summary summary `json:"summary"`
  Runs []run `json:"runs"`
}
type comparison struct {
  CandidateOnlyRouteCorrect int `json:"candidate_only_route_correct"`
  BothRouteCorrect int `json:"both_route_correct"`
}
type report struct {
  ReportVersion string `json:"report_version"`
  Repeat int `json:"repeat"`
  Passed bool `json:"passed"`
  Strategies []strategy `json:"strategies"`
  Comparisons []comparison `json:"comparisons"`
}
func fail(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...); os.Exit(1) }
func main() {
  raw, err := os.ReadFile(os.Args[1]); if err != nil { fail("read: %v", err) }
  var r report; if err := json.Unmarshal(raw, &r); err != nil { fail("decode: %v", err) }
  if r.ReportVersion == "" {
    var envelope struct { Result json.RawMessage `json:"result"` }
    if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Result) == 0 { fail("decode envelope: %v", err) }
    if err := json.Unmarshal(envelope.Result, &r); err != nil { fail("decode result: %v", err) }
  }
  if r.ReportVersion != "takt-task-evaluation-matrix/v1alpha1" || !r.Passed || r.Repeat != 2 { fail("header=%+v", r) }
  byID := map[string]strategy{}; for _, s := range r.Strategies { byID[s.ID] = s }
  base, ok := byID["force-template"]; if !ok { fail("missing baseline") }
  cand, ok := byID["semantic-router"]; if !ok { fail("missing candidate") }
  if base.Summary.Total != 6 || base.Summary.RouteAccuracy < 0.3333333333 || base.Summary.RouteAccuracy > 0.3333333334 { fail("baseline summary=%+v", base.Summary) }
  if cand.Summary.Total != 6 || cand.Summary.RouteAccuracy != 1 || cand.Summary.FinalSuccessRate != 1 || cand.Summary.ReplanExpectationRate != 1 { fail("candidate summary=%+v", cand.Summary) }
  if len(r.Comparisons) != 1 || r.Comparisons[0].CandidateOnlyRouteCorrect != 4 || r.Comparisons[0].BothRouteCorrect != 2 { fail("comparison=%+v", r.Comparisons) }
  replanRepeats := map[int]bool{}
  for _, item := range cand.Runs { if item.CaseID == "replan" { if item.PlanRevisions != 2 { fail("replan run=%+v", item) }; replanRepeats[item.Repeat] = true } }
  if !replanRepeats[1] || !replanRepeats[2] { fail("missing repeated replan runs: %+v", replanRepeats) }
}
GO

(cd "$ROOT" && go run "$TMP/assert.go" "$TMP/result.json")
(cd "$ROOT" && go run "$TMP/assert.go" "$TMP/report.json")

echo 'task-level dynamic evaluation: PASS'
