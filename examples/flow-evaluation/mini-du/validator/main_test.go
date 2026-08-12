package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
