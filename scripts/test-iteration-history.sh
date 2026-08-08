#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$ROOT/bin"
go build -o "$ROOT/bin/takt" "$ROOT/cmd/takt"

cat > "$TMP/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
models: {}
assistants: {}
YAML

cat > "$TMP/workflow.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: iteration-history
nodes:
  - id: converge
    loop_group:
      max_iterations: 4
      nodes:
        - id: increment
          bash: |
            n=0
            test ! -f counter || n=$(cat counter)
            n=$((n + 1))
            printf '%s' "$n" > counter
            printf '%s' "$n"
        - id: check
          depends_on: [increment]
          bash: test "$(cat counter)" -ge 3
          allow_failure: true
      until:
        node: check
        exit_code: 0
YAML

"$ROOT/bin/takt" run "$TMP/workflow.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json > "$TMP/run.json"

cat > "$TMP/assert-history.go" <<'GO'
package main

import (
  "encoding/json"
  "fmt"
  "os"
  "path/filepath"
)

type iteration struct {
  Iteration int `json:"iteration"`
  Satisfied bool `json:"satisfied"`
}
type node struct {
  LoopIterations []iteration `json:"loop_iterations"`
  LoopPrevious map[string]any `json:"loop_previous"`
}
type state struct {
  ID string `json:"id"`
  Status string `json:"status"`
  Nodes map[string]node `json:"nodes"`
}
type envelope struct { Result state `json:"result"` }
func mustReadState(path string) state {
  b, err := os.ReadFile(path); if err != nil { panic(err) }
  var s state
  if err := json.Unmarshal(b, &s); err != nil { panic(err) }
  return s
}
func mustReadEnvelope(path string) state {
  b, err := os.ReadFile(path); if err != nil { panic(err) }
  var e envelope
  if err := json.Unmarshal(b, &e); err != nil { panic(err) }
  return e.Result
}
func check(label string, s state) {
  if s.Status != "completed" { panic(fmt.Sprintf("%s status=%q", label, s.Status)) }
  n, ok := s.Nodes["converge"]; if !ok { panic(label+": missing converge node") }
  if len(n.LoopIterations) != 3 { panic(fmt.Sprintf("%s iterations=%d", label, len(n.LoopIterations))) }
  want := []bool{false,false,true}
  for i, it := range n.LoopIterations {
    if it.Iteration != i+1 || it.Satisfied != want[i] {
      panic(fmt.Sprintf("%s iteration[%d]=%+v", label, i, it))
    }
  }
  if len(n.LoopPrevious) == 0 { panic(label+": loop_previous missing") }
}
func main() {
  if len(os.Args) != 3 { panic("usage: assert-history RUN_JSON WORKSPACE") }
  public := mustReadEnvelope(os.Args[1])
  check("public", public)
  durablePath := filepath.Join(os.Args[2], ".takt", "runs", public.ID, "state.json")
  durable := mustReadState(durablePath)
  check("durable", durable)
  if durable.ID != public.ID { panic("durable/public run id mismatch") }
}
GO
go run "$TMP/assert-history.go" "$TMP/run.json" "$TMP"

cat > "$TMP/unbounded.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: unbounded-loop
nodes:
  - id: loop
    loop_group:
      max_iterations: 65
      nodes:
        - id: check
          bash: "true"
      until:
        node: check
        exit_code: 0
YAML

if "$ROOT/bin/takt" validate "$TMP/unbounded.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json >"$TMP/unbounded.out" 2>"$TMP/unbounded.err"; then
  echo 'max_iterations=65 unexpectedly accepted' >&2
  exit 1
fi
grep -q 'max_iterations must be' "$TMP/unbounded.err"
grep -q '64' "$TMP/unbounded.err"

echo 'iteration history contract: PASS'
