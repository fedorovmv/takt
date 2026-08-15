package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"takt/internal/config"
	"takt/internal/profile"
	"takt/internal/redact"
	"takt/internal/store"
)

const AnalysisReportVersion = "takt-evaluation-analysis/v1alpha1"

type AnalysisRunOptions struct {
	OutputDir, ConfigPath string
	ModelPreset, CaseID   string
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
}

func SelectAnalysisCases(report *SuiteReport, caseID string, repeat int) ([]AnalysisCase, error) {
	if report == nil {
		return nil, errors.New("evaluation report is required")
	}
	if repeat > 0 && strings.TrimSpace(caseID) == "" {
		return nil, errors.New("repeat requires case")
	}
	if repeat < 0 {
		return nil, errors.New("repeat cannot be negative")
	}
	selected := make([]AnalysisCase, 0)
	for _, run := range report.Runs {
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
		return nil, fmt.Errorf("model alias %q is not defined in preset %q", "takt_analyze", preset)
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
	modelInfo := AnalysisModel{Preset: preset, Alias: "takt_analyze", Provider: model.Provider, ID: model.ID}
	redactor := redact.NewFromEnvironment()
	redactor.Merge(redact.NewFromConfig(cfg))
	for {
		if _, statErr := os.Stat(analysisDir); os.IsNotExist(statErr) {
			break
		}
		analysisDir += "-001"
	}
	runReport := &AnalysisRunReport{ReportVersion: AnalysisReportVersion, OutputDir: analysisDir, SourceEvaluationDir: output, Status: "running", StartedAt: started, Model: modelInfo, SelectedCases: make([]AnalysisCaseRef, len(cases)), Analyses: []AnalysisCaseReport{}}
	for i, c := range cases {
		runReport.SelectedCases[i] = AnalysisCaseRef{CaseID: c.CaseID, Repeat: c.Repeat}
	}
	manifest := AnalysisManifest{Version: analysisManifestVersion, SourceEvaluationDir: output, SelectedCases: runReport.SelectedCases, ConfigFingerprint: configFingerprint, Model: modelInfo, Workspaces: []AnalysisWorkspaceRef{}}
	if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "manifest.json"), manifest, redactor); err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		runReport.Status = "no_cases"
		runReport.FinishedAt = opts.Now().UTC()
		runReport.DurationMS = runReport.FinishedAt.Sub(started).Milliseconds()
		if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "report.json"), runReport, redactor); err != nil {
			return nil, err
		}
		return runReport, nil
	}
	var firstErr error
	for _, c := range cases {
		if ctx.Err() != nil {
			firstErr = ctx.Err()
			break
		}
		originalRun, ok := findRun(report, c)
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("run not found for %s repeat %d", c.CaseID, c.Repeat)
			}
			continue
		}
		inspection, err := InspectFlowEvaluation(output, c.CaseID, c.Repeat)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			runReport.Analyses = append(runReport.Analyses, failedAnalysisCase(c, originalRun, modelInfo, "not_run", err))
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
			runReport.Analyses = append(runReport.Analyses, failedAnalysisCase(c, originalRun, modelInfo, "not_run", firstErr))
			continue
		}
		repeatRoot := filepath.Join(output, "cases", c.CaseID, fmt.Sprintf("repeat-%03d", c.Repeat))
		workspace := filepath.Join(analysisDir, "cases", sanitizeCaseID(c.CaseID), fmt.Sprintf("repeat-%03d", c.Repeat), "workspace")
		if _, err := profile.Init("evaluation", workspace, false); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			runReport.Analyses = append(runReport.Analyses, failedAnalysisCase(c, originalRun, modelInfo, "not_run", err))
			continue
		}
		em, err := buildAnalysisEvidenceManifest(output, repeatRoot, inspectCase, originalRun)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			runReport.Analyses = append(runReport.Analyses, failedAnalysisCase(c, originalRun, modelInfo, "not_run", err))
			continue
		}
		em.EvidenceRoot, _ = filepath.Rel(workspace, repeatRoot)
		em.EvidenceRoot = filepath.ToSlash(em.EvidenceRoot)
		if err := analysisAtomicJSONRedacted(filepath.Join(workspace, "evidence-manifest.json"), em, redactor); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		input := AnalysisCaseInput{CaseID: c.CaseID, Repeat: c.Repeat, ManifestPath: "evidence-manifest.json", EvidenceRoot: em.EvidenceRoot}
		inputJSON, _ := json.Marshal(input)
		inputJSON, _ = redactor.Bytes(inputJSON)
		if err := analysisAtomicBytes(filepath.Join(workspace, "input.json"), inputJSON); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			runReport.Analyses = append(runReport.Analyses, failedAnalysisCase(c, originalRun, modelInfo, "persistence_error", err))
			continue
		}
		manifest.Workspaces = append(manifest.Workspaces, AnalysisWorkspaceRef{CaseID: c.CaseID, Repeat: c.Repeat, Workspace: filepath.ToSlash(filepath.Join("cases", sanitizeCaseID(c.CaseID), fmt.Sprintf("repeat-%03d", c.Repeat), "workspace")), EvidenceManifest: filepath.ToSlash(filepath.Join("cases", sanitizeCaseID(c.CaseID), fmt.Sprintf("repeat-%03d", c.Repeat), "workspace", "evidence-manifest.json"))})
		_ = analysisAtomicJSONRedacted(filepath.Join(analysisDir, "manifest.json"), manifest, redactor)
		result, callbackErr := opts.CaseRunner(ctx, FlowCaseRunRequest{Workspace: workspace, Selector: "evaluation:analyze", ConfigPath: configPath, InputValue: string(inputJSON), ModelPreset: opts.ModelPreset, Trace: opts.Trace})
		caseReport := analysisCaseReportFromRun(c, originalRun, modelInfo, result, callbackErr)
		if fp, ferr := hashJSON(em); ferr == nil {
			caseReport.EvidenceFingerprint = fp
		}
		if caseReport.AnalysisStatus != "completed" && firstErr == nil {
			firstErr = errors.New(caseReport.ErrorCode)
		}
		if callbackErr != nil && firstErr == nil {
			firstErr = callbackErr
		}
		runReport.Analyses = append(runReport.Analyses, caseReport)
		if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "cases", sanitizeCaseID(c.CaseID), fmt.Sprintf("repeat-%03d", c.Repeat), "analysis.json"), caseReport, redactor); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "report.json"), runReport, redactor); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	runReport.Status = "completed"
	if firstErr != nil {
		runReport.Status = "failed"
	}
	runReport.FinishedAt = opts.Now().UTC()
	runReport.DurationMS = runReport.FinishedAt.Sub(started).Milliseconds()
	if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "manifest.json"), manifest, redactor); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := analysisAtomicJSONRedacted(filepath.Join(analysisDir, "report.json"), runReport, redactor); err != nil && firstErr == nil {
		firstErr = err
	}
	return runReport, firstErr
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
	r := AnalysisCaseReport{CaseID: c.CaseID, Repeat: c.Repeat, Deterministic: AnalysisDeterministic{Status: deterministic.Status, Outcome: deterministic.Outcome}, Model: model, AnalysisStatus: "not_run", Session: AnalysisSession{SessionEvidence: "unavailable"}}
	if len(result.States) == 0 {
		r.ErrorCode = "not_run"
		if callbackErr != nil {
			r.Error = callbackErr.Error()
		}
		return r
	}
	root := result.States[0]
	if node, ok := root.Nodes["analyze"]; ok && node != nil {
		r.Session.SessionID = node.SessionID
		r.Session.SessionPath = node.SessionPath
		r.Session.Adapter = node.Adapter
	}
	r.Usage = AnalysisUsage{}
	r.Usage.DurationMS = root.UpdatedAt.Sub(root.CreatedAt).Milliseconds()
	if r.Usage.DurationMS < 0 {
		r.Usage.DurationMS = 0
	}
	if root.Usage != nil {
		r.Usage.InputTokens = root.Usage.InputTokens
		r.Usage.OutputTokens = root.Usage.OutputTokens
		r.Usage.Cost = root.Usage.Cost
	}
	node, ok := root.Nodes["analyze"]
	if !ok || node == nil {
		r.ErrorCode = "not_run"
		r.Error = "analyze node not found"
		return r
	}
	if r.Session.Adapter == "" {
		r.Session.Adapter = node.Assistant
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
		r.AnalysisStatus = normalizeAnalysisStatus(r.ErrorCode)
		r.ErrorCode = r.AnalysisStatus
		return r
	}
	var analysis AdvisoryAnalysis
	if err := json.Unmarshal([]byte(node.Output), &analysis); err != nil {
		r.AnalysisStatus = "protocol"
		r.ErrorCode = "protocol"
		r.Error = err.Error()
		return r
	}
	r.Analysis = &analysis
	r.AnalysisStatus = "completed"
	return r
}

func failedAnalysisCase(c AnalysisCase, deterministic RunRecord, model AnalysisModel, status string, err error) AnalysisCaseReport {
	r := AnalysisCaseReport{CaseID: c.CaseID, Repeat: c.Repeat, Deterministic: AnalysisDeterministic{Status: deterministic.Status, Outcome: deterministic.Outcome}, AnalysisStatus: status, ErrorCode: status, Model: model, Session: AnalysisSession{SessionEvidence: "unavailable"}}
	if err != nil {
		r.Error = err.Error()
	}
	return r
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
	if err != nil {
		return "persistence_error"
	}
	return "not_run"
}
