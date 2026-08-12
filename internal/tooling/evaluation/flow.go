package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"takt/internal/config"
	"takt/internal/redact"
	"takt/internal/store"
	"takt/internal/version"
)

type FlowCaseRunRequest struct {
	Workspace, Selector, ConfigPath, InputValue, ApprovalAnswer string
}

type FlowCaseRunResult struct {
	States           []*store.RunState
	Events           []store.Event
	Artifacts        []store.ArtifactRef
	ArtifactDirs     map[string]string
	ContextCancelled bool
	Cleanup          func(context.Context) (*store.RunState, error)
}

type FlowCaseRunner func(context.Context, FlowCaseRunRequest) (FlowCaseRunResult, error)

type FlowRunOptions struct {
	SuitePath, CaseID, OutputDir, InvocationWorkspace string
	Repeat                                            int
	KeepWorkspaces                                    bool
	Now                                               func() time.Time
	HostPATH                                          string
	CaseRunner                                        FlowCaseRunner
}

type FlowGateFailureError struct{ Report *SuiteReport }

func (e *FlowGateFailureError) Error() string { return "flow evaluation gates failed" }

func RunFlow(ctx context.Context, opts FlowRunOptions) (*SuiteReport, error) {
	if opts.CaseRunner == nil {
		return nil, errors.New("flow case runner is required")
	}
	if opts.Repeat <= 0 {
		opts.Repeat = 1
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	suite, err := LoadFlowSuite(opts.SuitePath)
	if err != nil {
		return nil, err
	}
	cases, err := DiscoverFlowCases(suite.SuitePath, suite, opts.CaseID)
	if err != nil {
		return nil, err
	}
	output, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return nil, err
	}
	output = filepath.Clean(output)
	if err := validateFlowOutput(output, suite, opts.InvocationWorkspace); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return nil, err
	}
	pathHash := sha256.Sum256([]byte(opts.HostPATH))
	validatorFingerprint, err := hashPath(suite.Validator.ResolvedPath)
	if err != nil {
		return nil, fmt.Errorf("fingerprint validator: %w", err)
	}
	report := &SuiteReport{
		ReportVersion: ReportVersion, TaktVersion: version.Value, StartedAt: opts.Now().UTC(),
		Workflow: suite.Workflow, Config: suite.Config, CasesDir: suite.Cases.Directory,
		OutputDir: flowReportPath(output, opts.InvocationWorkspace), Mode: "flow", Summary: newSummary(),
		Benchmark:   BenchmarkIdentity{ID: filepath.Base(suite.SuitePath), CaseCount: len(cases), ValidationProtocol: FlowValidatorProtocol, Validator: ValidatorIdentity{ID: suite.Validator.ID, Version: suite.Validator.Version, Path: suite.Validator.Path, Fingerprint: validatorFingerprint}},
		Environment: EnvironmentIdentity{GOOS: goruntime.GOOS, GOARCH: goruntime.GOARCH, GoVersion: goruntime.Version(), PATHSHA256: hex.EncodeToString(pathHash[:])},
	}
	var oracleMetadata, strategyFingerprint string
	var redactor *redact.Redactor
	for _, item := range cases {
		for repeat := 1; repeat <= opts.Repeat; repeat++ {
			prepared, prepErr := PrepareFlowRepeat(ctx, suite, item, repeat, output, opts.HostPATH)
			if prepErr != nil {
				return finishFlowPartial(report, output, opts.Now, nil, prepErr)
			}
			cfg, cfgErr := config.Load(prepared.ConfigPath)
			if cfgErr != nil {
				return finishFlowPartial(report, output, opts.Now, nil, cfgErr)
			}
			redactor = redact.NewFromConfig(cfg)
			preflight, metadata, preflightErr := PreflightFlowValidator(ctx, suite.Validator, item.ID, prepared.BaselineWorkspace, item.ExpectedPath, suite.SuiteDir)
			if preflightErr != nil {
				_ = preflight
				return finishFlowPartial(report, output, opts.Now, redactor, preflightErr)
			}
			if oracleMetadata == "" {
				oracleMetadata = metadata
				report.Environment.OracleMetadataSHA256 = metadata
			} else if oracleMetadata != metadata {
				return finishFlowPartial(report, output, opts.Now, redactor, errors.New("oracle_identity_drift"))
			}

			selector := suite.Workflow
			if suite.ResolvedWorkflow != "" {
				selector = suite.ResolvedWorkflow
			}
			approval := suite.Approvals.Default
			if item.Expectation != nil && item.Expectation.Takt.ApprovalAnswer != "" {
				approval = item.Expectation.Takt.ApprovalAnswer
			}
			result, callbackErr := opts.CaseRunner(ctx, FlowCaseRunRequest{Workspace: prepared.ControlWorkspace, Selector: selector, ConfigPath: prepared.ConfigPath, InputValue: prepared.InputValue, ApprovalAnswer: approval})
			if len(result.States) == 0 || result.States[0] == nil {
				if callbackErr == nil {
					callbackErr = errors.New("flow case runner returned no root snapshot")
				}
				return finishFlowPartial(report, output, opts.Now, redactor, callbackErr)
			}

			root := result.States[0]
			record := recordFromFlowSnapshots(item.ID, repeat, prepared.ControlWorkspace, result.States)
			applyRuntimeMetricsFromEvents(&record, root, result.Events, "")
			if callbackErr != nil && record.Error == "" {
				record.Error = callbackErr.Error()
			}
			request := flowAuthoritativeRequest(item, repeat, root, prepared, result.ArtifactDirs)
			validationResult := FlowValidationExecution{Status: "error"}
			paused := root.Status == store.RunPausing || root.Status == store.RunPaused
			switch {
			case result.ContextCancelled || ctx.Err() != nil:
				validationResult = flowValidationError("validator_cancelled", context.Canceled)
			case paused:
				validationResult = flowValidationError("run_paused", errors.New("run paused"))
			case root.ExecutionWorkspace == "":
				validationResult = flowValidationError("missing_workspace", errors.New("run execution workspace is missing"))
			default:
				if _, statErr := os.Stat(root.ExecutionWorkspace); statErr != nil {
					validationResult = flowValidationError("missing_workspace", statErr)
				} else {
					validationResult = RunFlowValidator(ctx, suite.Validator, request, suite.SuiteDir)
				}
			}
			record.Validation = &FlowValidationRecord{Status: validationResult.Status, ErrorCode: validationResult.ErrorCode, Error: validationResult.Error, Result: validationResult.Result, DurationMS: validationResult.Duration.Milliseconds()}
			if paused {
				record.Cleanup = &FlowCleanupRecord{Status: "skipped"}
			}
			if validationResult.Status == "completed" && validationResult.Result != nil {
				elapsed := root.UpdatedAt.Sub(root.CreatedAt).Milliseconds() + validationResult.Duration.Milliseconds()
				if elapsed < 0 {
					elapsed = 0
				}
				record.TimeToValidMS = &elapsed
				ClassifyFlowRecord(&record)
			}
			report.Runs = append(report.Runs, record)
			addSummary(&report.Summary, record)
			if err := writeFlowRepeatEvidence(output, item, repeat, result, request, validationResult, prepared, redactor); err != nil {
				return finishFlowPartial(report, output, opts.Now, redactor, err)
			}
			if err := writeFlowReport(output, report, opts.Now, redactor); err != nil {
				return report, err
			}
			candidateStrategy, strategyErr := flowStrategy(root, prepared.ProfileFingerprint)
			if strategyErr != nil {
				return finishFlowPartial(report, output, opts.Now, redactor, strategyErr)
			}
			if strategyFingerprint == "" {
				strategyFingerprint = candidateStrategy.Fingerprint
				report.Strategy = candidateStrategy
			} else if strategyFingerprint != candidateStrategy.Fingerprint {
				return finishFlowPartial(report, output, opts.Now, redactor, errors.New("strategy_identity_drift"))
			}
			if result.ContextCancelled || ctx.Err() != nil {
				if opts.KeepWorkspaces {
					report.Runs[len(report.Runs)-1].Cleanup = &FlowCleanupRecord{Status: "skipped"}
				} else if cleanupErr := cleanupFlowResult(ctx, result, output, prepared); cleanupErr != nil {
					report.Runs[len(report.Runs)-1].Cleanup = &FlowCleanupRecord{Status: "error", Error: cleanupErr.Error(), Paths: flowCleanupPaths(prepared)}
					if err := writeFlowReport(output, report, opts.Now, redactor); err != nil {
						return report, fmt.Errorf("persist cleanup flow report: %w", err)
					}
					return report, cleanupErr
				} else {
					report.Runs[len(report.Runs)-1].Cleanup = &FlowCleanupRecord{Status: "completed", Paths: flowCleanupPaths(prepared)}
				}
				cancelErr := ctx.Err()
				if cancelErr == nil {
					cancelErr = context.Canceled
				}
				return finishFlowPartial(report, output, opts.Now, redactor, cancelErr)
			}
			if paused {
				return finishFlowPartial(report, output, opts.Now, redactor, errors.New("run_paused"))
			}
			if opts.KeepWorkspaces {
				report.Runs[len(report.Runs)-1].Cleanup = &FlowCleanupRecord{Status: "skipped"}
			} else if cleanupErr := cleanupFlowResult(ctx, result, output, prepared); cleanupErr != nil {
				report.Runs[len(report.Runs)-1].Cleanup = &FlowCleanupRecord{Status: "error", Error: cleanupErr.Error(), Paths: flowCleanupPaths(prepared)}
				if err := writeFlowReport(output, report, opts.Now, redactor); err != nil {
					return report, fmt.Errorf("persist cleanup flow report: %w", err)
				}
				return report, cleanupErr
			} else {
				report.Runs[len(report.Runs)-1].Cleanup = &FlowCleanupRecord{Status: "completed", Paths: flowCleanupPaths(prepared)}
			}
			if err := writeFlowReport(output, report, opts.Now, redactor); err != nil {
				return report, err
			}
		}
	}
	finishFlowReport(report)
	report.FinishedAt = opts.Now().UTC()
	report.DurationMS = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	report.Benchmark.DatasetFingerprint = flowDatasetFingerprint(suite, cases)
	report.Benchmark.Fingerprint, err = hashJSON(struct {
		Suite, Cases, Validator, ValidatorID, ValidatorVersion, Protocol, Path, Repeat, Oracle, OS, Arch, Go string
	}{sha256Text(suite.Source), report.Benchmark.DatasetFingerprint, validatorFingerprint, suite.Validator.ID, suite.Validator.Version, FlowValidatorProtocol, report.Environment.PATHSHA256, fmt.Sprint(opts.Repeat), oracleMetadata, goruntime.GOOS, goruntime.GOARCH, goruntime.Version()})
	if err != nil {
		return report, err
	}
	if err := writeFlowReport(output, report, opts.Now, redactor); err != nil {
		return report, err
	}
	for _, gate := range ApplyFlowGates(*suite.Gates, report.Summary) {
		if !gate.Passed {
			return report, &FlowGateFailureError{Report: report}
		}
	}
	return report, nil
}

func flowAuthoritativeRequest(item FlowCase, repeat int, root *store.RunState, prepared *PreparedFlowRepeat, artifactDirs map[string]string) FlowValidationRequest {
	artifacts := ""
	if artifactDirs != nil {
		artifacts = artifactDirs[root.ID]
	}
	if artifacts == "" && root.ExecutionWorkspace != "" {
		artifacts = filepath.Join(root.ExecutionWorkspace, ".takt", "runs", root.ID, "artifacts")
	}
	request := FlowValidationRequest{ProtocolVersion: FlowValidatorProtocol, Type: "validation_request", CaseID: item.ID, Repeat: repeat, Workspace: root.ExecutionWorkspace, Baseline: prepared.BaselineWorkspace, ExpectedPath: item.ExpectedPath, Run: FlowValidationRun{ID: root.ID, Status: root.Status, ArtifactsDir: artifacts}}
	if item.SCMPath != "" {
		request.ExternalState = &FlowValidationExternal{SCMDir: item.SCMPath}
	}
	return request
}

func recordFromFlowSnapshots(caseID string, repeat int, workspace string, states []*store.RunState) RunRecord {
	root := states[0]
	record := RunRecord{CaseID: caseID, Repeat: repeat, RunID: root.ID, Status: root.Status, Workspace: workspace, Mode: "flow", DurationMS: root.UpdatedAt.Sub(root.CreatedAt).Milliseconds(), Answers: len(root.Approvals), ErrorCode: root.ErrorCode, Error: root.Error, Nodes: map[string]NodeRecord{}}
	if root.Usage != nil {
		record.InputTokens, record.OutputTokens, record.Cost = root.Usage.InputTokens, root.Usage.OutputTokens, root.Usage.Cost
	}
	for _, state := range states {
		if state != nil {
			addFlowStateNodes(&record, state.ID, state.Nodes)
		}
	}
	return record
}

func writeFlowRepeatEvidence(output string, item FlowCase, repeat int, result FlowCaseRunResult, request FlowValidationRequest, execution FlowValidationExecution, prepared *PreparedFlowRepeat, redactor *redact.Redactor) error {
	return WriteFlowEvidence(output, FlowEvidence{CaseID: item.ID, Repeat: repeat, States: result.States, Request: request, Validation: execution, Artifacts: result.Artifacts, ArtifactDirs: result.ArtifactDirs, PreparedHeadCommit: prepared.HeadCommit, SCMDir: item.SCMPath}, redactor)
}

func cleanupFlowResult(ctx context.Context, result FlowCaseRunResult, output string, prepared *PreparedFlowRepeat) error {
	if result.Cleanup != nil {
		cleanupCtx := ctx
		if ctx.Err() != nil {
			var cancel context.CancelFunc
			cleanupCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
		}
		if _, err := result.Cleanup(cleanupCtx); err != nil {
			return err
		}
	}
	return CleanupFlowRepeat(output, FlowCleanupPaths{ControlWorkspace: prepared.ControlWorkspace, BaselineWorkspace: prepared.BaselineWorkspace, BareRemote: prepared.BareRemote, Created: flowCleanupPaths(prepared)})
}

func flowCleanupPaths(prepared *PreparedFlowRepeat) []string {
	paths := []string{prepared.ControlWorkspace, prepared.BaselineWorkspace}
	if prepared.BareRemote != "" {
		paths = append(paths, prepared.BareRemote)
	}
	return paths
}

func flowStrategy(state *store.RunState, profileFingerprint string) (StrategyIdentity, error) {
	fingerprint, err := hashJSON(struct{ Workflow, Config, Commands, Profile string }{state.WorkflowFingerprint, state.ConfigFingerprint, state.CommandsFingerprint, profileFingerprint})
	return StrategyIdentity{ID: state.WorkflowPath, Fingerprint: fingerprint, WorkflowFingerprint: state.WorkflowFingerprint, ConfigFingerprint: state.ConfigFingerprint, CommandsFingerprint: state.CommandsFingerprint}, err
}

func flowDatasetFingerprint(suite *FlowSuite, cases []FlowCase) string {
	entries := make([]struct{ ID, Fingerprint string }, len(cases))
	for i, item := range cases {
		entries[i] = struct{ ID, Fingerprint string }{item.ID, item.Fingerprint}
	}
	hash, _ := hashJSON(entries)
	return hash
}

func sha256Text(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

func writeFlowReport(output string, report *SuiteReport, now func() time.Time, redactor *redact.Redactor) error {
	finishFlowReport(report)
	report.FinishedAt = now().UTC()
	report.DurationMS = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if redactor != nil {
		value = redactor.Any(value)
	}
	data, err = json.MarshalIndent(value, "", "  ")
	if err != nil || !json.Valid(data) {
		return fmt.Errorf("encode redacted flow report: %w", err)
	}
	return writeFlowRaw(filepath.Join(output, "report.json"), data, 0644)
}

func finishFlowPartial(report *SuiteReport, output string, now func() time.Time, redactor *redact.Redactor, cause error) (*SuiteReport, error) {
	if report != nil {
		if err := writeFlowReport(output, report, now, redactor); err != nil {
			return report, fmt.Errorf("persist partial flow report: %w", err)
		}
	}
	return report, cause
}

func flowReportPath(output, invocation string) string {
	if invocation == "" {
		return output
	}
	root, err := filepath.Abs(invocation)
	if err != nil {
		return output
	}
	if rel, err := filepath.Rel(root, output); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return output
}

func validateFlowOutput(output string, suite *FlowSuite, invocation string) error {
	if _, err := os.Stat(output); err == nil {
		return fmt.Errorf("flow evaluation output already exists: %s", output)
	} else if !os.IsNotExist(err) {
		return err
	}
	invocationPath := ""
	if invocation != "" {
		var err error
		invocationPath, err = filepath.Abs(invocation)
		if err != nil {
			return err
		}
		invocationPath = filepath.Clean(invocationPath)
	}
	for _, path := range []string{suite.SuiteDir, suite.ResolvedCases} {
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if filepath.Clean(absolute) == invocationPath {
			continue
		}
		if pathsOverlap(output, filepath.Clean(absolute)) {
			return fmt.Errorf("flow evaluation output overlaps protected path: %s", path)
		}
	}
	if invocationPath != "" {
		if invocationPath == output {
			return fmt.Errorf("flow evaluation output is invocation workspace: %s", invocation)
		}
	}
	return nil
}
