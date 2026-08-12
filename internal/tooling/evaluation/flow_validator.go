package evaluation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"takt/internal/assistant"
	"takt/internal/validation"
)

type FlowValidationRequest struct {
	ProtocolVersion string                  `json:"protocol_version"`
	Type            string                  `json:"type"`
	CaseID          string                  `json:"case_id"`
	Repeat          int                     `json:"repeat"`
	Workspace       string                  `json:"workspace"`
	Baseline        string                  `json:"baseline_workspace"`
	ExpectedPath    string                  `json:"expected_path"`
	Run             FlowValidationRun       `json:"run"`
	ExternalState   *FlowValidationExternal `json:"external_state,omitempty"`
}

type FlowValidationRun struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ArtifactsDir string `json:"artifacts_dir"`
}

type FlowValidationExternal struct {
	SCMDir string `json:"scm_dir"`
}

type FlowValidationExecution struct {
	Status    string             `json:"status"`
	ErrorCode string             `json:"error_code,omitempty"`
	Error     string             `json:"error,omitempty"`
	Stdout    []byte             `json:"-"`
	Stderr    []byte             `json:"-"`
	Duration  time.Duration      `json:"-"`
	Result    *validation.Result `json:"result,omitempty"`
}

func RunFlowValidator(ctx context.Context, spec FlowValidatorSpec, req FlowValidationRequest, suiteDir string) (result FlowValidationExecution) {
	started := time.Now()
	defer func() { result.Duration = time.Since(started) }()
	if err := validateFlowValidationRequest(req); err != nil {
		return flowValidationError("validator_protocol", err)
	}
	before, err := hashPath(req.Baseline)
	if err != nil {
		return flowValidationError("validator_protocol", err)
	}
	input, err := json.Marshal(req)
	if err != nil {
		return flowValidationError("validator_protocol", err)
	}
	if len(spec.ResolvedCommand) == 0 || strings.TrimSpace(spec.ResolvedCommand[0]) == "" {
		return flowValidationError("validator_start", errors.New("validator command is required"))
	}
	timedCtx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()
	budget := assistant.NewOutputBudget(spec.MaxOutputBytes, nil)
	stdout, stderr := assistant.NewLimitedBuffer(budget), assistant.NewLimitedBuffer(budget)
	cmd := exec.CommandContext(timedCtx, spec.ResolvedCommand[0], spec.ResolvedCommand[1:]...)
	cmd.Dir = suiteDir
	cmd.Stdin = bytes.NewReader(append(input, '\n'))
	cmd.Stdout, cmd.Stderr = stdout, stderr
	runErr := cmd.Run()
	result.Stdout, result.Stderr = []byte(stdout.String()), []byte(stderr.String())
	if !stdout.Truncated() && !stderr.Truncated() {
		result.Result, _ = validation.Decode(result.Stdout)
	}
	after, hashErr := hashPath(req.Baseline)
	if hashErr != nil {
		return flowValidationErrorWithOutput("baseline_modified", hashErr, result)
	}
	if before != after {
		return flowValidationErrorWithOutput("baseline_modified", errors.New("validator modified baseline workspace"), result)
	}
	if ctx.Err() != nil {
		return flowValidationErrorWithOutput("validator_cancelled", ctx.Err(), result)
	}
	if timedCtx.Err() != nil {
		return flowValidationErrorWithOutput("validator_timeout", timedCtx.Err(), result)
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return flowValidationErrorWithOutput("validator_exit", runErr, result)
		}
		return flowValidationErrorWithOutput("validator_start", runErr, result)
	}
	if stdout.Truncated() || stderr.Truncated() {
		return flowValidationErrorWithOutput("validator_protocol", errors.New("validator output exceeded max_output_bytes"), result)
	}
	decoded, err := validation.Decode(result.Stdout)
	if err != nil {
		return flowValidationErrorWithOutput("validator_protocol", err, result)
	}
	result.Status, result.Result = "completed", decoded
	return result
}

func PreflightFlowValidator(ctx context.Context, spec FlowValidatorSpec, caseID string, baseline, expectedPath, suiteDir string) (FlowValidationExecution, string, error) {
	req := FlowValidationRequest{
		ProtocolVersion: FlowValidatorProtocol,
		Type:            "validation_request",
		CaseID:          caseID,
		Workspace:       baseline,
		Baseline:        baseline,
		ExpectedPath:    expectedPath,
		Run:             FlowValidationRun{ID: "preflight", Status: "not_started"},
	}
	execution := RunFlowValidator(ctx, spec, req, suiteDir)
	if execution.Status != "completed" {
		return execution, "", fmt.Errorf("flow validator preflight: %s: %s", execution.ErrorCode, execution.Error)
	}
	metadata, err := json.Marshal(execution.Result.Metadata)
	if err != nil {
		return execution, "", err
	}
	hash := sha256.Sum256(metadata)
	return execution, hex.EncodeToString(hash[:]), nil
}

func validateFlowValidationRequest(req FlowValidationRequest) error {
	if req.ProtocolVersion != FlowValidatorProtocol || req.Type != "validation_request" {
		return fmt.Errorf("unsupported validation request")
	}
	if req.Repeat < 0 || strings.TrimSpace(req.Run.ID) == "" || strings.TrimSpace(req.Run.Status) == "" {
		return fmt.Errorf("repeat and run id/status are required")
	}
	for _, field := range []struct{ name, path string }{{"workspace", req.Workspace}, {"baseline_workspace", req.Baseline}, {"expected_path", req.ExpectedPath}} {
		if !filepath.IsAbs(field.path) {
			return fmt.Errorf("%s must be an absolute path", field.name)
		}
		if _, err := os.Stat(field.path); err != nil {
			return fmt.Errorf("%s: %w", field.name, err)
		}
	}
	if req.Run.ArtifactsDir == "" {
		if req.Run.ID != "preflight" {
			return errors.New("artifacts_dir is required outside preflight")
		}
		return nil
	}
	if !filepath.IsAbs(req.Run.ArtifactsDir) {
		return errors.New("artifacts_dir must be an absolute path")
	}
	return nil
}

func flowValidationError(code string, err error) FlowValidationExecution {
	return FlowValidationExecution{Status: "error", ErrorCode: code, Error: err.Error()}
}

func flowValidationErrorWithOutput(code string, err error, execution FlowValidationExecution) FlowValidationExecution {
	execution.Status, execution.ErrorCode, execution.Error = "error", code, err.Error()
	return execution
}
