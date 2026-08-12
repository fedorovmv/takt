package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const validFlowEnvelope = `{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}`
const invalidFlowEnvelope = `{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":false}`

func TestRunFlowValidatorTreatsInvalidAsMeasuredResult(t *testing.T) {
	got := runFlowValidatorFixture(t, invalidFlowEnvelope, "")
	if got.Status != "completed" || got.Result == nil || got.Result.Valid {
		t.Fatalf("execution=%+v", got)
	}
}

func TestRunFlowValidatorOutcomeMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, mode, wantCode string
		wantResult           bool
	}{
		{"valid", validFlowEnvelope, "", true},
		{"malformed", "{", "validator_protocol", false},
		{"empty", "", "validator_protocol", false},
		{"multiple", validFlowEnvelope + "\n" + validFlowEnvelope, "validator_protocol", false},
		{"exit", validFlowEnvelope + "|exit=7", "validator_exit", true},
		{"stdout overflow", "stdout=" + strings.Repeat("x", 128), "validator_protocol", false},
		{"stderr overflow", validFlowEnvelope + "|stderr=" + strings.Repeat("x", 128), "validator_protocol", false},
		{"baseline modified", validFlowEnvelope + "|mutate", "baseline_modified", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runFlowValidatorFixture(t, tc.mode, "")
			if tc.wantCode == "" {
				if got.Status != "completed" || got.Result == nil || got.Result.Valid != tc.wantResult {
					t.Fatalf("execution=%+v", got)
				}
				return
			}
			if got.Status != "error" || got.ErrorCode != tc.wantCode || (tc.wantResult && got.Result == nil) {
				t.Fatalf("execution=%+v", got)
			}
		})
	}
}

func TestRunFlowValidatorTimeoutAndCancellation(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		got := runFlowValidatorFixture(t, "sleep", "")
		if got.Status != "error" || got.ErrorCode != "validator_timeout" {
			t.Fatalf("execution=%+v", got)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		got := runFlowValidatorFixture(t, "sleep", "", ctx)
		if got.Status != "error" || got.ErrorCode != "validator_cancelled" {
			t.Fatalf("execution=%+v", got)
		}
	})
}

func TestRunFlowValidatorCancellationBeatsBaselineModified(t *testing.T) {
	d := t.TempDir()
	for _, name := range []string{"workspace", "baseline", "expected"} {
		if err := os.Mkdir(filepath.Join(d, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	signal := filepath.Join(d, "mutated")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", "mutate-sleep")
	t.Setenv("TAKT_FLOW_VALIDATOR_SIGNAL", signal)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan FlowValidationExecution, 1)
	spec := flowValidatorSpec(t, 256)
	req := flowValidatorRequest(d)
	go func() { result <- RunFlowValidator(ctx, spec, req, d) }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(signal); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("validator did not mutate baseline")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	got := <-result
	if got.Status != "error" || got.ErrorCode != "validator_cancelled" {
		t.Fatalf("execution=%+v", got)
	}
}

func TestRunFlowValidatorTimeoutBeatsBaselineModified(t *testing.T) {
	got := runFlowValidatorFixture(t, "mutate-sleep", "")
	if got.Status != "error" || got.ErrorCode != "validator_timeout" {
		t.Fatalf("execution=%+v", got)
	}
}

func TestRunFlowValidatorSendsExactRequest(t *testing.T) {
	d := t.TempDir()
	for _, name := range []string{"workspace", "baseline", "expected"} {
		if err := os.Mkdir(filepath.Join(d, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	requestPath := filepath.Join(d, "request.json")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	t.Setenv("TAKT_FLOW_VALIDATOR_REQUEST", requestPath)
	req := flowValidatorRequest(d)
	req.CaseID, req.Repeat = "case-a", 2
	req.ExternalState = &FlowValidationExternal{SCMDir: filepath.Join(d, "scm")}
	got := RunFlowValidator(context.Background(), flowValidatorSpec(t, 1024), req, d)
	if got.Status != "completed" {
		t.Fatalf("execution=%+v", got)
	}
	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var actual FlowValidationRequest
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, req) {
		t.Fatalf("request=%+v want=%+v", actual, req)
	}
}

func TestPreflightFlowValidatorUsesBaselineRequestAndMetadataHash(t *testing.T) {
	d := t.TempDir()
	for _, name := range []string{"baseline", "expected"} {
		if err := os.Mkdir(filepath.Join(d, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	requestPath := filepath.Join(d, "request.json")
	metadata := `{"validator":"mini-du","version":1}`
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", invalidFlowEnvelope+"|metadata="+metadata)
	t.Setenv("TAKT_FLOW_VALIDATOR_REQUEST", requestPath)
	got, fingerprint, err := PreflightFlowValidator(context.Background(), flowValidatorSpec(t, 1024), "case-a", filepath.Join(d, "baseline"), filepath.Join(d, "expected"), d)
	if err != nil || got.Status != "completed" || got.Result == nil || got.Result.Valid {
		t.Fatalf("execution=%+v fingerprint=%q err=%v", got, fingerprint, err)
	}
	if fingerprint != "7905d6c33f2bffb7991fa3c31dc4c1aaf50c76ff4dbdf03b9165078061071085" {
		t.Fatalf("fingerprint=%q", fingerprint)
	}
	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var req FlowValidationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatal(err)
	}
	if req.Repeat != 0 || req.Workspace != req.Baseline || req.Run.ID != "preflight" || req.Run.Status != "not_started" || req.Run.ArtifactsDir != "" {
		t.Fatalf("request=%+v", req)
	}
}

func runFlowValidatorFixture(t *testing.T, mode, requestPath string, contexts ...context.Context) FlowValidationExecution {
	t.Helper()
	d := t.TempDir()
	for _, name := range []string{"workspace", "baseline", "expected"} {
		if err := os.Mkdir(filepath.Join(d, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", mode)
	if requestPath != "" {
		t.Setenv("TAKT_FLOW_VALIDATOR_REQUEST", requestPath)
	}
	ctx := context.Background()
	if len(contexts) != 0 {
		ctx = contexts[0]
	}
	limit := 256
	if strings.HasPrefix(mode, "stdout=") {
		limit = 64
	}
	if strings.Contains(mode, "|stderr=") {
		limit = 128
	}
	spec := flowValidatorSpec(t, limit)
	if strings.Contains(mode, "sleep") {
		spec.Timeout = 20 * time.Millisecond
	}
	return RunFlowValidator(ctx, spec, flowValidatorRequest(d), d)
}

func flowValidatorSpec(t *testing.T, limit int) FlowValidatorSpec {
	t.Helper()
	return FlowValidatorSpec{
		ResolvedCommand: []string{os.Args[0], "-test.run=TestFlowValidatorHelperProcess", "--"},
		Timeout:         time.Second,
		MaxOutputBytes:  limit,
	}
}

func flowValidatorRequest(root string) FlowValidationRequest {
	return FlowValidationRequest{
		ProtocolVersion: FlowValidatorProtocol,
		Type:            "validation_request",
		CaseID:          "case",
		Repeat:          1,
		Workspace:       filepath.Join(root, "workspace"),
		Baseline:        filepath.Join(root, "baseline"),
		ExpectedPath:    filepath.Join(root, "expected"),
		Run:             FlowValidationRun{ID: "run-1", Status: "completed", ArtifactsDir: filepath.Join(root, "artifacts")},
	}
}

func TestFlowValidatorHelperProcess(t *testing.T) {
	if os.Getenv("TAKT_FLOW_VALIDATOR_MODE") == "" {
		return
	}
	var request FlowValidationRequest
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(2)
	}
	if path := os.Getenv("TAKT_FLOW_VALIDATOR_REQUEST"); path != "" {
		data, _ := json.Marshal(request)
		_ = os.WriteFile(path, data, 0600)
	}
	mode := os.Getenv("TAKT_FLOW_VALIDATOR_MODE")
	if mode == "sleep" {
		time.Sleep(time.Second)
		os.Exit(0)
	}
	if strings.Contains(mode, "|mutate") {
		_ = os.WriteFile(filepath.Join(request.Baseline, "changed"), []byte("changed"), 0600)
		if signal := os.Getenv("TAKT_FLOW_VALIDATOR_SIGNAL"); signal != "" {
			_ = os.WriteFile(signal, []byte("mutated"), 0600)
		}
		mode = strings.TrimSuffix(mode, "|mutate")
	}
	if mode == "mutate-sleep" {
		_ = os.WriteFile(filepath.Join(request.Baseline, "changed"), []byte("changed"), 0600)
		if signal := os.Getenv("TAKT_FLOW_VALIDATOR_SIGNAL"); signal != "" {
			_ = os.WriteFile(signal, []byte("mutated"), 0600)
		}
		time.Sleep(time.Second)
		os.Exit(0)
	}
	if i := strings.Index(mode, "|stderr="); i >= 0 {
		fmt.Fprint(os.Stderr, mode[i+len("|stderr="):])
		mode = mode[:i]
	}
	if i := strings.Index(mode, "|metadata="); i >= 0 {
		mode = strings.TrimSuffix(mode[:i], "")
		fmt.Printf(`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":false,"metadata":%s}`, os.Getenv("TAKT_FLOW_VALIDATOR_MODE")[i+len("|metadata="):])
		os.Exit(0)
	}
	if i := strings.Index(mode, "|exit="); i >= 0 {
		fmt.Fprint(os.Stdout, mode[:i])
		fmt.Fprint(os.Stderr, "exit")
		os.Exit(7)
	}
	if strings.HasPrefix(mode, "stdout=") {
		fmt.Fprint(os.Stdout, strings.TrimPrefix(mode, "stdout="))
		os.Exit(0)
	}
	fmt.Fprint(os.Stdout, mode)
	os.Exit(0)
}
