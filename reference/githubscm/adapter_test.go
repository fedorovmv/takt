package githubscm

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	sdk "takt/sdk/domainadapter"
)

func TestDescribeUsesPublicDomainSDK(t *testing.T) {
	resp, code, diag := serve(t, Adapter{}, envelope{APIVersion: sdk.ProtocolV1Alpha1, Kind: "DescribeRequest"})
	if code != 0 {
		t.Fatalf("code=%d diag=%s", code, diag)
	}
	if resp.Declaration == nil {
		t.Fatal("missing declaration")
	}
	if err := sdk.ValidateDeclaration(*resp.Declaration); err != nil {
		t.Fatal(err)
	}
	if resp.Declaration.Domain != sdk.DomainSCM {
		t.Fatalf("domain=%s", resp.Declaration.Domain)
	}
	if len(resp.Declaration.Capabilities) != len(sdk.CoreOperations(sdk.DomainSCM)) {
		t.Fatalf("capabilities=%v", resp.Declaration.Capabilities)
	}
}

func TestInvokeUsesRepositoryWorkspaceAndGitRemote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "services", "api")
	mustRun(t, root, "git", "init", repo)
	mustRun(t, root, "git", "-C", repo, "remote", "add", "origin", "git@github.com:acme/service.git")
	log := filepath.Join(root, "gh.log")
	fake := fakeGH(t, root, `
printf 'cwd=%s repo=%s args=%s\n' "$PWD" "${GH_REPO:-}" "$*" >> "$GH_FAKE_LOG"
case "$1 $2" in
  "api repos/{owner}/{repo}") printf '{"full_name":"acme/service"}\n' ;;
  *) printf '{}\n' ;;
esac
`)
	t.Setenv("GH_FAKE_LOG", log)
	input, _ := json.Marshal(changeInput{Repository: "services/api", RepositoryWorkspace: repo})
	req := sdk.InvokeRequest{RunID: "r", NodeID: "n", Attempt: 1, Workspace: root, Domain: sdk.DomainSCM, Operation: sdk.SCMRepositoryGet, Input: input}
	resp, code, diag := serve(t, Adapter{GHBinary: fake}, envelope{APIVersion: sdk.ProtocolV1Alpha1, Kind: "InvokeRequest", Request: &req})
	if code != 0 || resp.Result == nil || resp.Result.Status != "completed" {
		t.Fatalf("code=%d diag=%s result=%+v", code, diag, resp.Result)
	}
	raw, _ := os.ReadFile(log)
	text := string(raw)
	expectedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "cwd="+expectedRepo) || !strings.Contains(text, "repo=acme/service") {
		t.Fatalf("wrong gh context: %s", text)
	}
}

func TestChangeCreateUnknownReconcilesByIdempotencyMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mustRun(t, root, "git", "init", repo)
	mustRun(t, root, "git", "-C", repo, "remote", "add", "origin", "https://github.com/acme/service.git")
	state := filepath.Join(root, "body")
	fake := fakeGH(t, root, `
case "$1 $2" in
  "pr create")
    prev=""
    for arg in "$@"; do
      if [ "$prev" = "--body" ]; then printf '%s' "$arg" > "$GH_FAKE_STATE"; fi
      prev="$arg"
    done
    echo 'network lost after create' >&2
    exit 1
    ;;
  "pr list")
    body=$(cat "$GH_FAKE_STATE")
    printf '[{"number":17,"url":"https://github.com/acme/service/pull/17","body":"%s"}]\n' "$body"
    ;;
  *) printf '{}\n' ;;
esac
`)
	t.Setenv("GH_FAKE_STATE", state)
	input, _ := json.Marshal(changeInput{RepositoryWorkspace: repo, Title: "Takt change", Head: "takt/run-1"})
	key := "run-secret-node-secret"
	req := sdk.InvokeRequest{RunID: "r", NodeID: "n", Attempt: 1, Workspace: root, Domain: sdk.DomainSCM, Operation: sdk.SCMChangeCreate, Input: input, IdempotencyKey: key, SideEffectMode: "reconcile"}
	resp, code, diag := serve(t, Adapter{GHBinary: fake}, envelope{APIVersion: sdk.ProtocolV1Alpha1, Kind: "InvokeRequest", Request: &req})
	if code != 0 || resp.Result == nil || resp.Result.Status != "unknown" {
		t.Fatalf("invoke code=%d diag=%s result=%+v", code, diag, resp.Result)
	}
	body, _ := os.ReadFile(state)
	if bytes.Contains(body, []byte(key)) || !bytes.Contains(body, []byte(idempotencyMarker(key))) {
		t.Fatalf("idempotency marker leaks raw key or missing: %s", body)
	}
	recReq := sdk.ReconcileRequest{RunID: "r", NodeID: "n", Workspace: root, Domain: sdk.DomainSCM, Operation: sdk.SCMChangeCreate, Input: input, IdempotencyKey: key}
	rec, code, diag := serve(t, Adapter{GHBinary: fake}, envelope{APIVersion: sdk.ProtocolV1Alpha1, Kind: "ReconcileRequest", Reconcile: &recReq})
	if code != 0 || rec.Reconcile == nil || rec.Reconcile.Outcome != "applied" || rec.Reconcile.Receipt != "https://github.com/acme/service/pull/17" {
		t.Fatalf("reconcile code=%d diag=%s result=%+v", code, diag, rec.Reconcile)
	}
}

func TestRelativePathWinsOverOwnerRepoShapeWhenItExists(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "owner", "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, root, "git", "init", repo)
	mustRun(t, root, "git", "-C", repo, "remote", "add", "origin", "git@github.example:team/project.git")
	got, dir, err := resolveRepository(context.Background(), root, changeInput{Repository: "owner/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "github.example/team/project" || dir != repo {
		t.Fatalf("repo=%q dir=%q", got, dir)
	}
}

func TestCommentAndReviewReconcileByHashedMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	for _, tc := range []struct {
		name      string
		operation string
		input     changeInput
		postMatch string
		listMatch string
	}{
		{name: "comment", operation: sdk.SCMChangeComment, input: changeInput{Number: 17, Body: "hello"}, postMatch: "/issues/17/comments", listMatch: "/issues/17/comments --paginate"},
		{name: "review", operation: sdk.SCMChangeReview, input: changeInput{Number: 17, Body: "looks good", Review: "approve"}, postMatch: "/pulls/17/reviews", listMatch: "/pulls/17/reviews --paginate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			repo := filepath.Join(root, "repo")
			mustRun(t, root, "git", "init", repo)
			mustRun(t, root, "git", "-C", repo, "remote", "add", "origin", "https://github.com/acme/service.git")
			state := filepath.Join(root, "body")
			log := filepath.Join(root, "gh.log")
			fake := fakeGH(t, root, `
printf '%s\n' "$*" >> "$GH_FAKE_LOG"
if [ "$1" = "api" ] && ! printf '%s' "$*" | grep -q -- '--paginate'; then
  prev=""
  for arg in "$@"; do
    case "$arg" in body=*) printf '%s' "${arg#body=}" > "$GH_FAKE_STATE" ;; esac
    prev="$arg"
  done
  echo 'transport lost after mutation' >&2
  exit 1
fi
if [ "$1" = "api" ] && printf '%s' "$*" | grep -q -- '--paginate'; then
  body=$(tail -n 1 "$GH_FAKE_STATE")
  printf '[{"id":99,"html_url":"https://github.com/acme/service/item/99","body":"%s"}]\n' "$body"
  exit 0
fi
printf '{}\n'
`)
			t.Setenv("GH_FAKE_STATE", state)
			t.Setenv("GH_FAKE_LOG", log)
			rawInput, _ := json.Marshal(tc.input)
			key := "raw-secret-idempotency"
			req := sdk.InvokeRequest{RunID: "r", NodeID: "n", Attempt: 1, Workspace: repo, Domain: sdk.DomainSCM, Operation: tc.operation, Input: rawInput, IdempotencyKey: key, SideEffectMode: "reconcile"}
			result := (Adapter{GHBinary: fake}).invoke(context.Background(), req)
			if result.Status != "unknown" {
				t.Fatalf("invoke=%+v", result)
			}
			body, _ := os.ReadFile(state)
			if bytes.Contains(body, []byte(key)) || !bytes.Contains(body, []byte(idempotencyMarker(key))) {
				t.Fatalf("marker=%s", body)
			}
			rec := (Adapter{GHBinary: fake}).reconcile(context.Background(), sdk.ReconcileRequest{RunID: "r", NodeID: "n", Workspace: repo, Domain: sdk.DomainSCM, Operation: tc.operation, Input: rawInput, IdempotencyKey: key})
			if rec.Outcome != "applied" || rec.Receipt == "" {
				t.Fatalf("reconcile=%+v", rec)
			}
			logRaw, _ := os.ReadFile(log)
			if !strings.Contains(string(logRaw), tc.postMatch) || !strings.Contains(string(logRaw), tc.listMatch) {
				t.Fatalf("gh log=%s", logRaw)
			}
		})
	}
}

func TestChecksGetAcceptsPendingExitCodeEight(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mustRun(t, root, "git", "init", repo)
	mustRun(t, root, "git", "-C", repo, "remote", "add", "origin", "https://github.com/acme/service.git")
	fake := fakeGH(t, root, `
if [ "$1" = "pr" ] && [ "$2" = "checks" ]; then
  printf '[{"name":"test","state":"IN_PROGRESS","bucket":"pending"}]\n'
  exit 8
fi
exit 2
`)
	input, _ := json.Marshal(changeInput{Number: 17})
	result := (Adapter{GHBinary: fake}).invoke(context.Background(), sdk.InvokeRequest{RunID: "r", NodeID: "checks", Attempt: 1, Workspace: repo, Domain: sdk.DomainSCM, Operation: sdk.SCMChecksGet, Input: input})
	if result.Status != "completed" || !strings.Contains(string(result.Output), `"bucket":"pending"`) {
		t.Fatalf("result=%+v", result)
	}
}

func TestReconcileRejectsUnknownInputFields(t *testing.T) {
	req := sdk.ReconcileRequest{RunID: "r", NodeID: "n", Workspace: t.TempDir(), Domain: sdk.DomainSCM, Operation: sdk.SCMChangeCreate, Input: json.RawMessage(`{"head":"x","unknown":true}`), IdempotencyKey: "k"}
	resp, code, diag := serve(t, Adapter{}, envelope{APIVersion: sdk.ProtocolV1Alpha1, Kind: "ReconcileRequest", Reconcile: &req})
	if code != 0 || resp.Reconcile == nil || resp.Reconcile.Outcome != "unknown" || resp.Reconcile.ErrorCode != "INVALID_INPUT" {
		t.Fatalf("code=%d diag=%s result=%+v", code, diag, resp.Reconcile)
	}
}

func TestReferencePackageDoesNotImportRuntimeInternals(t *testing.T) {
	raw, err := os.ReadFile("adapter.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"takt/internal/`)) {
		t.Fatal("reference adapter imports internal package")
	}
}

func serve(t *testing.T, a Adapter, env envelope) (response, int, string) {
	t.Helper()
	var in, out, diag bytes.Buffer
	if err := json.NewEncoder(&in).Encode(env); err != nil {
		t.Fatal(err)
	}
	code := a.Serve(context.Background(), &in, &out, &diag)
	var resp response
	if out.Len() > 0 {
		if err := json.NewDecoder(&out).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v: %s", err, out.String())
		}
	}
	return resp, code, diag.String()
}
func fakeGH(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, raw)
	}
}

func TestUnsafeRefsFailBeforeGhInvocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	marker := filepath.Join(root, "called")
	fake := fakeGH(t, root, "touch \"$GH_CALLED\"\nprintf '{}\\n'\n")
	t.Setenv("GH_CALLED", marker)
	for _, head := range []string{"-evil", "feature/../main", "feature//main", "bad\\ref"} {
		raw, _ := json.Marshal(changeInput{Repository: "acme/service", Title: "x", Head: head})
		result := (Adapter{GHBinary: fake}).invoke(context.Background(), sdk.InvokeRequest{RunID: "r", NodeID: "n", Attempt: 1, Workspace: root, Domain: sdk.DomainSCM, Operation: sdk.SCMChangeCreate, Input: raw})
		if result.Status != "failed" || result.ErrorCode != "INVALID_INPUT" {
			t.Fatalf("head=%q result=%+v", head, result)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("gh invoked for unsafe ref: %v", err)
	}
}

func TestGhTimeoutAndDiagnosticsDoNotExposeBody(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	secretBody := "BODY-SHOULD-NOT-LEAK-123"
	fake := fakeGH(t, root, `
if [ "$1 $2" = "pr create" ]; then
  printf '%s\n' "$*" >&2
  while :; do :; done
fi
printf '{}\n'
`)
	raw, _ := json.Marshal(changeInput{Repository: "acme/service", Title: "x", Head: "feature/test", Body: secretBody})
	started := time.Now()
	result := (Adapter{GHBinary: fake, Timeout: 25 * time.Millisecond}).invoke(context.Background(), sdk.InvokeRequest{RunID: "r", NodeID: "n", Attempt: 1, Workspace: root, Domain: sdk.DomainSCM, Operation: sdk.SCMChangeCreate, Input: raw})
	if result.Status != "failed" || result.ErrorCode != "GH_ERROR" {
		t.Fatalf("result=%+v", result)
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatalf("gh timeout took %s", time.Since(started))
	}
	if strings.Contains(result.Error, secretBody) {
		t.Fatalf("body leaked through diagnostics: %s", result.Error)
	}
}
