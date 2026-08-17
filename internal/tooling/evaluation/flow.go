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
	"sort"
	"strings"
	"time"

	"takt/internal/config"
	"takt/internal/redact"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/version"
)

type FlowCaseRunRequest struct {
	Workspace, Selector, ConfigPath, InputValue, ApprovalAnswer string
	ModelPreset                                                 string
	ModelOverrides                                              map[string]string
	Trace                                                       func(string)
	AssistantIdleTimeout                                        time.Duration
	Progress                                                    func(FlowRuntimeProgress) (*FlowProgress, error)
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
	Trace                                             func(string)
	AssistantIdleTimeout                              time.Duration
	ModelPreset                                       string
	ModelOverrides                                    map[string]string
}

type FlowGateFailureError struct{ Report *SuiteReport }

func (e *FlowGateFailureError) Error() string { return "flow evaluation gates failed" }

func RunFlow(ctx context.Context, opts FlowRunOptions) (result *SuiteReport, resultErr error) {
	var progressTracker *flowProgressTracker
	defer func() {
		if progressTracker != nil && resultErr != nil {
			if err := progressTracker.fail(resultErr); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("persist failed flow progress: %w", err))
			}
		}
	}()
	if opts.CaseRunner == nil {
		return nil, errors.New("flow case runner is required")
	}
	if opts.AssistantIdleTimeout <= 0 {
		opts.AssistantIdleTimeout = 5 * time.Minute
	}
	if opts.Repeat <= 0 {
		opts.Repeat = 1
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	startedAt := opts.Now().UTC()
	suite, err := LoadFlowSuite(opts.SuitePath)
	if err != nil {
		return nil, err
	}
	cases, err := DiscoverFlowCases(suite.SuitePath, suite, opts.CaseID)
	if err != nil {
		return nil, err
	}
	outputDir := opts.OutputDir
	if outputDir == "" {
		suiteName := strings.Trim(safeCaseID.ReplaceAllString(filepath.Base(filepath.Dir(suite.SuitePath)), "-"), "-.")
		if suiteName == "" {
			return nil, errors.New("flow evaluation suite name is empty after sanitization")
		}
		if opts.InvocationWorkspace == "" {
			return nil, errors.New("invocation workspace is required for default flow evaluation output")
		}
		outputDir = filepath.Join(opts.InvocationWorkspace, ".takt", "evals", suiteName, startedAt.Format("20060102T150405.000000000Z"))
	}
	output, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, err
	}
	output, err = canonicalPath(output)
	if err != nil {
		return nil, err
	}
	if err := validateFlowOutput(output, suite, opts.InvocationWorkspace); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return nil, err
	}
	traceFlow(opts.Trace, "EVAL | start | suite=%s workflow=%s case=%s repeat=%d model_preset=%s model_overrides=%s assistant_idle_timeout=%s output=%s", suite.SuitePath, suite.Workflow, opts.CaseID, opts.Repeat, opts.ModelPreset, flowModelReferenceSummary(opts.ModelOverrides), opts.AssistantIdleTimeout, output)
	traceFlow(opts.Trace, "EVAL | plan | prepare -> validator_preflight -> workflow -> validator -> evidence -> cleanup -> finalized")
	pathHash := sha256.Sum256([]byte(opts.HostPATH))
	validatorFingerprint, err := hashPath(suite.Validator.ResolvedPath)
	if err != nil {
		return nil, fmt.Errorf("fingerprint validator: %w", err)
	}
	report := &SuiteReport{
		ReportVersion: ReportVersion, TaktVersion: version.Value, StartedAt: startedAt,
		Workflow: suite.Workflow, Config: suite.Config, CasesDir: suite.Cases.Directory,
		OutputDir: flowReportPath(output, opts.InvocationWorkspace), Mode: "flow", Summary: newSummary(),
		Benchmark:   BenchmarkIdentity{ID: filepath.Base(suite.SuitePath), CaseCount: len(cases), ValidationProtocol: FlowValidatorProtocol, Validator: ValidatorIdentity{ID: suite.Validator.ID, Version: suite.Validator.Version, Path: suite.Validator.Path, Fingerprint: validatorFingerprint}},
		Environment: EnvironmentIdentity{GOOS: goruntime.GOOS, GOARCH: goruntime.GOARCH, GoVersion: goruntime.Version(), PATHSHA256: hex.EncodeToString(pathHash[:]), AssistantIdleTimeout: opts.AssistantIdleTimeout.String()},
	}
	progressTracker, err = newFlowProgressTracker(output, FlowProgress{
		ReportVersion: FlowProgressVersion, Status: "running", Suite: suite.SuitePath, Workflow: suite.Workflow, OutputDir: output,
		StartedAt: startedAt, TotalRuns: len(cases) * opts.Repeat, Runtime: FlowRuntimeProgress{RunningNodes: []string{}}, Results: FlowProgressResults{},
	}, opts.Now)
	if err != nil {
		return report, fmt.Errorf("create flow evaluation progress: %w", err)
	}
	var oracleMetadata, strategyFingerprint string
	preparedIdentities := make([]string, 0, len(cases)*opts.Repeat)
	redactor := redact.NewFromEnvironment()
	ordinal := 0
	for _, item := range cases {
		for repeat := 1; repeat <= opts.Repeat; repeat++ {
			ordinal++
			if err := progressTracker.begin(item.ID, repeat, ordinal); err != nil {
				return finishFlowPartial(report, output, opts.Now, redactor, err)
			}
			caseScope := fmt.Sprintf("CASE %s#%d", item.ID, repeat)
			traceFlow(opts.Trace, "%s | prepare", caseScope)
			prepared, prepErr := PrepareFlowRepeat(ctx, suite, item, repeat, output, opts.HostPATH, config.ModelSelection{Preset: opts.ModelPreset, Overrides: opts.ModelOverrides})
			if prepErr != nil {
				return finishFlowPartial(report, output, opts.Now, nil, prepErr)
			}
			preparedIdentities = append(preparedIdentities, flowPreparedIdentity(prepared))
			traceFlow(opts.Trace, "%s | prepared | workspace=%s", caseScope, prepared.ControlWorkspace)
			cfg, cfgErr := config.Load(prepared.ConfigPath)
			if cfgErr != nil {
				return finishFlowPartial(report, output, opts.Now, nil, cfgErr)
			}
			redactor.Merge(redact.NewFromConfig(cfg))
			retryOwner := ""
			if _, ok := flowPiAssistant(cfg); ok {
				retryOwner = " provider_retry_owner=takt pi_internal_retries=disabled"
			}
			traceFlow(opts.Trace, "%s | config loaded | assistant=%s model_preset=%s models=%s%s", caseScope, cfg.DefaultAssistant, prepared.ModelPreset, flowModelReferenceSummary(prepared.EffectiveModels), retryOwner)
			if err := progressTracker.phase("validator_preflight"); err != nil {
				return finishFlowPartial(report, output, opts.Now, redactor, err)
			}
			traceFlow(opts.Trace, "%s | validator preflight", caseScope)
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
			if err := progressTracker.phase("workflow"); err != nil {
				return finishFlowPartial(report, output, opts.Now, redactor, err)
			}
			result, callbackErr := opts.CaseRunner(ctx, FlowCaseRunRequest{Workspace: prepared.ControlWorkspace, Selector: selector, ConfigPath: prepared.ConfigPath, InputValue: prepared.InputValue, ApprovalAnswer: approval, ModelPreset: prepared.ModelPreset, ModelOverrides: opts.ModelOverrides, Trace: opts.Trace, AssistantIdleTimeout: opts.AssistantIdleTimeout, Progress: progressTracker.runtime})
			if len(result.States) == 0 || result.States[0] == nil {
				if callbackErr == nil {
					callbackErr = errors.New("flow case runner returned no root snapshot")
				}
				return finishFlowPartial(report, output, opts.Now, redactor, callbackErr)
			}

			root := result.States[0]
			record := recordFromFlowSnapshots(item.ID, repeat, prepared.ControlWorkspace, result.States)
			applyRuntimeMetricsFromEvents(&record, root, result.Events, "")
			if err := progressTracker.phase("validator"); err != nil {
				return finishFlowPartial(report, output, opts.Now, redactor, err)
			}
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
			traceFlow(opts.Trace, "%s | validator %s | valid=%t", caseScope, validationResult.Status, validationResult.Result != nil && validationResult.Result.Valid)
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
			}
			ClassifyFlowRecord(&record)
			traceFlow(opts.Trace, "RUN %s | result | case=%s#%d status=%s outcome=%s valid=%t diagnostic=%s", flowTraceRunID(root.ID), item.ID, repeat, record.Status, record.Outcome, validationResult.Result != nil && validationResult.Result.Valid, flowValidationDiagnostic(validationResult))
			report.Runs = append(report.Runs, record)
			addSummary(&report.Summary, record)
			if err := progressTracker.results(len(report.Runs), report.Summary); err != nil {
				return finishFlowPartial(report, output, opts.Now, redactor, err)
			}
			if err := progressTracker.phase("evidence"); err != nil {
				return finishFlowPartial(report, output, opts.Now, redactor, err)
			}
			if err := writeFlowRepeatEvidence(output, item, repeat, result, request, validationResult, prepared, redactor); err != nil {
				return finishFlowPartial(report, output, opts.Now, redactor, err)
			}
			traceFlow(opts.Trace, "%s | evidence written", caseScope)
			if err := writeFlowReport(output, report, opts.Now, redactor); err != nil {
				return report, err
			}
			traceFlow(opts.Trace, "REPORT | checkpoint | phase=validation path=%s", filepath.Join(output, "report.json"))
			candidateStrategy, strategyErr := flowStrategy(root, prepared.ProfileFingerprint, prepared.ModelPreset, prepared.EffectiveModels)
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
				if err := progressTracker.phase("cleanup"); err != nil {
					return finishFlowPartial(report, output, opts.Now, redactor, err)
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
				cancelErr := ctx.Err()
				if cancelErr == nil {
					cancelErr = context.Canceled
				}
				return finishFlowPartial(report, output, opts.Now, redactor, cancelErr)
			}
			if paused {
				return finishFlowPartial(report, output, opts.Now, redactor, errors.New("run_paused"))
			}
			if err := progressTracker.phase("cleanup"); err != nil {
				return finishFlowPartial(report, output, opts.Now, redactor, err)
			}
			if opts.KeepWorkspaces {
				report.Runs[len(report.Runs)-1].Cleanup = &FlowCleanupRecord{Status: "skipped"}
				traceFlow(opts.Trace, "%s | cleanup skipped", caseScope)
			} else if cleanupErr := cleanupFlowResult(ctx, result, output, prepared); cleanupErr != nil {
				report.Runs[len(report.Runs)-1].Cleanup = &FlowCleanupRecord{Status: "error", Error: cleanupErr.Error(), Paths: flowCleanupPaths(prepared)}
				if err := writeFlowReport(output, report, opts.Now, redactor); err != nil {
					return report, fmt.Errorf("persist cleanup flow report: %w", err)
				}
				return report, cleanupErr
			} else {
				report.Runs[len(report.Runs)-1].Cleanup = &FlowCleanupRecord{Status: "completed", Paths: flowCleanupPaths(prepared)}
				traceFlow(opts.Trace, "%s | cleanup completed", caseScope)
			}
			if err := writeFlowReport(output, report, opts.Now, redactor); err != nil {
				return report, err
			}
			traceFlow(opts.Trace, "REPORT | checkpoint | phase=cleanup path=%s", filepath.Join(output, "report.json"))
			if callbackErr != nil {
				return finishFlowPartial(report, output, opts.Now, redactor, callbackErr)
			}
		}
	}
	finishFlowReport(report)
	report.FinishedAt = opts.Now().UTC()
	report.DurationMS = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	report.Benchmark.DatasetFingerprint = flowDatasetFingerprint(suite, cases)
	report.Benchmark.Fingerprint = flowBenchmarkFingerprint(sha256Text(suite.Source), report.Benchmark.DatasetFingerprint, validatorFingerprint, suite.Validator.ID, suite.Validator.Version, report.Environment.PATHSHA256, oracleMetadata, opts.Repeat, opts.AssistantIdleTimeout, preparedIdentities)
	if err := writeFlowReport(output, report, opts.Now, redactor); err != nil {
		return report, err
	}
	traceFlow(opts.Trace, "REPORT | finalized | path=%s", filepath.Join(output, "report.json"))
	for _, gate := range ApplyFlowGates(*suite.Gates, report.Summary) {
		if !gate.Passed {
			return report, &FlowGateFailureError{Report: report}
		}
	}
	if err := progressTracker.complete(filepath.Join(output, "report.json")); err != nil {
		return report, err
	}
	return report, nil
}

func flowModelSummary(cfg *spec.Config) string {
	if cfg == nil || len(cfg.Models) == 0 {
		return "none"
	}
	names := make([]string, 0, len(cfg.Models))
	for name := range cfg.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	values := make([]string, 0, len(names))
	for _, name := range names {
		model := cfg.Models[name]
		values = append(values, fmt.Sprintf("%s=%s/%s", name, model.Provider, model.ID))
	}
	return strings.Join(values, ",")
}

func flowModelReferenceSummary(models map[string]string) string {
	if len(models) == 0 {
		return "none"
	}
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	values := make([]string, 0, len(names))
	for _, name := range names {
		values = append(values, name+"="+models[name])
	}
	return strings.Join(values, ",")
}

func flowValidationDiagnostic(result FlowValidationExecution) string {
	if result.Result != nil && len(result.Result.Diagnostics) > 0 {
		return result.Result.Diagnostics[0].Code
	}
	return result.ErrorCode
}

func traceFlow(trace func(string), format string, args ...any) {
	if trace != nil {
		trace(fmt.Sprintf(format, args...))
	}
}

func flowTraceRunID(runID string) string {
	if strings.HasPrefix(runID, "run-") && len(runID) > len("run-")+8 {
		return runID[len("run-") : len("run-")+8]
	}
	return runID
}

func redactFlowReport(report *SuiteReport, redactor *redact.Redactor) error {
	if report == nil || redactor == nil {
		return nil
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	data, err = json.Marshal(redactor.Any(json.RawMessage(data)))
	if err != nil {
		return err
	}
	var redactedReport SuiteReport
	if err := json.Unmarshal(data, &redactedReport); err != nil {
		return err
	}
	*report = redactedReport
	return nil
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
	if item.SCMPath != "" && root.ExecutionWorkspace != "" {
		request.ExternalState = &FlowValidationExternal{SCMDir: filepath.Join(root.ExecutionWorkspace, ".takt", "evals", "scm")}
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
	scmDir := ""
	if request.ExternalState != nil {
		scmDir = request.ExternalState.SCMDir
	}
	return WriteFlowEvidence(output, FlowEvidence{CaseID: item.ID, Repeat: repeat, States: result.States, Events: result.Events, Request: request, Validation: execution, Artifacts: result.Artifacts, ArtifactDirs: result.ArtifactDirs, PreparedHeadCommit: prepared.HeadCommit, SCMDir: scmDir}, redactor)
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

func flowStrategy(state *store.RunState, profileFingerprint, modelPreset string, models map[string]string) (StrategyIdentity, error) {
	fingerprint, err := hashJSON(struct{ Workflow, Config, Commands, Profile string }{state.WorkflowFingerprint, state.ConfigFingerprint, state.CommandsFingerprint, profileFingerprint})
	return StrategyIdentity{ID: state.WorkflowPath, Fingerprint: fingerprint, WorkflowFingerprint: state.WorkflowFingerprint, ConfigFingerprint: state.ConfigFingerprint, CommandsFingerprint: state.CommandsFingerprint, ModelPreset: modelPreset, Models: models}, err
}

func flowDatasetFingerprint(suite *FlowSuite, cases []FlowCase) string {
	entries := make([]struct{ ID, Fingerprint string }, len(cases))
	for i, item := range cases {
		entries[i] = struct{ ID, Fingerprint string }{item.ID, item.Fingerprint}
	}
	hash, _ := hashJSON(entries)
	return hash
}

func flowPreparedIdentity(prepared *PreparedFlowRepeat) string {
	if prepared == nil {
		return ""
	}
	identity, _ := hashJSON(struct {
		BaseCommit, HeadCommit string
	}{prepared.BaseCommit, prepared.HeadCommit})
	return identity
}

func flowBenchmarkFingerprint(suite, cases, validator, validatorID, validatorVersion, path, oracle string, repeat int, assistantIdleTimeout time.Duration, prepared []string) string {
	fingerprint, _ := hashJSON(struct {
		Suite, Cases, Validator, ValidatorID, ValidatorVersion, Protocol, Path, Repeat, Oracle, OS, Arch, Go, AssistantIdleTimeout string
		Prepared                                                                                                                   []string
	}{suite, cases, validator, validatorID, validatorVersion, FlowValidatorProtocol, path, fmt.Sprint(repeat), oracle, goruntime.GOOS, goruntime.GOARCH, goruntime.Version(), assistantIdleTimeout.String(), prepared})
	return fingerprint
}

func sha256Text(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

func writeFlowReport(output string, report *SuiteReport, now func() time.Time, redactor *redact.Redactor) error {
	finishFlowReport(report)
	report.FinishedAt = now().UTC()
	report.DurationMS = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	if err := redactFlowReport(report, redactor); err != nil {
		return err
	}
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
	root, err := canonicalPath(invocation)
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
		invocationPath, err = canonicalPath(invocation)
		if err != nil {
			return err
		}
		invocationPath = filepath.Clean(invocationPath)
	}
	for _, path := range []string{suite.SuiteDir, suite.ResolvedCases} {
		if path == "" {
			continue
		}
		absolute, err := canonicalPath(path)
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
