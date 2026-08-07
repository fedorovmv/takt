// takt-fake-domain-adapter is a deterministic Adapter Platform fixture. It can
// speak both takt-domain-adapter/v1alpha1 (one request per process) and MCP
// stdio (--mcp) so the release E2E exercises both transports without GitHub.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	domainsdk "takt/sdk/domainadapter"
)

const version = domainsdk.ProtocolV1Alpha1

type envelope struct {
	APIVersion string                      `json:"apiVersion"`
	Kind       string                      `json:"kind"`
	Request    *domainsdk.InvokeRequest    `json:"request,omitempty"`
	Reconcile  *domainsdk.ReconcileRequest `json:"reconcile,omitempty"`
}

func main() {
	domain := "tracker"
	mcp := false
	for _, a := range os.Args[1:] {
		if a == "--mcp" {
			mcp = true
		} else if strings.HasPrefix(a, "--domain=") {
			domain = strings.TrimPrefix(a, "--domain=")
		}
	}
	if mcp {
		serveMCP(domain)
		return
	}
	var req envelope
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fatal(err)
	}
	switch req.Kind {
	case "DescribeRequest":
		write(map[string]any{"apiVersion": version, "kind": "DescribeResponse", "declaration": declaration(domain)})
	case "InvokeRequest":
		if req.Request == nil {
			fatal(fmt.Errorf("missing request"))
		}
		write(map[string]any{"apiVersion": version, "kind": "InvokeResponse", "result": invokeOperation(*req.Request)})
	case "ReconcileRequest":
		if req.Reconcile == nil {
			fatal(fmt.Errorf("missing reconcile"))
		}
		write(map[string]any{"apiVersion": version, "kind": "ReconcileResponse", "reconcile": reconcileOperation(*req.Reconcile)})
	default:
		fatal(fmt.Errorf("unsupported kind %s", req.Kind))
	}
}

func declaration(domain string) map[string]any {
	caps := capabilities(domain)
	rec := []string{}
	for _, op := range caps {
		if mutating(op) {
			rec = append(rec, op)
		}
	}
	return map[string]any{"apiVersion": version, "kind": "AdapterCapabilities", "domain": domain, "capabilities": caps, "reconcile": rec}
}
func capabilities(domain string) []string {
	values := domainsdk.CoreOperations(domain)
	sort.Strings(values)
	return values
}
func mutating(op string) bool {
	return strings.Contains(op, ".create") || strings.Contains(op, ".comment") || strings.Contains(op, ".transition") || op == "change.review" || op == "run.start" || op == "run.cancel"
}
func invokeOperation(req domainsdk.InvokeRequest) map[string]any {
	var input map[string]any
	_ = json.Unmarshal(req.Input, &input)
	receipt := ""
	if req.IdempotencyKey != "" && mutating(req.Operation) {
		receipt = "fake:" + req.IdempotencyKey
		remember(req.IdempotencyKey, receipt)
	}
	output := map[string]any{"domain": req.Domain, "operation": req.Operation, "ok": true, "idempotency_key": req.IdempotencyKey}
	if id, ok := input["id"]; ok {
		output["id"] = id
	}
	if title, ok := input["title"]; ok {
		output["title"] = title
	}
	raw, _ := json.Marshal(output)
	if b, _ := input["simulate_unknown"].(bool); b {
		return map[string]any{"status": "unknown", "receipt": receipt}
	}
	return map[string]any{"status": "completed", "output": json.RawMessage(raw), "receipt": receipt}
}
func reconcileOperation(req domainsdk.ReconcileRequest) map[string]any {
	if receipt := lookup(req.IdempotencyKey); receipt != "" {
		raw, _ := json.Marshal(map[string]any{"reconciled": true, "operation": req.Operation, "idempotency_key": req.IdempotencyKey})
		return map[string]any{"outcome": "applied", "receipt": receipt, "output": json.RawMessage(raw)}
	}
	return map[string]any{"outcome": "not_applied"}
}
func statePath() string {
	if p := os.Getenv("TAKT_FAKE_ADAPTER_STATE"); p != "" {
		return p
	}
	return os.TempDir() + "/takt-fake-domain-adapter-state"
}
func remember(key, receipt string) {
	f, _ := os.OpenFile(statePath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if f != nil {
		fmt.Fprintf(f, "%s\t%s\n", key, receipt)
		_ = f.Close()
	}
}
func lookup(key string) string {
	raw, _ := os.ReadFile(statePath())
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[0] == key {
			return parts[1]
		}
	}
	return ""
}
func write(v any)     { _ = json.NewEncoder(os.Stdout).Encode(v) }
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(2) }

func serveMCP(domain string) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var msg map[string]any
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		method, _ := msg["method"].(string)
		id, hasID := msg["id"]
		if !hasID {
			continue
		}
		switch method {
		case "initialize":
			rpc(id, map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "takt-fake-domain-adapter", "version": "0.1.42-alpha"}})
		case "tools/list":
			var tools []map[string]any
			for _, op := range capabilities(domain) {
				tools = append(tools, map[string]any{"name": domain + "." + op, "description": "fake neutral operation", "inputSchema": map[string]any{"type": "object"}})
				if mutating(op) {
					tools = append(tools, map[string]any{"name": domain + "." + op + ".reconcile", "description": "fake reconcile", "inputSchema": map[string]any{"type": "object"}})
				}
			}
			sort.Slice(tools, func(i, j int) bool { return tools[i]["name"].(string) < tools[j]["name"].(string) })
			rpc(id, map[string]any{"tools": tools})
		case "tools/call":
			params, _ := msg["params"].(map[string]any)
			name, _ := params["name"].(string)
			args, _ := params["arguments"].(map[string]any)
			op := strings.TrimPrefix(name, domain+".")
			var value any
			if strings.HasSuffix(op, ".reconcile") {
				base := strings.TrimSuffix(op, ".reconcile")
				raw, _ := json.Marshal(args)
				value = reconcileOperation(domainsdk.ReconcileRequest{Domain: domain, Operation: base, Input: raw, IdempotencyKey: stringValue(args["idempotency_key"]), Receipt: stringValue(args["receipt"])})
			} else {
				raw, _ := json.Marshal(args)
				value = invokeOperation(domainsdk.InvokeRequest{Domain: domain, Operation: op, Input: raw, IdempotencyKey: stringValue(args["idempotency_key"]), SideEffectMode: "reconcile"})
			}
			rpc(id, map[string]any{"structuredContent": value, "content": []map[string]any{{"type": "text", "text": "ok"}}})
		default:
			rpcError(id, -32601, "method not found")
		}
	}
}
func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func rpc(id any, result any) { write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}) }
func rpcError(id any, code int, message string) {
	write(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}
