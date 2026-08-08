#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$ROOT/bin" "$TMP/fakebin"
if grep -R 'takt/internal/' "$ROOT/reference/githubtask" "$ROOT/sdk/tasksource" --include='*.go' --exclude='*_test.go' >/dev/null; then
  echo 'task source public reference imports takt/internal' >&2
  exit 1
fi
if (cd "$ROOT" && go list -deps ./reference/githubtask ./sdk/tasksource | grep '^takt/internal/' >/dev/null); then
  echo 'task source reference dependency closure reaches takt/internal' >&2
  exit 1
fi
go build -o "$ROOT/bin/takt" "$ROOT/cmd/takt"
go build -o "$ROOT/bin/takt-fake-code-agent" "$ROOT/cmd/takt-fake-code-agent"
go build -o "$ROOT/bin/takt-github-task-source" "$ROOT/cmd/takt-github-task-source"
cat > "$TMP/fakebin/gh" <<'SH'
#!/bin/sh
set -eu
printf '%s\n' '{"number":42,"title":"Implement ordinary repository change","body":"Update behavior.\n- [ ] tests pass\n- [ ] docs updated","url":"https://github.com/acme/app/issues/42","labels":[{"name":"backend"}],"state":"OPEN","updatedAt":"2026-08-08T12:00:00Z"}'
SH
chmod +x "$TMP/fakebin/gh"
"$ROOT/bin/takt" init code --dir "$TMP/work" --json >/dev/null
cat > "$TMP/work/.takt/config.yaml" <<CFG
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
    argv: [$ROOT/bin/takt-fake-code-agent]
    capabilities:
      - tool_policy
      - skills
      - mcp
      - sandbox_filesystem
task_sources:
  github:
    transport: process
    argv: [$ROOT/bin/takt-github-task-source]
    timeout: 5s
CFG
git -C "$TMP/work" init -q
git -C "$TMP/work" config user.email fixture@example.com
git -C "$TMP/work" config user.name Fixture
printf 'base\n' > "$TMP/work/base.txt"
git -C "$TMP/work" add . && git -C "$TMP/work" commit -qm base
PATH="$TMP/fakebin:$PATH" "$ROOT/bin/takt" task start --workspace "$TMP/work" --source github --source-ref acme/app#42 --profile code --json > "$TMP/start.json"
cat > "$TMP/mcp-request.jsonl" <<'JSON'
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"takt.task.start","arguments":{"source":"github","source_ref":"acme/app#42","profile":"code"}}}
JSON
PATH="$TMP/fakebin:$PATH" "$ROOT/bin/takt" mcp --workspace "$TMP/work" --config "$TMP/work/.takt/config.yaml" < "$TMP/mcp-request.jsonl" > "$TMP/mcp-response.jsonl"
cat > "$TMP/assert.go" <<'GO'
package main
import("encoding/json";"fmt";"os")
type source struct{Adapter string `json:"adapter"`;Kind string `json:"kind"`;Reference string `json:"reference"`;Revision string `json:"revision"`}
type task struct{ID string `json:"id"`;Title string `json:"title"`;Goal string `json:"goal"`; Acceptance []string `json:"acceptance"`; Source source `json:"source"`}
type view struct{Kind string `json:"kind"`;Status string `json:"status"`;PlanID string `json:"plan_id"`; TaskSource *task `json:"task_source"`}
func check(v view){if v.TaskSource==nil||v.TaskSource.Source.Adapter!="github"||v.TaskSource.Source.Reference!="acme/app#42"||v.TaskSource.Source.Revision==""||len(v.TaskSource.Acceptance)!=2{fmt.Printf("%+v\n",v);os.Exit(1)}}
func main(){
 b,_:=os.ReadFile(os.Args[1]);var env struct{Result view `json:"result"`};if err:=json.Unmarshal(b,&env);err!=nil{panic(err)};v:=env.Result;if v.PlanID==""{var direct view;if err:=json.Unmarshal(b,&direct);err!=nil{panic(err)};v=direct};check(v)
 if len(os.Args)>2 { raw,_:=os.ReadFile(os.Args[2]); var rpc struct{Result struct{IsError bool `json:"isError"`; StructuredContent view `json:"structuredContent"`} `json:"result"`}; if err:=json.Unmarshal(raw,&rpc);err!=nil{panic(err)}; if rpc.Result.IsError{panic("MCP task start failed")}; check(rpc.Result.StructuredContent) }
}
GO
(cd "$ROOT" && go run "$TMP/assert.go" "$TMP/start.json" "$TMP/mcp-response.jsonl")
echo 'structured task sources: PASS'
