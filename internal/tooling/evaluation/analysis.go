package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"takt/internal/config"
	"takt/internal/execution"
	"takt/internal/profile"
	"takt/internal/redact"
	"takt/internal/store"
	toolingpkg "takt/internal/tooling"
)

const AnalysisReportVersion = "takt-evaluation-analysis/v1alpha1"
const DefaultAnalysisLanguage = toolingpkg.DefaultEvaluationAnalysisLanguage

var analysisFailureModePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func NormalizeAnalysisLanguage(raw string) (string, error) {
	return toolingpkg.NormalizeEvaluationAnalysisLanguage(raw)
}

type AnalysisRunOptions struct {
	OutputDir, ConfigPath string
	ModelPreset, CaseID   string
	Language              string
	Repeat                int
	Trace                 func(string)
	Now                   func() time.Time
	CaseRunner            FlowCaseRunner
}

type AnalysisCaseInput struct {
	CaseID       string `json:"case_id"`
	Repeat       int    `json:"repeat"`
	ManifestPath string `json:"manifest_path"`
	EvidenceRoot string `json:"evidence_root"`
	Language     string `json:"language"`
}

func traceAnalysis(trace func(string), format string, args ...any) {
	if trace != nil {
		trace(fmt.Sprintf(format, args...))
	}
}

func uniqueAnalysisStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func persistAnalysisTrace(caseOutputDir string, lines []string, redactor *redact.Redactor) error {
	data := []byte(strings.Join(lines, "\n"))
	if redactor != nil {
		data = []byte(redactor.String(string(data)))
	}
	if len(data) > maxAnalysisTraceBytes {
		data = data[:maxAnalysisTraceBytes]
	}
	if len(data) > 0 && len(data) < maxAnalysisTraceBytes {
		data = append(data, '\n')
	}
	return analysisAtomicBytes(filepath.Join(caseOutputDir, "trace.log"), data)
}

const maxAnalysisTraceBytes = 1 << 20

func SelectAnalysisCases(report *SuiteReport, caseID string, repeat int) ([]AnalysisCase, error) {
	if report == nil {
		return nil, errors.New("evaluation report is required")
	}
	if repeat > 0 && strings.TrimSpace(caseID) == "" {
		return nil, errors.New("repeat requires --case")
	}
	if repeat < 0 {
		return nil, errors.New("repeat cannot be negative")
	}
	selected := make([]AnalysisCase, 0)
	for _, run := range report.Runs {
		if run.Repeat <= 0 {
			return nil, fmt.Errorf("evaluation report contains invalid repeat %d for case %q", run.Repeat, run.CaseID)
		}
		if caseID != "" && run.CaseID != caseID {
			continue
		}
		if repeat > 0 && run.Repeat != repeat {
			continue
		}
		if caseID == "" && run.Outcome == "true_accept" {
			continue
		}
		selected = append(selected, AnalysisCase{CaseID: run.CaseID, Repeat: run.Repeat})
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].CaseID != selected[j].CaseID {
			return selected[i].CaseID < selected[j].CaseID
		}
		return selected[i].Repeat < selected[j].Repeat
	})
	return selected, nil
}

func AnalyzeFlow(ctx context.Context, opts AnalysisRunOptions) (*AnalysisRunReport, error) {
	language, err := NormalizeAnalysisLanguage(opts.Language)
	if err != nil {
		return nil, err
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	started := opts.Now().UTC()
	if started.IsZero() || started.UnixNano() <= 0 {
		return nil, errors.New("analysis clock must be positive")
	}
	if opts.CaseRunner == nil {
		return nil, errors.New("analysis case runner is required")
	}
	if opts.OutputDir == "" {
		return nil, errors.New("analysis output directory is required")
	}
	output, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(filepath.Join(output, "report.json")); os.IsNotExist(statErr) {
		if progress, progressErr := LoadFlowProgress(output); progressErr == nil && progress.Status == "running" {
			current, phase := "unknown", "unknown"
			if progress.Current != nil {
				current = fmt.Sprintf("%s#%d", progress.Current.CaseID, progress.Current.Repeat)
				phase = progress.Current.Phase
			}
			return nil, fmt.Errorf("evaluation is still running: current=%s phase=%s; analysis requires completed case evidence", current, phase)
		}
	}
	configPath, err := filepath.Abs(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	if opts.ConfigPath == "" {
		return nil, errors.New("analysis config path is required")
	}
	if _, err := os.Stat(configPath); err != nil {
		return nil, fmt.Errorf("analysis config: %w", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	materialized, preset, err := config.MaterializeModels(cfg, config.ModelSelection{Preset: opts.ModelPreset})
	if err != nil {
		return nil, err
	}
	model, ok := materialized.Models["takt_analyze"]
	if !ok {
		label := preset
		if label == "" {
			label = "legacy"
		}
		return nil, fmt.Errorf("model alias %q is not defined in preset %q", "takt_analyze", label)
	}
	report, err := LoadReport(output)
	if err != nil {
		return nil, err
	}
	cases, err := SelectAnalysisCases(report, opts.CaseID, opts.Repeat)
	if err != nil {
		return nil, err
	}
	configFingerprint, err := hashPath(configPath)
	if err != nil {
		return nil, err
	}
	analysisDir := filepath.Join(output, "analyses", started.Format("20060102T150405.000000000Z"))
	modelPreset := preset
	if modelPreset == "" {
		modelPreset = "legacy"
	}
	modelInfo := AnalysisModel{Preset: modelPreset, Alias: "takt_analyze", Provider: model.Provider, ID: model.ID}
	redactor := redact.NewFromEnvironment()
	redactor.Merge(redact.NewFromConfig(cfg))
	analysisTraceLines := []string{}
	var analysisTraceMu sync.Mutex
	analysisTrace := func(line string) {
		line = redactor.String(line)
		analysisTraceMu.Lock()
		analysisTraceLines = append(analysisTraceLines, line)
		analysisTraceMu.Unlock()
		if opts.Trace != nil {
			opts.Trace(line)
		}
	}
	for {
		if _, statErr := os.Stat(analysisDir); os.IsNotExist(statErr) {
			break
		}
		analysisDir += "-001"
	}
	// Keep every incremental report schema-valid. A run is pessimistically
	// failed until finalization proves that all selected cases completed.
	runReport := &AnalysisRunReport{ReportVersion: AnalysisReportVersion, OutputDir: analysisDir, SourceEvaluationDir: output, Status: "failed", StartedAt: started, Language: language, Model: modelInfo, TracePath: "trace.log", SelectedCases: make([]AnalysisCaseRef, len(cases)), Analyses: []AnalysisCaseReport{}}
	for i, c := range cases {
		runReport.SelectedCases[i] = AnalysisCaseRef{CaseID: c.CaseID, Repeat: c.Repeat}
	}
	manifest := AnalysisManifest{Version: analysisManifestVersion, SourceEvaluationDir: output, SelectedCases: runReport.SelectedCases, ConfigFingerprint: configFingerprint, Language: language, Model: modelInfo, Trace: "trace.log", Workspaces: []AnalysisWorkspaceRef{}}
	traceAnalysis(analysisTrace, "analysis.selected cases=%d source=%s", len(cases), output)
	if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "manifest.json"), manifest, redactor); err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		runReport.Status = "no_cases"
		runReport.FinishedAt = opts.Now().UTC()
		runReport.DurationMS = runReport.FinishedAt.Sub(started).Milliseconds()
		if err := persistAnalysisTrace(analysisDir, analysisTraceLines, redactor); err != nil {
			return nil, err
		}
		if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "report.json"), runReport, redactor); err != nil {
			return nil, err
		}
		return runReport, nil
	}
	var firstErr error
	rememberErr := func(err error) {
		if err == nil {
			return
		}
		if firstErr == nil {
			firstErr = err
			return
		}
		firstErr = errors.Join(firstErr, err)
	}
	for _, c := range cases {
		caseOutputDir := filepath.Join(analysisDir, "cases", sanitizeCaseID(c.CaseID), fmt.Sprintf("repeat-%03d", c.Repeat))
		traceLines := []string{}
		var caseTraceMu sync.Mutex
		caseTrace := func(line string) {
			line = redactor.String(line)
			caseTraceMu.Lock()
			traceLines = append(traceLines, line)
			caseTraceMu.Unlock()
			analysisTrace(line)
		}
		caseTrace(fmt.Sprintf("analysis.case.selected case=%s repeat=%d", c.CaseID, c.Repeat))
		persistCase := func(caseReport AnalysisCaseReport) {
			if caseReport.rawOutput != "" {
				if path, err := persistAnalysisRawOutput(caseOutputDir, caseReport.rawOutput, redactor); err != nil {
					rememberErr(fmt.Errorf("persist analysis raw output %s#%d: %w", c.CaseID, c.Repeat, err))
				} else {
					caseReport.RawOutputPath = path
				}
				caseReport.rawOutput = ""
			}
			caseTrace(fmt.Sprintf("analysis.report.write case=%s repeat=%d", c.CaseID, c.Repeat))
			if err := persistAnalysisTrace(caseOutputDir, traceLines, redactor); err != nil {
				rememberErr(fmt.Errorf("persist analysis trace %s#%d: %w", c.CaseID, c.Repeat, err))
			} else {
				caseReport.TracePath = "trace.log"
			}
			runReport.Analyses = append(runReport.Analyses, caseReport)
			if err := analysisAtomicJSONRedacted(filepath.Join(caseOutputDir, "analysis.json"), caseReport, redactor); err != nil {
				rememberErr(fmt.Errorf("persist analysis case %s#%d: %w", c.CaseID, c.Repeat, err))
			}
			if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "report.json"), runReport, redactor); err != nil {
				rememberErr(fmt.Errorf("persist analysis report: %w", err))
			}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			if firstErr == nil {
				firstErr = ctxErr
			}
			persistCase(failedAnalysisCase(c, RunRecord{CaseID: c.CaseID, Repeat: c.Repeat}, modelInfo, "not_run", ctxErr))
			continue
		}
		if !safeAnalysisCaseID(c.CaseID) {
			err := fmt.Errorf("unsafe evaluation case ID %q", c.CaseID)
			if firstErr == nil {
				firstErr = err
			}
			persistCase(failedAnalysisCase(c, RunRecord{CaseID: c.CaseID, Repeat: c.Repeat}, modelInfo, "not_run", err))
			continue
		}
		originalRun, ok := findRun(report, c)
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("run not found for %s repeat %d", c.CaseID, c.Repeat)
			}
			persistCase(failedAnalysisCase(c, RunRecord{CaseID: c.CaseID, Repeat: c.Repeat}, modelInfo, "not_run", firstErr))
			continue
		}
		inspection, err := InspectFlowEvaluation(output, c.CaseID, c.Repeat)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			persistCase(failedAnalysisCase(c, originalRun, modelInfo, "not_run", err))
			continue
		}
		var inspectCase *InspectionCase
		for i := range inspection.Cases {
			if inspection.Cases[i].CaseID == c.CaseID && inspection.Cases[i].Repeat == c.Repeat {
				inspectCase = &inspection.Cases[i]
				break
			}
		}
		if inspectCase == nil {
			if firstErr == nil {
				firstErr = errors.New("analysis evidence case not found")
			}
			persistCase(failedAnalysisCase(c, originalRun, modelInfo, "not_run", firstErr))
			continue
		}
		repeatRoot := filepath.Join(output, "cases", c.CaseID, fmt.Sprintf("repeat-%03d", c.Repeat))
		workspace := filepath.Join(caseOutputDir, "workspace")
		caseTrace(fmt.Sprintf("analysis.preparation workspace=%s", workspace))
		if _, err := profile.Init("evaluation", workspace, false); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			persistCase(failedAnalysisCase(c, originalRun, modelInfo, "not_run", err))
			continue
		}
		evidenceRoot := filepath.Join(workspace, "evidence")
		missingEvidence, err := copyAnalysisEvidenceRoot(repeatRoot, evidenceRoot, redactor)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			persistCase(failedAnalysisCase(c, originalRun, modelInfo, "not_run", err))
			continue
		}
		caseTrace(fmt.Sprintf("analysis.evidence.copied root=%s missing=%d", evidenceRoot, len(missingEvidence)))
		em, err := buildAnalysisEvidenceManifest(evidenceRoot, evidenceRoot, inspectCase, originalRun, report)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			persistCase(failedAnalysisCase(c, originalRun, modelInfo, "not_run", err))
			continue
		}
		em.EvidenceRoot = "evidence"
		em.MissingEvidence = append(em.MissingEvidence, missingEvidence...)
		sort.Strings(em.MissingEvidence)
		em.MissingEvidence = uniqueAnalysisStrings(em.MissingEvidence)
		if err := analysisAtomicJSONRedacted(filepath.Join(workspace, "evidence-manifest.json"), em, redactor); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			persistCase(failedAnalysisCase(c, originalRun, modelInfo, "persistence_error", err))
			continue
		}
		caseTrace(fmt.Sprintf("analysis.evidence.manifest path=%s", filepath.Join(workspace, "evidence-manifest.json")))
		input := AnalysisCaseInput{CaseID: c.CaseID, Repeat: c.Repeat, ManifestPath: "evidence-manifest.json", EvidenceRoot: em.EvidenceRoot, Language: language}
		inputJSON, _ := json.Marshal(input)
		inputJSON, _ = redactor.Bytes(inputJSON)
		if err := analysisAtomicBytes(filepath.Join(workspace, "input.json"), inputJSON); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			persistCase(failedAnalysisCase(c, originalRun, modelInfo, "persistence_error", err))
			continue
		}
		manifest.Workspaces = append(manifest.Workspaces, AnalysisWorkspaceRef{CaseID: c.CaseID, Repeat: c.Repeat, Workspace: filepath.ToSlash(filepath.Join("cases", sanitizeCaseID(c.CaseID), fmt.Sprintf("repeat-%03d", c.Repeat), "workspace")), EvidenceManifest: filepath.ToSlash(filepath.Join("cases", sanitizeCaseID(c.CaseID), fmt.Sprintf("repeat-%03d", c.Repeat), "workspace", "evidence-manifest.json")), Trace: filepath.ToSlash(filepath.Join("cases", sanitizeCaseID(c.CaseID), fmt.Sprintf("repeat-%03d", c.Repeat), "trace.log"))})
		if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "manifest.json"), manifest, redactor); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			persistCase(failedAnalysisCase(c, originalRun, modelInfo, "persistence_error", err))
			continue
		}
		caseTrace(fmt.Sprintf("analysis.callback.start case=%s repeat=%d", c.CaseID, c.Repeat))
		result, callbackErr := opts.CaseRunner(ctx, FlowCaseRunRequest{Workspace: workspace, Selector: "evaluation:analyze", ConfigPath: configPath, InputValue: string(inputJSON), ModelPreset: opts.ModelPreset, Trace: caseTrace})
		caseTrace(fmt.Sprintf("analysis.callback.finish case=%s repeat=%d callback_error=%t", c.CaseID, c.Repeat, callbackErr != nil))
		caseReport := analysisCaseReportFromRunEvidence(c, originalRun, modelInfo, result, callbackErr, &em, evidenceRoot)
		if fp, ferr := hashJSON(em); ferr == nil {
			caseReport.EvidenceFingerprint = fp
		}
		if len(result.States) > 0 && result.States[0] != nil {
			if node := result.States[0].Nodes["analyze"]; node != nil {
				if err := captureAnalysisSessionEvidence(caseOutputDir, result.States[0].ExecutionWorkspace, &caseReport.Session, redactor); err != nil {
					caseReport.AnalysisStatus, caseReport.ErrorCode, caseReport.Error, caseReport.Analysis = "persistence_error", "persistence_error", err.Error(), nil
				}
				caseTrace(fmt.Sprintf("analysis.session adapter=%s session=%s evidence=%s path=%s", caseReport.Session.Adapter, caseReport.Session.SessionID, caseReport.Session.SessionEvidence, caseReport.Session.SessionEvidencePath))
			}
		}
		caseTrace("analysis.trace.persist before_cleanup")
		if err := persistAnalysisTrace(caseOutputDir, traceLines, redactor); err != nil {
			rememberErr(fmt.Errorf("persist analysis trace %s#%d before cleanup: %w", c.CaseID, c.Repeat, err))
		}
		if result.Cleanup != nil {
			caseTrace("analysis.cleanup.start")
			if _, cleanupErr := result.Cleanup(context.WithoutCancel(ctx)); cleanupErr != nil {
				caseReport.AnalysisStatus, caseReport.ErrorCode, caseReport.Error, caseReport.Analysis = "persistence_error", "persistence_error", cleanupErr.Error(), nil
			}
			caseTrace("analysis.cleanup.finish")
		}
		if caseReport.AnalysisStatus != "completed" && firstErr == nil {
			firstErr = fmt.Errorf("analysis %s: %s", caseReport.ErrorCode, valueOrDash(caseReport.Error))
		}
		if callbackErr != nil && firstErr == nil {
			firstErr = callbackErr
		}
		persistCase(caseReport)
	}
	runReport.Status = "completed"
	if firstErr != nil {
		runReport.Status = "failed"
	}
	runReport.FinishedAt = opts.Now().UTC()
	runReport.DurationMS = runReport.FinishedAt.Sub(started).Milliseconds()
	if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "manifest.json"), manifest, redactor); err != nil {
		runReport.Status = "failed"
		rememberErr(fmt.Errorf("persist analysis manifest: %w", err))
	}
	if err := persistAnalysisTrace(analysisDir, analysisTraceLines, redactor); err != nil {
		runReport.Status = "failed"
		rememberErr(fmt.Errorf("persist analysis trace: %w", err))
	}
	if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "report.json"), runReport, redactor); err != nil {
		runReport.Status = "failed"
		rememberErr(fmt.Errorf("persist analysis report: %w", err))
	}
	return runReport, firstErr
}

func safeAnalysisCaseID(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(filepath.Clean(value)) == value && !strings.ContainsAny(value, `/\\`)
}

func findRun(report *SuiteReport, c AnalysisCase) (RunRecord, bool) {
	for _, r := range report.Runs {
		if r.CaseID == c.CaseID && r.Repeat == c.Repeat {
			return r, true
		}
	}
	return RunRecord{}, false
}

func analysisCaseReportFromRun(c AnalysisCase, deterministic RunRecord, model AnalysisModel, result FlowCaseRunResult, callbackErr error) AnalysisCaseReport {
	return analysisCaseReportFromRunEvidence(c, deterministic, model, result, callbackErr, nil, "")
}

func analysisCaseReportFromRunEvidence(c AnalysisCase, deterministic RunRecord, model AnalysisModel, result FlowCaseRunResult, callbackErr error, manifest *AnalysisEvidenceManifest, evidenceRoot string) AnalysisCaseReport {
	causeSource, cause := primaryRunCause(deterministic)
	r := AnalysisCaseReport{CaseID: c.CaseID, Repeat: c.Repeat, Deterministic: AnalysisDeterministic{Status: deterministic.Status, Outcome: deterministic.Outcome, CauseSource: causeSource, Cause: cause}, Model: model, AnalysisStatus: "not_run", Session: AnalysisSession{Adapter: "unavailable", SessionEvidence: "unavailable"}}
	if len(result.States) == 0 || result.States[0] == nil {
		r.ErrorCode = classifyAnalysisError(nil, callbackErr)
		r.AnalysisStatus = r.ErrorCode
		if callbackErr != nil {
			r.Error = callbackErr.Error()
		} else {
			r.Error = "analyze case runner returned no root snapshot"
		}
		return r
	}
	root := result.States[0]
	if node, ok := root.Nodes["analyze"]; ok && node != nil {
		r.Session.SessionID = node.SessionID
		r.Session.SessionPath = node.SessionPath
		r.Session.Adapter = node.Adapter
		r.Prompt = node.Prompt
		r.PromptFingerprint = node.PromptFingerprint
	}
	node, ok := root.Nodes["analyze"]
	if !ok || node == nil {
		r.ErrorCode = "not_run"
		r.AnalysisStatus = "not_run"
		r.Error = "analyze node not found"
		return r
	}
	if r.Session.Adapter == "" {
		r.Session.Adapter = node.Adapter
	}
	if r.Session.Adapter == "" {
		r.Session.Adapter = node.Assistant
	}
	if r.Session.Adapter == "" {
		r.Session.Adapter = "unavailable"
	}
	r.Usage = AnalysisUsage{}
	if node.Usage != nil {
		r.Usage.InputTokens = node.Usage.InputTokens
		r.Usage.OutputTokens = node.Usage.OutputTokens
		r.Usage.Cost = node.Usage.Cost
	}
	r.Usage.DurationMS = root.UpdatedAt.Sub(root.CreatedAt).Milliseconds()
	if r.Usage.DurationMS < 0 {
		r.Usage.DurationMS = 0
	}
	if callbackErr != nil || node.Status != store.NodeCompleted {
		r.ErrorCode = node.ErrorCode
		if r.ErrorCode == "" {
			r.ErrorCode = classifyAnalysisError(node, callbackErr)
		}
		r.Error = node.Error
		if r.Error == "" && callbackErr != nil {
			r.Error = callbackErr.Error()
		}
		if r.Error == "" {
			r.Error = fmt.Sprintf("analyze node terminated with status %s", node.Status)
		}
		r.AnalysisStatus = normalizeAnalysisStatus(r.ErrorCode)
		r.ErrorCode = r.AnalysisStatus
		if r.AnalysisStatus == "protocol" {
			r.rawOutput = node.Output
			if r.rawOutput == "" {
				r.rawOutput = node.Stdout
			}
		}
		return r
	}
	var analysis AdvisoryAnalysis
	if err := json.Unmarshal([]byte(node.Output), &analysis); err != nil {
		r.AnalysisStatus = "protocol"
		r.ErrorCode = "protocol"
		r.Error = err.Error()
		r.rawOutput = node.Output
		return r
	}
	if err := validateAdvisoryAnalysis(analysis); err != nil {
		r.AnalysisStatus = "protocol"
		r.ErrorCode = "protocol"
		r.Error = err.Error()
		r.rawOutput = node.Output
		return r
	}
	if manifest != nil {
		if err := validateAdvisoryAnalysisEvidence(analysis, *manifest, evidenceRoot); err != nil {
			r.AnalysisStatus = "protocol"
			r.ErrorCode = "protocol"
			r.Error = err.Error()
			r.rawOutput = node.Output
			return r
		}
	}
	r.Analysis = &analysis
	r.AnalysisStatus = "completed"
	return r
}

func validateAdvisoryAnalysis(value AdvisoryAnalysis) error {
	switch value.PrimaryClass {
	case "infrastructure", "assistant", "workflow", "candidate", "validator", "task", "unknown":
	default:
		return fmt.Errorf("analysis primary_class %q is invalid", value.PrimaryClass)
	}
	if strings.TrimSpace(value.FailureMode) == "" || strings.TrimSpace(value.RootCause) == "" {
		return errors.New("analysis failure_mode and root_cause are required")
	}
	if !analysisFailureModePattern.MatchString(value.FailureMode) {
		return fmt.Errorf("analysis failure_mode %q must be a lowercase machine code", value.FailureMode)
	}
	if strings.TrimSpace(value.CausalMechanism) == "" || strings.TrimSpace(value.FailurePoint) == "" || strings.TrimSpace(value.Prevention) == "" {
		return errors.New("analysis causal_mechanism, failure_point and prevention are required")
	}
	switch value.FailurePoint {
	case "assistant_decision", "workflow_control", "validator", "infrastructure", "unknown":
	default:
		return fmt.Errorf("analysis failure_point %q is invalid", value.FailurePoint)
	}
	switch value.Confidence {
	case "high", "medium", "low":
	default:
		return fmt.Errorf("analysis confidence %q is invalid", value.Confidence)
	}
	if len(value.Evidence) == 0 {
		return errors.New("analysis evidence is required")
	}
	for i, evidence := range value.Evidence {
		if strings.TrimSpace(evidence.Path) == "" || strings.TrimSpace(evidence.Pointer) == "" || strings.TrimSpace(evidence.Fact) == "" {
			return fmt.Errorf("analysis evidence[%d] requires path, pointer and fact", i)
		}
	}
	for i, link := range value.CausalChain {
		if strings.TrimSpace(link.Fact) == "" || strings.TrimSpace(link.Consequence) == "" || len(link.Evidence) == 0 {
			return fmt.Errorf("analysis causal_chain[%d] is incomplete", i)
		}
	}
	return nil
}

func validateAdvisoryAnalysisEvidence(value AdvisoryAnalysis, manifest AnalysisEvidenceManifest, evidenceRoot string) error {
	files := make(map[string]AnalysisEvidenceFile, len(manifest.Files))
	for _, file := range manifest.Files {
		files[file.Path] = file
	}
	runtimeEvidence := false
	for i, evidence := range value.Evidence {
		path := normalizeAnalysisCitationPath(evidence.Path, manifest, files)
		if err := validateEvidenceCitation(path, normalizeAnalysisCitationPointer(evidence.Pointer), files, evidenceRoot); err != nil {
			return fmt.Errorf("analysis evidence[%d] citation: %w", i, err)
		}
		if !analysisValidatorOnlyPath(path) {
			runtimeEvidence = true
		}
	}
	for i, link := range value.CausalChain {
		for j, citation := range link.Evidence {
			path, pointer, ok := splitAnalysisCitation(citation)
			if !ok {
				return fmt.Errorf("analysis causal_chain[%d].evidence[%d] citation %q is invalid", i, j, citation)
			}
			path = normalizeAnalysisCitationPath(path, manifest, files)
			if err := validateEvidenceCitation(path, pointer, files, evidenceRoot); err != nil {
				return fmt.Errorf("analysis causal_chain[%d].evidence[%d] citation: %w", i, j, err)
			}
			if !analysisValidatorOnlyPath(path) {
				runtimeEvidence = true
			}
		}
	}
	if !runtimeEvidence {
		return errors.New("analysis evidence must include runtime, assistant, artifact, source, diff or SCM evidence beyond the validator verdict")
	}
	return nil
}

func analysisValidatorOnlyPath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	switch clean {
	case "validation-request.json", "validation-result.json", "validator.stderr", "evidence-manifest.json":
		return true
	default:
		return false
	}
}

func normalizeAnalysisCitationPointer(raw string) string {
	pointer := strings.TrimSpace(raw)
	if strings.HasPrefix(pointer, "#") {
		pointer = strings.TrimPrefix(pointer, "#")
	}
	if strings.HasPrefix(pointer, ":") {
		return "line:" + strings.TrimPrefix(pointer, ":")
	}
	return pointer
}

func splitAnalysisCitation(raw string) (string, string, bool) {
	if path, pointer, ok := strings.Cut(raw, "#"); ok && path != "" && pointer != "" {
		return path, normalizeAnalysisCitationPointer(pointer), true
	}
	colon := strings.LastIndex(raw, ":")
	if colon <= 0 || colon == len(raw)-1 {
		return "", "", false
	}
	line := raw[colon+1:]
	if _, _, ok := parseLineCitation("line:" + line); !ok {
		return "", "", false
	}
	return raw[:colon], "line:" + line, true
}

func normalizeAnalysisCitationPath(raw string, manifest AnalysisEvidenceManifest, files map[string]AnalysisEvidenceFile) string {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(raw)))
	root := filepath.ToSlash(filepath.Clean(filepath.FromSlash(manifest.EvidenceRoot)))
	if root == "." || root == "" || strings.HasPrefix(root, "../") || filepath.IsAbs(root) {
		return clean
	}
	prefix := root + "/"
	if !strings.HasPrefix(clean, prefix) {
		return clean
	}
	trimmed := strings.TrimPrefix(clean, prefix)
	if _, listed := files[trimmed]; listed || trimmed == "evidence-manifest.json" {
		return trimmed
	}
	return clean
}

func validateEvidenceCitation(rawPath, pointer string, files map[string]AnalysisEvidenceFile, evidenceRoot string) error {
	pointer = normalizeAnalysisCitationPointer(pointer)
	if strings.TrimSpace(rawPath) == "" || strings.ContainsRune(rawPath, '\x00') || strings.Contains(rawPath, "\\") || filepath.IsAbs(rawPath) {
		return fmt.Errorf("path %q is not relative", rawPath)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rawPath)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("path %q escapes evidence root", rawPath)
	}
	manifestCitation := clean == "evidence-manifest.json"
	file, ok := files[clean]
	if !ok && !manifestCitation {
		return fmt.Errorf("path %q is not in evidence manifest", rawPath)
	}
	root, err := filepath.Abs(evidenceRoot)
	if err != nil {
		return err
	}
	path := filepath.Join(root, filepath.FromSlash(clean))
	if manifestCitation {
		path = filepath.Join(filepath.Dir(root), "evidence-manifest.json")
	}
	if !manifestCitation {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("path %q escapes evidence root", rawPath)
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("path %q is unavailable: %w", rawPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("path %q is not a regular file", rawPath)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !manifestCitation && (file.Size > 0 || file.SHA256 != "") && (file.Size != int64(len(data)) || (file.SHA256 != "" && !strings.EqualFold(file.SHA256, sha256Bytes(data)))) {
		return fmt.Errorf("path %q does not match evidence manifest", rawPath)
	}
	if start, end, ok := parseLineCitation(pointer); ok {
		if !isTextEvidence(data) {
			return fmt.Errorf("line citation requires a text file")
		}
		lineCount := len(strings.Split(string(data), "\n"))
		if start < 1 || end < start || end > lineCount {
			return fmt.Errorf("line citation %q is outside file", pointer)
		}
		return nil
	}
	if strings.HasPrefix(pointer, "/") && isTextEvidence(data) && json.Valid(data) {
		var document any
		if err := json.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("decode JSON evidence: %w", err)
		}
		if _, err := resolveJSONPointer(document, pointer); err != nil {
			return err
		}
		return nil
	}
	if isTextEvidence(data) {
		if line, ok := parseZeroBasedLineCitation(pointer); ok {
			lineCount := len(strings.Split(string(data), "\n"))
			if line < 0 || line >= lineCount {
				return fmt.Errorf("line citation %q is outside file", pointer)
			}
			return nil
		}
	}
	if !strings.HasPrefix(pointer, "/") {
		return fmt.Errorf("JSON pointer %q is invalid", pointer)
	}
	return fmt.Errorf("JSON pointer requires a JSON text file")
}

func parseZeroBasedLineCitation(pointer string) (int, bool) {
	value := strings.TrimPrefix(strings.TrimSpace(pointer), "/")
	if value == "" || strings.Contains(value, "/") {
		return 0, false
	}
	line, err := strconv.Atoi(value)
	return line, err == nil && line >= 0
}

func isTextEvidence(data []byte) bool {
	return utf8.Valid(data) && !strings.ContainsRune(string(data), 0)
}

func parseLineCitation(pointer string) (int, int, bool) {
	value := strings.TrimSpace(pointer)
	if strings.HasPrefix(value, "line:") {
		value = strings.TrimPrefix(value, "line:")
	} else if strings.HasPrefix(value, "L") {
		value = strings.TrimPrefix(value, "L")
		value = strings.ReplaceAll(value, "-L", "-")
	}
	if value == "" || strings.Contains(value, "/") {
		return 0, 0, false
	}
	parts := strings.Split(value, "-")
	if len(parts) > 2 {
		return 0, 0, false
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil || start < 1 {
		return 0, 0, false
	}
	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
	}
	return start, end, true
}

func resolveJSONPointer(document any, pointer string) (any, error) {
	current := document
	for _, token := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch value := current.(type) {
		case map[string]any:
			next, ok := value[token]
			if !ok {
				return nil, fmt.Errorf("JSON pointer %q does not resolve", pointer)
			}
			current = next
		case []any:
			if token == "" || (len(token) > 1 && token[0] == '0') {
				return nil, fmt.Errorf("JSON pointer %q has invalid array index", pointer)
			}
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(value) {
				return nil, fmt.Errorf("JSON pointer %q does not resolve", pointer)
			}
			current = value[index]
		default:
			return nil, fmt.Errorf("JSON pointer %q does not resolve", pointer)
		}
	}
	return current, nil
}

func failedAnalysisCase(c AnalysisCase, deterministic RunRecord, model AnalysisModel, status string, err error) AnalysisCaseReport {
	causeSource, cause := primaryRunCause(deterministic)
	r := AnalysisCaseReport{CaseID: c.CaseID, Repeat: c.Repeat, Deterministic: AnalysisDeterministic{Status: deterministic.Status, Outcome: deterministic.Outcome, CauseSource: causeSource, Cause: cause}, AnalysisStatus: status, ErrorCode: status, Model: model, Session: AnalysisSession{Adapter: "unavailable", SessionEvidence: "unavailable"}}
	if err != nil {
		r.Error = err.Error()
	}
	if r.Error == "" {
		r.Error = status
	}
	if fingerprint, hashErr := hashJSON(struct {
		CaseID string
		Repeat int
		RunID  string
		Status string
		Error  string
	}{c.CaseID, c.Repeat, deterministic.RunID, status, r.Error}); hashErr == nil {
		r.EvidenceFingerprint = fingerprint
	}
	return r
}

const maxAnalysisRawOutputBytes = 1 << 20

func persistAnalysisRawOutput(caseOutputDir, raw string, redactor *redact.Redactor) (string, error) {
	data := []byte(raw)
	if redactor != nil {
		data, _ = redactor.Bytes(data)
	}
	if len(data) > maxAnalysisRawOutputBytes {
		data = data[:maxAnalysisRawOutputBytes]
	}
	const relativePath = "raw-output.txt"
	if err := analysisAtomicBytes(filepath.Join(caseOutputDir, relativePath), data); err != nil {
		return "", err
	}
	return relativePath, nil
}

func captureAnalysisSessionEvidence(caseOutputDir, executionWorkspace string, session *AnalysisSession, redactor *redact.Redactor) error {
	if session == nil || session.SessionPath == "" {
		return nil
	}
	sourcePath := session.SessionPath
	if !filepath.IsAbs(sourcePath) {
		if executionWorkspace == "" {
			return nil
		}
		workspace, err := filepath.Abs(executionWorkspace)
		if err != nil {
			return nil
		}
		sourcePath = filepath.Join(workspace, filepath.FromSlash(sourcePath))
		rel, err := filepath.Rel(workspace, sourcePath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
		resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
		if err != nil {
			return nil
		}
		resolvedSource, err := filepath.EvalSymlinks(sourcePath)
		if err != nil {
			return nil
		}
		resolvedRel, err := filepath.Rel(resolvedWorkspace, resolvedSource)
		if err != nil || resolvedRel != rel || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	info, err := os.Lstat(sourcePath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxSessionEvidenceBytes {
		return nil
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil
	}
	if len(data) > maxSessionEvidenceBytes {
		return nil
	}
	if redactor != nil {
		redacted, matched := redactor.Bytes(data)
		if matched && !isTextEvidence(data) {
			return fmt.Errorf("analysis session contains known secret in non-UTF-8 data: %s", sourcePath)
		}
		data = redacted
	}
	if len(data) > maxSessionEvidenceBytes {
		return nil
	}
	const relativePath = "sessions/analyze.jsonl"
	if err := analysisAtomicBytes(filepath.Join(caseOutputDir, filepath.FromSlash(relativePath)), data); err != nil {
		return err
	}
	session.SessionEvidence = "recorded"
	session.SessionEvidencePath = relativePath
	return nil
}

func normalizeAnalysisStatus(s string) string {
	switch s {
	case "provider_unavailable", "timed_out", "protocol", "protocol_error":
		if s == "protocol_error" {
			return "protocol"
		}
		return s
	case "persistence_error", "not_run":
		return s
	case "cancelled", "blocked", "skipped":
		return "not_run"
	default:
		return "persistence_error"
	}
}

func classifyAnalysisError(node *store.NodeState, err error) string {
	if node != nil && node.ErrorCode != "" {
		switch node.ErrorCode {
		case "provider_unavailable", "timed_out", "protocol_error", "persistence_error", "not_run":
			return node.ErrorCode
		}
	}
	if node != nil {
		switch node.Status {
		case store.NodeTimedOut:
			return "timed_out"
		case store.NodeFailed, store.NodeErrored:
			if node.ErrorCode == "protocol" || node.ErrorCode == "protocol_error" {
				return "protocol"
			}
			if node.ErrorCode == "provider_unavailable" {
				return "provider_unavailable"
			}
			return "persistence_error"
		case store.NodeCancelled, store.NodeBlocked, store.NodeSkipped:
			return "not_run"
		}
	}
	if err != nil {
		switch execution.KindOf(err) {
		case execution.KindProviderUnavailable:
			return "provider_unavailable"
		case execution.KindTimedOut:
			return "timed_out"
		case execution.KindProtocol:
			return "protocol"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "timed_out"
		}
		if errors.Is(err, context.Canceled) {
			return "not_run"
		}
		return "persistence_error"
	}
	return "not_run"
}
