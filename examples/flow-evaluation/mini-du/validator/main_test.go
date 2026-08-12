package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeRequestStrict(t *testing.T) {
	root := t.TempDir()
	req := testRequest(root)
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeRequest(data); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeRequest(append(data[:len(data)-1], []byte(`,"extra":true}`)...)); err == nil {
		t.Fatal("unknown request field accepted")
	}
	if _, err := decodeRequest([]byte(`{"protocol_version":"wrong"}`)); err == nil {
		t.Fatal("wrong protocol accepted")
	}
}

func TestValidatorPreflightReportsOracleMetadata(t *testing.T) {
	root := t.TempDir()
	result, err := validate(testRequest(root))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.Metadata["oracle_path"] == "" || result.Metadata["oracle_sha256"] == "" || result.Metadata["oracle_signature"] == "" {
		t.Fatalf("result=%+v", result)
	}
}

func TestValidatorRejectsInvalidExpectation(t *testing.T) {
	root := t.TempDir()
	req := testRequest(root)
	if err := os.WriteFile(req.ExpectedPath, []byte("allowed_paths: []\nscenarios: [unknown]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := validate(req); err == nil {
		t.Fatal("invalid expectation accepted")
	}
}

func TestRunWritesOneInvalidEnvelopeForProductFailure(t *testing.T) {
	root := t.TempDir()
	req := testRequest(root)
	req.Run.Status = "completed"
	if err := os.WriteFile(filepath.Join(root, "candidate", "go.mod"), []byte("module example.test/candidate\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(bytes.NewReader(data), &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var result validationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Valid {
		t.Fatalf("stdout=%s err=%v", stdout.String(), err)
	}
}

func TestHasPushChecksCurrentBranchInsteadOfBareRemoteHEAD(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	remote := filepath.Join(root, "origin.git")
	gitTest(t, root, "init", "--bare", remote)
	gitTest(t, root, "init", "-b", "feature", workspace)
	gitTest(t, workspace, "config", "user.name", "Takt Test")
	gitTest(t, workspace, "config", "user.email", "takt@example.test")
	if err := os.WriteFile(filepath.Join(workspace, "value"), []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, workspace, "add", "value")
	gitTest(t, workspace, "commit", "-m", "one")
	gitTest(t, workspace, "remote", "add", "origin", remote)
	gitTest(t, workspace, "push", "-u", "origin", "feature")
	if cmd := exec.Command("git", "--git-dir="+remote, "rev-parse", "HEAD"); cmd.Run() == nil {
		t.Fatal("fixture unexpectedly has a valid bare HEAD")
	}
	if !hasPush(workspace) {
		t.Fatal("pushed current branch was not detected")
	}
	if err := os.WriteFile(filepath.Join(workspace, "value"), []byte("two"), 0644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, workspace, "commit", "-am", "two")
	if hasPush(workspace) {
		t.Fatal("unpushed current branch was accepted")
	}
}

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func testRequest(root string) validatorRequest {
	for _, dir := range []string{"candidate", "baseline", "artifacts"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			panic(err)
		}
	}
	expected := filepath.Join(root, "expected.yaml")
	if err := os.WriteFile(expected, []byte("allowed_paths: [cmd/mini-du/**]\nscenarios: [empty]\n"), 0644); err != nil {
		panic(err)
	}
	return validatorRequest{ProtocolVersion: validatorProtocol, Type: "validation_request", CaseID: "case", Repeat: 1, Workspace: filepath.Join(root, "candidate"), Baseline: filepath.Join(root, "baseline"), ExpectedPath: expected, Run: validatorRun{ID: "preflight", Status: "not_started", ArtifactsDir: filepath.Join(root, "artifacts")}}
}
