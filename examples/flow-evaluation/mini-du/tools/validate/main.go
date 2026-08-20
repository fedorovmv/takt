package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type request struct {
	CaseID       string `json:"case_id"`
	Repeat       int    `json:"repeat"`
	Workspace    string `json:"workspace"`
	Baseline     string `json:"baseline_workspace"`
	ExpectedPath string `json:"expected_path"`
	RunID        string `json:"run_id"`
	RunStatus    string `json:"run_status"`
}

type validatorRequest struct {
	ProtocolVersion string                  `json:"protocol_version"`
	Type            string                  `json:"type"`
	CaseID          string                  `json:"case_id"`
	Repeat          int                     `json:"repeat"`
	Workspace       string                  `json:"workspace"`
	Baseline        string                  `json:"baseline_workspace"`
	ExpectedPath    string                  `json:"expected_path"`
	Run             validatorRun            `json:"run"`
	ExternalState   *validatorExternalState `json:"external_state,omitempty"`
}

type validatorRun struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ArtifactsDir string `json:"artifacts_dir"`
}

type validatorExternalState struct {
	SCMDir string `json:"scm_dir"`
}

func main() {
	if err := run(os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(input io.Reader, output, diagnostic io.Writer) error {
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	var value request
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode validation input: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("validation input must contain one JSON object")
	}
	if value.CaseID == "" || value.Repeat <= 0 || value.RunID == "" || value.RunStatus == "" {
		return fmt.Errorf("validation input requires case, repeat, and run")
	}
	for name, path := range map[string]string{"workspace": value.Workspace, "baseline_workspace": value.Baseline, "expected_path": value.ExpectedPath} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s must be absolute", name)
		}
	}
	control := strings.TrimSpace(os.Getenv("TAKT_WORKSPACE"))
	if control == "" {
		return fmt.Errorf("TAKT_WORKSPACE is required")
	}
	request := validatorRequest{
		ProtocolVersion: "takt-evaluation-validator/v1alpha1", Type: "validation_request",
		CaseID: value.CaseID, Repeat: value.Repeat, Workspace: value.Workspace, Baseline: value.Baseline, ExpectedPath: value.ExpectedPath,
		Run: validatorRun{ID: value.RunID, Status: value.RunStatus, ArtifactsDir: filepath.Join(control, ".takt", "runs", value.RunID, "artifacts")},
	}
	if scm := filepath.Join(value.Workspace, ".takt", "evals", "scm"); directory(scm) {
		request.ExternalState = &validatorExternalState{SCMDir: scm}
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("resolve validator source")
	}
	validator := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "validator"))
	cmd := exec.Command("go", "run", validator)
	cmd.Dir = filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", ".."))
	cmd.Stdin = bytes.NewReader(append(raw, '\n'))
	cmd.Stdout, cmd.Stderr = output, diagnostic
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run mini-du validator: %w", err)
	}
	return nil
}

func directory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
