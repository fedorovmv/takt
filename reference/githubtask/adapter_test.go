package githubtask

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	sdk "takt/sdk/tasksource"
	"testing"
)

func TestReferenceResolve(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	d := t.TempDir()
	fake := filepath.Join(d, "gh")
	if err := os.WriteFile(fake, []byte(`#!/bin/sh
printf '%s\n' '{"number":42,"title":"Fix retry","body":"Details\n- [ ] retries pass\n- [x] docs updated","url":"https://github.com/acme/app/issues/42","labels":[{"name":"bug"},{"name":"backend"}],"state":"OPEN","updatedAt":"2026-08-08T10:00:00Z"}'
`), 0755); err != nil {
		t.Fatal(err)
	}
	task, err := (Adapter{GHBinary: fake}).Resolve(context.Background(), "acme/app#42")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "github:acme/app#42" || len(task.Acceptance) != 2 || task.Source.Revision == "" {
		t.Fatalf("%+v", task)
	}
	if err := sdk.ValidateTask(*task); err != nil {
		t.Fatal(err)
	}
}
func TestServeUsesPublicProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	d := t.TempDir()
	fake := filepath.Join(d, "gh")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\nprintf '%s\\n' '{\"number\":1,\"title\":\"One\",\"body\":\"\",\"url\":\"u\",\"labels\":[],\"state\":\"OPEN\",\"updatedAt\":\"x\"}'\n"), 0755)
	req := sdk.ResolveRequest{APIVersion: sdk.ProtocolV1Alpha1, Kind: "ResolveRequest", Reference: "a/b#1"}
	var in, out, diag bytes.Buffer
	_ = json.NewEncoder(&in).Encode(req)
	if code := (Adapter{GHBinary: fake}).Serve(context.Background(), &in, &out, &diag); code != 0 {
		t.Fatalf("%d %s", code, diag.String())
	}
	var resp sdk.ResolveResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if err := sdk.ValidateResolveResponse(resp); err != nil {
		t.Fatal(err)
	}
}
func TestReferencePackageNoInternalImports(t *testing.T) {
	raw, err := os.ReadFile("adapter.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "takt/internal/") {
		t.Fatal("internal import")
	}
}

func TestReferenceRejectsNonGitHubDotComURLInsteadOfDroppingHost(t *testing.T) {
	_, _, _, err := parseReference("https://github.example.com/acme/app/issues/42")
	if err == nil || !strings.Contains(err.Error(), "host must be github.com") {
		t.Fatalf("unexpected error: %v", err)
	}
}
