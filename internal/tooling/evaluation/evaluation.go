package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"time"

	cfgpkg "takt/internal/config"
	"takt/internal/definition"
	"takt/internal/redact"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/validation"
	"takt/internal/version"
	"takt/internal/workflow"
)

const ReportVersion = "takt-evaluation/v1alpha1"

type EvaluationRunner interface {
	Start(context.Context, string) (*store.RunState, error)
	Resume(context.Context, *store.RunState) (*store.RunState, error)
}

type Execution struct {
	Runner EvaluationRunner
	Store  store.Repository
}

type ExecutionFactory func(*spec.Workflow, *spec.Config, string, string, string) (Execution, error)

type RunOptions struct {
	ExecutionFactory  ExecutionFactory
	WorkflowPath      string
	ConfigPath        string
	CasesDir          string
	WorkspaceTemplate string
	OutputDir         string
	Repeat            int
	ApprovalAnswer    string
	Replace           bool
	StrategyID        string
	BenchmarkID       string
	QualityNode       string
	GenerationNode    string
	ValidatorID       string
	ValidatorVersion  string
	ValidatorPath     string
	CaseManifestPath  string
	ModelPreset       string
	ModelOverrides    map[string]string
}

type SuiteReport struct {
	ReportVersion string              `json:"report_version"`
	TaktVersion   string              `json:"takt_version"`
	StartedAt     time.Time           `json:"started_at"`
	FinishedAt    time.Time           `json:"finished_at"`
	DurationMS    int64               `json:"duration_ms"`
	Workflow      string              `json:"workflow"`
	Config        string              `json:"config"`
	CasesDir      string              `json:"cases_dir"`
	OutputDir     string              `json:"output_dir"`
	Strategy      StrategyIdentity    `json:"strategy"`
	Benchmark     BenchmarkIdentity   `json:"benchmark"`
	Environment   EnvironmentIdentity `json:"environment"`
	Runs          []RunRecord         `json:"runs"`
	Summary       Summary             `json:"summary"`
	Mode          string              `json:"mode,omitempty"`
}

type StrategyIdentity struct {
	ID                  string            `json:"id"`
	Fingerprint         string            `json:"fingerprint"`
	WorkflowFingerprint string            `json:"workflow_fingerprint"`
	ConfigFingerprint   string            `json:"config_fingerprint"`
	CommandsFingerprint string            `json:"commands_fingerprint"`
	ModelPreset         string            `json:"model_preset,omitempty"`
	Models              map[string]string `json:"models,omitempty"`
}

type BenchmarkIdentity struct {
	ID                      string            `json:"id"`
	Fingerprint             string            `json:"fingerprint"`
	DatasetFingerprint      string            `json:"dataset_fingerprint"`
	WorkspaceFingerprint    string            `json:"workspace_fingerprint"`
	CaseCount               int               `json:"case_count"`
	QualityNode             string            `json:"quality_node,omitempty"`
	GenerationNode          string            `json:"generation_node,omitempty"`
	ValidationProtocol      string            `json:"validation_protocol,omitempty"`
	Validator               ValidatorIdentity `json:"validator,omitempty"`
	CaseManifestFingerprint string            `json:"case_manifest_fingerprint,omitempty"`
}

type ValidatorIdentity struct {
	ID          string `json:"id,omitempty"`
	Version     string `json:"version,omitempty"`
	Path        string `json:"path,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type EnvironmentIdentity struct {
	GOOS                 string `json:"goos"`
	GOARCH               string `json:"goarch"`
	GoVersion            string `json:"go_version"`
	PATHSHA256           string `json:"path_sha256,omitempty"`
	OracleMetadataSHA256 string `json:"oracle_metadata_sha256,omitempty"`
	AssistantIdleTimeout string `json:"assistant_idle_timeout,omitempty"`
}

type Summary struct {
	Total                       int                       `json:"total"`
	ByStatus                    map[string]int            `json:"by_status"`
	Attempts                    int                       `json:"attempts"`
	InfrastructureErrors        int                       `json:"infrastructure_errors"`
	InputTokens                 int                       `json:"input_tokens"`
	OutputTokens                int                       `json:"output_tokens"`
	Cost                        float64                   `json:"cost"`
	DurationMS                  int64                     `json:"duration_ms"`
	Answers                     int                       `json:"answers"`
	Truncated                   int                       `json:"truncated_nodes"`
	Resumed                     int                       `json:"resumed_nodes"`
	ByAssistant                 map[string]int            `json:"by_assistant"`
	ByAssistantVersion          map[string]int            `json:"by_assistant_version"`
	ByRequestedModel            map[string]int            `json:"by_requested_model"`
	ByResolvedModel             map[string]int            `json:"by_resolved_model"`
	UsageByExecutionIdentity    map[string]UsageBreakdown `json:"usage_by_execution_identity"`
	MixedExecutionIdentityNodes int                       `json:"mixed_execution_identity_nodes"`
	QualityRuns                 int                       `json:"quality_runs"`
	Valid                       int                       `json:"valid"`
	Invalid                     int                       `json:"invalid"`
	ValidAtFirstAttempt         int                       `json:"valid_at_first_attempt"`
	ScoredRuns                  int                       `json:"scored_runs"`
	SuccessAt1                  *float64                  `json:"success_at_1"`
	FinalSuccessRate            *float64                  `json:"final_success_rate"`
	AverageAttemptsToValid      *float64                  `json:"average_attempts_to_valid"`
	AverageScore                *float64                  `json:"average_score"`
	CostPerValid                *float64                  `json:"cost_per_valid"`
	AmortizedEndToEndMSPerValid *float64                  `json:"amortized_end_to_end_ms_per_valid"`
	DiagnosticsBySeverity       map[string]int            `json:"diagnostics_by_severity"`
	DiagnosticsByCode           map[string]int            `json:"diagnostics_by_code"`
	DiagnosticsByFingerprint    map[string]int            `json:"diagnostics_by_fingerprint"`
	AverageTimeToValidMS        *float64                  `json:"average_time_to_valid_ms"`
	RetryScheduled              int                       `json:"retry_scheduled"`
	FailedExecutions            int                       `json:"failed_executions"`
	FailedExecutionCost         float64                   `json:"failed_execution_cost"`
	StableValidCases            int                       `json:"stable_valid_cases"`
	StableInvalidCases          int                       `json:"stable_invalid_cases"`
	UnstableCases               int                       `json:"unstable_cases"`
	Flow                        *FlowSummary              `json:"flow,omitempty"`
}

type UsageBreakdown struct {
	Executions   int     `json:"executions"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

type RunRecord struct {
	CaseID              string                `json:"case_id"`
	Repeat              int                   `json:"repeat"`
	Labels              map[string]string     `json:"labels,omitempty"`
	RunID               string                `json:"run_id,omitempty"`
	Status              string                `json:"status"`
	Workspace           string                `json:"workspace"`
	DurationMS          int64                 `json:"duration_ms"`
	Attempts            int                   `json:"attempts"`
	ProviderAttempts    int                   `json:"provider_attempts"`
	AttemptsToValid     *int                  `json:"attempts_to_valid"`
	ValidAtFirstAttempt bool                  `json:"valid_at_first_attempt"`
	InputTokens         int                   `json:"input_tokens"`
	OutputTokens        int                   `json:"output_tokens"`
	Cost                float64               `json:"cost"`
	Answers             int                   `json:"answers"`
	Truncated           int                   `json:"truncated_nodes"`
	Resumed             int                   `json:"resumed_nodes"`
	MixedIdentityNodes  int                   `json:"mixed_execution_identity_nodes"`
	ErrorCode           string                `json:"error_code,omitempty"`
	Error               string                `json:"error,omitempty"`
	Quality             *validation.Result    `json:"quality,omitempty"`
	QualityNodeStatus   string                `json:"quality_node_status,omitempty"`
	QualityError        string                `json:"quality_error,omitempty"`
	QualityExpected     bool                  `json:"quality_expected"`
	TimeToValidMS       *int64                `json:"time_to_valid_ms"`
	RetryScheduled      int                   `json:"retry_scheduled"`
	RetryFingerprints   []string              `json:"retry_fingerprints,omitempty"`
	Nodes               map[string]NodeRecord `json:"nodes"`
	Mode                string                `json:"mode,omitempty"`
	Validation          *FlowValidationRecord `json:"validation,omitempty"`
	Outcome             string                `json:"outcome,omitempty"`
	RunPassed           *bool                 `json:"run_passed,omitempty"`
	Cleanup             *FlowCleanupRecord    `json:"cleanup,omitempty"`
}

type NodeRecord struct {
	Status           string                 `json:"status"`
	Attempts         int                    `json:"attempts"`
	ProviderAttempts int                    `json:"provider_attempts"`
	DurationMS       *int64                 `json:"duration_ms,omitempty"`
	Assistant        string                 `json:"assistant,omitempty"`
	AssistantVersion string                 `json:"assistant_version,omitempty"`
	RequestedModel   *store.ModelRef        `json:"requested_model,omitempty"`
	ResolvedModel    *store.ModelRef        `json:"resolved_model,omitempty"`
	SessionID        string                 `json:"session_id,omitempty"`
	Resumed          bool                   `json:"resumed"`
	ExitCode         int                    `json:"exit_code"`
	ErrorCode        string                 `json:"error_code,omitempty"`
	Error            string                 `json:"error,omitempty"`
	Feedback         string                 `json:"feedback,omitempty"`
	DiagnosticOutput string                 `json:"diagnostic_output,omitempty"`
	Stdout           string                 `json:"stdout,omitempty"`
	Stderr           string                 `json:"stderr,omitempty"`
	OutputTruncated  bool                   `json:"output_truncated"`
	Usage            *store.Usage           `json:"usage,omitempty"`
	Diagnostic       *store.DiagnosticState `json:"diagnostic,omitempty"`
	MixedIdentity    bool                   `json:"mixed_execution_identity"`
	Executions       []ExecutionRecord      `json:"executions"`
}

type ExecutionRecord struct {
	Attempt          int                    `json:"attempt"`
	ProviderAttempt  int                    `json:"provider_attempt"`
	Status           string                 `json:"status"`
	Assistant        string                 `json:"assistant,omitempty"`
	AssistantVersion string                 `json:"assistant_version,omitempty"`
	RequestedModel   *store.ModelRef        `json:"requested_model,omitempty"`
	ResolvedModel    *store.ModelRef        `json:"resolved_model,omitempty"`
	SessionID        string                 `json:"session_id,omitempty"`
	Resumed          bool                   `json:"resumed"`
	ExitCode         int                    `json:"exit_code"`
	ErrorCode        string                 `json:"error_code,omitempty"`
	Error            string                 `json:"error,omitempty"`
	OutputTruncated  bool                   `json:"output_truncated"`
	Usage            *store.Usage           `json:"usage,omitempty"`
	Diagnostic       *store.DiagnosticState `json:"diagnostic,omitempty"`
}

var safeCaseID = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func Run(ctx context.Context, opts RunOptions) (*SuiteReport, error) {
	if opts.Repeat <= 0 {
		opts.Repeat = 1
	}
	paths, err := resolveOptions(opts)
	if err != nil {
		return nil, err
	}
	cases, err := listCases(paths.CasesDir)
	if err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("evaluation cases directory %s contains no .md files", paths.CasesDir)
	}
	caseIDs, err := resolveCaseIDs(cases)
	if err != nil {
		return nil, err
	}
	caseLabels, manifestFingerprint, err := loadCaseManifest(opts.CaseManifestPath, paths.CasesDir, caseIDs)
	if err != nil {
		return nil, err
	}
	wf, err := workflow.Load(paths.WorkflowPath)
	if err != nil {
		return nil, err
	}
	cfg, err := cfgpkg.Load(paths.ConfigPath)
	if err != nil {
		return nil, err
	}
	cfg, selectedPreset, err := cfgpkg.MaterializeModels(cfg, cfgpkg.ModelSelection{Preset: opts.ModelPreset, Overrides: opts.ModelOverrides})
	if err != nil {
		return nil, err
	}
	opts.ModelPreset = selectedPreset
	if opts.StrategyID == "" {
		opts.StrategyID = wf.Name
	}
	if opts.BenchmarkID == "" {
		opts.BenchmarkID = filepath.Base(paths.CasesDir)
	}
	if opts.QualityNode != "" {
		if !workflowHasNode(wf.Nodes, opts.QualityNode) {
			return nil, fmt.Errorf("quality node %q is not present in workflow", opts.QualityNode)
		}
		if opts.GenerationNode == "" {
			opts.GenerationNode = firstAssistantNode(wf.Nodes)
		}
		if opts.GenerationNode == "" || !workflowHasNode(wf.Nodes, opts.GenerationNode) {
			return nil, fmt.Errorf("generation node %q is not present in workflow", opts.GenerationNode)
		}
	}
	ids, err := buildIdentities(paths, opts, wf, cfg, cases, caseIDs, manifestFingerprint)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(paths.OutputDir, 0o755); err != nil {
		return nil, err
	}

	report := &SuiteReport{
		ReportVersion: ReportVersion,
		TaktVersion:   version.Value,
		StartedAt:     time.Now().UTC(),
		Workflow:      paths.WorkflowPath,
		Config:        paths.ConfigPath,
		CasesDir:      paths.CasesDir,
		OutputDir:     paths.OutputDir,
		Strategy:      ids.Strategy,
		Benchmark:     ids.Benchmark,
		Environment:   EnvironmentIdentity{GOOS: goruntime.GOOS, GOARCH: goruntime.GOARCH, GoVersion: goruntime.Version()},
		Summary:       newSummary(),
	}
	for _, casePath := range cases {
		caseID := caseIDs[casePath]
		for repeat := 1; repeat <= opts.Repeat; repeat++ {
			workspace := filepath.Join(paths.OutputDir, "workspaces", fmt.Sprintf("%s-%03d", caseID, repeat))
			record, runErr := runOne(ctx, paths, opts, casePath, caseID, repeat, workspace)
			record.Labels = cloneLabels(caseLabels[caseID])
			report.Runs = append(report.Runs, record)
			addSummary(&report.Summary, record)
			if runErr != nil && isInfrastructureError(runErr) {
				finishReport(report)
				_ = writeReport(paths.OutputDir, report)
				return report, runErr
			}
		}
	}
	finishReport(report)
	if err := writeReport(paths.OutputDir, report); err != nil {
		return report, err
	}
	return report, nil
}

type identities struct {
	Strategy  StrategyIdentity
	Benchmark BenchmarkIdentity
}

func buildIdentities(paths resolvedOptions, opts RunOptions, wf *spec.Workflow, cfg *spec.Config, cases []string, caseIDs map[string]string, caseManifestFingerprint string) (identities, error) {
	resolver := runtime.NewCommandResolver(paths.WorkflowPath, paths.WorkspaceTemplate, paths.WorkspaceTemplate)
	fingerprints, err := definition.Compute(wf, cfg, paths.WorkflowPath, paths.ConfigPath, resolver)
	if err != nil {
		return identities{}, err
	}
	strategyFingerprint, err := hashJSON(struct {
		Workflow string `json:"workflow"`
		Config   string `json:"config"`
		Commands string `json:"commands"`
	}{fingerprints.Workflow, fingerprints.Config, fingerprints.Commands})
	if err != nil {
		return identities{}, err
	}
	datasetFingerprint, err := hashCases(cases, caseIDs)
	if err != nil {
		return identities{}, err
	}
	workspaceFingerprint, err := hashWorkspaceTemplate(paths.WorkspaceTemplate)
	if err != nil {
		return identities{}, fmt.Errorf("fingerprint workspace template: %w", err)
	}
	validator, err := buildValidatorIdentity(paths, opts)
	if err != nil {
		return identities{}, err
	}
	protocol := ""
	if opts.QualityNode != "" {
		protocol = validation.ProtocolV1Alpha1
	}
	benchmarkFingerprint, err := hashJSON(struct {
		Dataset                 string `json:"dataset"`
		Workspace               string `json:"workspace"`
		QualityNode             string `json:"quality_node"`
		GenerationNode          string `json:"generation_node"`
		ValidationProtocol      string `json:"validation_protocol"`
		ValidatorID             string `json:"validator_id"`
		ValidatorVersion        string `json:"validator_version"`
		ValidatorFingerprint    string `json:"validator_fingerprint"`
		CaseManifestFingerprint string `json:"case_manifest_fingerprint"`
	}{datasetFingerprint, workspaceFingerprint, opts.QualityNode, opts.GenerationNode, protocol, validator.ID, validator.Version, validator.Fingerprint, caseManifestFingerprint})
	if err != nil {
		return identities{}, err
	}
	return identities{
		Strategy: StrategyIdentity{
			ID: opts.StrategyID, Fingerprint: strategyFingerprint,
			WorkflowFingerprint: fingerprints.Workflow,
			ConfigFingerprint:   fingerprints.Config,
			CommandsFingerprint: fingerprints.Commands,
			ModelPreset:         opts.ModelPreset, Models: modelReferences(cfg.Models),
		},
		Benchmark: BenchmarkIdentity{
			ID: opts.BenchmarkID, Fingerprint: benchmarkFingerprint,
			DatasetFingerprint: datasetFingerprint, WorkspaceFingerprint: workspaceFingerprint, CaseCount: len(cases),
			QualityNode: opts.QualityNode, GenerationNode: opts.GenerationNode,
			ValidationProtocol: protocol, Validator: validator, CaseManifestFingerprint: caseManifestFingerprint,
		},
	}, nil
}

func buildValidatorIdentity(paths resolvedOptions, opts RunOptions) (ValidatorIdentity, error) {
	identity := ValidatorIdentity{ID: opts.ValidatorID, Version: opts.ValidatorVersion}
	if opts.ValidatorPath == "" {
		return identity, nil
	}
	path := opts.ValidatorPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(paths.WorkspaceTemplate, path)
	}
	canonical, err := canonicalPath(path)
	if err != nil {
		return identity, fmt.Errorf("resolve validator path: %w", err)
	}
	if _, err := os.Stat(canonical); err != nil {
		return identity, fmt.Errorf("validator path: %w", err)
	}
	fingerprint, err := hashPath(canonical)
	if err != nil {
		return identity, fmt.Errorf("fingerprint validator: %w", err)
	}
	if identity.ID == "" {
		identity.ID = filepath.Base(canonical)
	}
	identity.Fingerprint = fingerprint
	if rel, err := filepath.Rel(paths.WorkspaceTemplate, canonical); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		identity.Path = filepath.ToSlash(rel)
	} else {
		identity.Path = canonical
	}
	return identity, nil
}

type resolvedOptions struct {
	WorkflowPath, ConfigPath, CasesDir, WorkspaceTemplate, OutputDir string
}

func resolveOptions(opts RunOptions) (resolvedOptions, error) {
	values := []struct {
		name  string
		value string
	}{
		{"workflow", opts.WorkflowPath}, {"config", opts.ConfigPath}, {"cases", opts.CasesDir},
		{"workspace template", opts.WorkspaceTemplate}, {"output", opts.OutputDir},
	}
	out := resolvedOptions{}
	resolved := []*string{&out.WorkflowPath, &out.ConfigPath, &out.CasesDir, &out.WorkspaceTemplate, &out.OutputDir}
	for i, item := range values {
		if strings.TrimSpace(item.value) == "" {
			return out, fmt.Errorf("%s path is required", item.name)
		}
		abs, err := filepath.Abs(item.value)
		if err != nil {
			return out, err
		}
		canonical, err := canonicalPath(abs)
		if err != nil {
			return out, fmt.Errorf("resolve %s path: %w", item.name, err)
		}
		*resolved[i] = canonical
	}
	if pathsOverlap(out.WorkspaceTemplate, out.OutputDir) {
		return out, fmt.Errorf("workspace template and output directories must not overlap: template=%s output=%s", out.WorkspaceTemplate, out.OutputDir)
	}
	return out, nil
}

func resolveCaseIDs(paths []string) (map[string]string, error) {
	ids := make(map[string]string, len(paths))
	owners := make(map[string]string, len(paths))
	for _, path := range paths {
		id := sanitizeCaseID(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
		if previous, exists := owners[id]; exists {
			return nil, fmt.Errorf("evaluation case id collision %q after normalization: %s and %s", id, filepath.Base(previous), filepath.Base(path))
		}
		owners[id] = path
		ids[path] = id
	}
	return ids, nil
}

func canonicalPath(path string) (string, error) {
	path = filepath.Clean(path)
	current := path
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path, nil
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func pathsOverlap(first, second string) bool {
	return pathContains(first, second) || pathContains(second, first)
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func listCases(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

func runOne(ctx context.Context, paths resolvedOptions, opts RunOptions, casePath, caseID string, repeat int, workspacePath string) (RunRecord, error) {
	record := RunRecord{CaseID: caseID, Repeat: repeat, Workspace: workspacePath, Status: "not_started", Nodes: map[string]NodeRecord{}}
	if _, err := os.Stat(workspacePath); err == nil {
		if !opts.Replace {
			record.Status = "infrastructure_error"
			record.Error = "workspace already exists; use --replace"
			return record, fmt.Errorf("workspace %s already exists", workspacePath)
		}
		if err := os.RemoveAll(workspacePath); err != nil {
			return record, err
		}
	} else if !os.IsNotExist(err) {
		return record, err
	}
	if err := copyTree(paths.WorkspaceTemplate, workspacePath); err != nil {
		record.Status, record.Error = "infrastructure_error", err.Error()
		return record, err
	}
	input, err := os.ReadFile(casePath)
	if err != nil {
		record.Status, record.Error = "infrastructure_error", err.Error()
		return record, err
	}
	wf, err := workflow.Load(paths.WorkflowPath)
	if err != nil {
		record.Status, record.Error = "infrastructure_error", err.Error()
		return record, err
	}
	cfg, err := cfgpkg.Load(paths.ConfigPath)
	if err != nil {
		record.Status, record.Error = "infrastructure_error", err.Error()
		return record, err
	}
	cfg, _, err = cfgpkg.MaterializeModels(cfg, cfgpkg.ModelSelection{Preset: opts.ModelPreset, Overrides: opts.ModelOverrides})
	if err != nil {
		record.Status, record.Error = "infrastructure_error", err.Error()
		return record, err
	}
	if opts.ExecutionFactory == nil {
		record.Status, record.Error = "infrastructure_error", "evaluation execution factory is required"
		return record, fmt.Errorf("evaluation execution factory is required")
	}
	execution, err := opts.ExecutionFactory(wf, cfg, paths.WorkflowPath, paths.ConfigPath, workspacePath)
	if err != nil {
		record.Status, record.Error = "infrastructure_error", err.Error()
		return record, err
	}
	runner := execution.Runner
	repo := execution.Store
	if runner == nil || repo == nil {
		record.Status, record.Error = "infrastructure_error", "evaluation execution factory returned incomplete dependencies"
		return record, fmt.Errorf("evaluation execution factory returned incomplete dependencies")
	}
	state, runErr := runner.Start(ctx, string(input))
	for errors.Is(runErr, runtime.ErrWaiting) && opts.ApprovalAnswer != "" {
		if state.Waiting == nil {
			return record, fmt.Errorf("run %s returned waiting without waiting state", state.ID)
		}
		nodeID := state.Waiting.NodeID
		state.Approvals[nodeID] = opts.ApprovalAnswer
		if node := state.Nodes[nodeID]; node != nil {
			node.Status = store.NodePending
		}
		state.Status = store.RunRunning
		state.Waiting = nil
		if err := commitEvaluationState(repo, state, store.Event{Type: "approval.answered", NodeID: nodeID, Data: map[string]any{"value_captured": true, "source": "evaluation"}}, cfg); err != nil {
			return record, err
		}
		state, runErr = runner.Resume(ctx, state)
	}
	if state != nil {
		record = recordFromState(caseID, repeat, workspacePath, state)
		record.QualityExpected = opts.QualityNode != ""
		if opts.QualityNode != "" {
			if err := applyQuality(&record, state, opts.QualityNode, opts.GenerationNode); err != nil {
				record.Status = "evaluation_error"
				record.ErrorCode = "quality_contract"
				record.Error = err.Error()
				return record, err
			}
		}
		applyRuntimeMetrics(&record, state, repo, opts.QualityNode)
	}
	if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) {
		if record.Error == "" {
			record.Error = runErr.Error()
		}
		return record, runErr
	}
	return record, nil
}

func commitEvaluationState(repo store.Repository, state *store.RunState, event store.Event, cfg *spec.Config) error {
	r := redact.NewFromConfig(cfg)
	persisted, err := redact.CloneRunState(state)
	if err != nil {
		return err
	}
	redact.RedactRunState(r, persisted)
	event.Data = redact.EventData(r, event.Data)
	if err := repo.Commit(persisted, event); err != nil {
		return err
	}
	state.Revision = persisted.Revision
	state.UpdatedAt = persisted.UpdatedAt
	return nil
}

func applyQuality(record *RunRecord, state *store.RunState, qualityNode, generationNode string) error {
	record.AttemptsToValid = intPointer(0)
	node := state.Nodes[qualityNode]
	if node == nil {
		return fmt.Errorf("quality node %q has no runtime state", qualityNode)
	}
	record.QualityNodeStatus = string(node.Status)

	stdout := strings.TrimSpace(node.Stdout)
	if stdout != "" {
		result, err := validation.Decode([]byte(node.Stdout))
		if err != nil {
			return fmt.Errorf("quality node %q: %w", qualityNode, err)
		}
		record.Quality = result
	} else if node.Status == store.NodeCompleted {
		return fmt.Errorf("quality node %q produced no validation result", qualityNode)
	}

	if node.Status != store.NodeCompleted {
		record.QualityError = fmt.Sprintf("quality node %q did not complete: status=%s", qualityNode, node.Status)
		if node.ErrorCode != "" {
			record.QualityError += " error_code=" + node.ErrorCode
		}
		if node.Error != "" {
			record.QualityError += " error=" + node.Error
		}
		return nil
	}

	if record.Quality == nil {
		return fmt.Errorf("quality node %q produced no validation result", qualityNode)
	}
	generator := state.Nodes[generationNode]
	if generator == nil {
		return fmt.Errorf("generation node %q has no runtime state", generationNode)
	}
	if record.Quality.Valid {
		record.AttemptsToValid = intPointer(generator.Attempts)
		record.ValidAtFirstAttempt = generator.Attempts == 1
	}
	return nil
}

func recordFromState(caseID string, repeat int, workspacePath string, state *store.RunState) RunRecord {
	record := RunRecord{
		CaseID: caseID, Repeat: repeat, RunID: state.ID, Status: state.Status, Workspace: workspacePath,
		DurationMS: state.UpdatedAt.Sub(state.CreatedAt).Milliseconds(), Answers: len(state.Approvals),
		ErrorCode: state.ErrorCode, Error: state.Error, Nodes: map[string]NodeRecord{},
	}
	for id, node := range state.Nodes {
		if node == nil {
			continue
		}
		record.Attempts += node.Attempts
		record.ProviderAttempts += node.ProviderAttempts
		if node.OutputTruncated {
			record.Truncated++
		}
		if node.Resumed {
			record.Resumed++
		}
		if node.Usage != nil {
			record.InputTokens += node.Usage.InputTokens
			record.OutputTokens += node.Usage.OutputTokens
			record.Cost += node.Usage.Cost
		}
		executions := make([]ExecutionRecord, 0, len(node.Executions))
		identities := map[string]struct{}{}
		for _, executionState := range node.Executions {
			executionRecord := executionRecordFromState(executionState)
			executions = append(executions, executionRecord)
			if key := executionIdentityKey(executionRecord.Assistant, executionRecord.AssistantVersion, executionRecord.RequestedModel, executionRecord.ResolvedModel); key != "" {
				identities[key] = struct{}{}
			}
		}
		mixedIdentity := len(identities) > 1
		if mixedIdentity {
			record.MixedIdentityNodes++
		}
		record.Nodes[id] = NodeRecord{
			Status: node.Status, Attempts: node.Attempts, ProviderAttempts: node.ProviderAttempts, Assistant: node.Assistant, AssistantVersion: node.AssistantVersion,
			RequestedModel: node.RequestedModel, ResolvedModel: node.ResolvedModel,
			SessionID: node.SessionID, Resumed: node.Resumed,
			ExitCode: node.ExitCode, ErrorCode: node.ErrorCode, Error: node.Error, Feedback: node.Feedback,
			DiagnosticOutput: node.Output, Stdout: node.Stdout, Stderr: node.Stderr,
			OutputTruncated: node.OutputTruncated, Usage: node.Usage, Diagnostic: node.Diagnostic,
			MixedIdentity: mixedIdentity, Executions: executions,
		}
	}
	return record
}

func executionRecordFromState(state store.ExecutionState) ExecutionRecord {
	return ExecutionRecord{
		Attempt: state.Attempt, ProviderAttempt: state.ProviderAttempt, Status: state.Status,
		Assistant: state.Assistant, AssistantVersion: state.AssistantVersion,
		RequestedModel: state.RequestedModel, ResolvedModel: state.ResolvedModel,
		SessionID: state.SessionID, Resumed: state.Resumed,
		ExitCode: state.ExitCode, ErrorCode: state.ErrorCode, Error: state.Error,
		OutputTruncated: state.OutputTruncated, Usage: state.Usage, Diagnostic: state.Diagnostic,
	}
}

func newSummary() Summary {
	return Summary{
		ByStatus: map[string]int{}, ByAssistant: map[string]int{}, ByAssistantVersion: map[string]int{},
		ByRequestedModel: map[string]int{}, ByResolvedModel: map[string]int{},
		UsageByExecutionIdentity: map[string]UsageBreakdown{},
		DiagnosticsBySeverity:    map[string]int{}, DiagnosticsByCode: map[string]int{}, DiagnosticsByFingerprint: map[string]int{},
	}
}

func addSummary(summary *Summary, record RunRecord) {
	summary.Total++
	summary.ByStatus[record.Status]++
	summary.Attempts += record.Attempts
	summary.InputTokens += record.InputTokens
	summary.OutputTokens += record.OutputTokens
	summary.Cost += record.Cost
	summary.DurationMS += record.DurationMS
	summary.Answers += record.Answers
	summary.Truncated += record.Truncated
	summary.Resumed += record.Resumed
	summary.MixedExecutionIdentityNodes += record.MixedIdentityNodes
	summary.RetryScheduled += record.RetryScheduled
	for _, fingerprint := range record.RetryFingerprints {
		if fingerprint != "" {
			summary.DiagnosticsByFingerprint[fingerprint]++
		}
	}
	for _, node := range record.Nodes {
		if len(node.Executions) == 0 {
			if node.Diagnostic != nil && node.Diagnostic.Fingerprint != "" {
				summary.DiagnosticsByFingerprint[node.Diagnostic.Fingerprint]++
			}
			addExecutionIdentitySummary(summary, ExecutionRecord{
				Assistant: node.Assistant, AssistantVersion: node.AssistantVersion,
				RequestedModel: node.RequestedModel, ResolvedModel: node.ResolvedModel, Usage: node.Usage,
			})
			continue
		}
		for _, executionRecord := range node.Executions {
			addExecutionIdentitySummary(summary, executionRecord)
			if executionRecord.Diagnostic != nil && executionRecord.Diagnostic.Fingerprint != "" {
				summary.DiagnosticsByFingerprint[executionRecord.Diagnostic.Fingerprint]++
			}
			if executionRecord.Status != string(store.NodeCompleted) && executionRecord.Status != "completed" {
				summary.FailedExecutions++
				if executionRecord.Usage != nil {
					summary.FailedExecutionCost += executionRecord.Usage.Cost
				}
			}
		}
	}
	if record.Mode == "flow" {
		addFlowSummary(summary, record)
	}
	if isProviderUnavailableRecord(record) {
		summary.InfrastructureErrors++
		addValidationDiagnostics(summary, record)
		return
	}
	if record.Mode == "flow" {
		if record.Validation == nil || record.Validation.Status != "completed" || record.Validation.Result == nil {
			return
		}
		record.QualityExpected = true
		record.Quality = record.Validation.Result
	}
	if !record.QualityExpected {
		return
	}
	summary.QualityRuns++
	if qualitySucceeded(record) {
		summary.Valid++
		if record.ValidAtFirstAttempt {
			summary.ValidAtFirstAttempt++
		}
	} else {
		summary.Invalid++
	}
	addValidationDiagnostics(summary, record)
}

func addValidationDiagnostics(summary *Summary, record RunRecord) {
	result := record.Quality
	if result == nil && record.Validation != nil {
		result = record.Validation.Result
	}
	if result == nil {
		return
	}
	for _, diagnostic := range result.Diagnostics {
		summary.DiagnosticsBySeverity[diagnostic.Severity]++
		summary.DiagnosticsByCode[diagnostic.Code]++
	}
}

func qualitySucceeded(record RunRecord) bool {
	if record.Mode == "flow" {
		return record.Validation != nil && record.Validation.Status == "completed" && record.Validation.Result != nil && record.Validation.Result.Valid
	}
	return record.QualityNodeStatus == string(store.NodeCompleted) && record.Quality != nil && record.Quality.Valid
}

func finishReport(report *SuiteReport) {
	report.FinishedAt = time.Now().UTC()
	report.DurationMS = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	if report.Mode == "flow" {
		finishFlowReport(report)
		return
	}
	if report.Summary.QualityRuns == 0 {
		return
	}
	report.Summary.SuccessAt1 = floatPointer(float64(report.Summary.ValidAtFirstAttempt) / float64(report.Summary.QualityRuns))
	report.Summary.FinalSuccessRate = floatPointer(float64(report.Summary.Valid) / float64(report.Summary.QualityRuns))
	var attemptsToValid, scored int
	var scoreTotal float64
	for _, record := range report.Runs {
		if isProviderUnavailableRecord(record) {
			continue
		}
		if record.Quality == nil {
			continue
		}
		if record.Quality.Score != nil {
			scored++
			scoreTotal += *record.Quality.Score
		}
		if qualitySucceeded(record) {
			if record.AttemptsToValid != nil {
				attemptsToValid += *record.AttemptsToValid
			}
		}
	}
	if report.Summary.Valid > 0 {
		report.Summary.AverageAttemptsToValid = floatPointer(float64(attemptsToValid) / float64(report.Summary.Valid))
		report.Summary.CostPerValid = floatPointer(report.Summary.Cost / float64(report.Summary.Valid))
		report.Summary.AmortizedEndToEndMSPerValid = floatPointer(float64(report.Summary.DurationMS) / float64(report.Summary.Valid))
	}
	if scored > 0 {
		report.Summary.ScoredRuns = scored
		report.Summary.AverageScore = floatPointer(scoreTotal / float64(scored))
	}
	var timeCount int
	var timeTotal int64
	caseOutcomes := map[string]map[bool]bool{}
	for _, record := range report.Runs {
		if isProviderUnavailableRecord(record) {
			continue
		}
		if qualitySucceeded(record) && record.TimeToValidMS != nil {
			timeCount++
			timeTotal += *record.TimeToValidMS
		}
		if record.QualityExpected {
			set := caseOutcomes[record.CaseID]
			if set == nil {
				set = map[bool]bool{}
				caseOutcomes[record.CaseID] = set
			}
			set[qualitySucceeded(record)] = true
		}
	}
	if timeCount > 0 {
		report.Summary.AverageTimeToValidMS = floatPointer(float64(timeTotal) / float64(timeCount))
	}
	for _, values := range caseOutcomes {
		switch {
		case len(values) > 1:
			report.Summary.UnstableCases++
		case values[true]:
			report.Summary.StableValidCases++
		default:
			report.Summary.StableInvalidCases++
		}
	}
}

func addFlowSummary(summary *Summary, record RunRecord) {
	if summary.Flow == nil {
		summary.Flow = &FlowSummary{}
	}
	flow := summary.Flow
	if record.Status == store.RunCompleted {
		flow.FlowCompleted++
	}
	if isProviderUnavailableRecord(record) {
		flow.InfrastructureErrors++
		return
	}
	if record.Validation == nil || record.Validation.Status != "completed" || record.Validation.Result == nil {
		flow.ValidationErrors++
		return
	}
	flow.EvaluatedRuns++
	switch record.Outcome {
	case "true_accept":
		flow.TrueAccept++
	case "false_accept":
		flow.FalseAccept++
	case "true_reject":
		flow.TrueReject++
	case "false_reject":
		flow.FalseReject++
	}
}

func finishFlowReport(report *SuiteReport) {
	report.Summary.StableValidCases = 0
	report.Summary.StableInvalidCases = 0
	report.Summary.UnstableCases = 0
	flow := report.Summary.Flow
	if flow == nil {
		flow = &FlowSummary{}
		report.Summary.Flow = flow
	}
	if flow.EvaluatedRuns > 0 {
		flow.ValidRate = floatPointer(float64(flow.TrueAccept) / float64(flow.EvaluatedRuns))
		flow.FalseAcceptRate = floatPointer(float64(flow.FalseAccept) / float64(flow.EvaluatedRuns))
		flow.FalseRejectRate = floatPointer(float64(flow.FalseReject) / float64(flow.EvaluatedRuns))
		report.Summary.FinalSuccessRate = flow.ValidRate
	}
	if report.Summary.Total > 0 {
		flow.FlowCompletionRate = floatPointer(float64(flow.FlowCompleted) / float64(report.Summary.Total))
		flow.ValidationErrorRate = floatPointer(float64(flow.ValidationErrors) / float64(report.Summary.Total))
	}
	var scored int
	var scoreTotal float64
	for _, record := range report.Runs {
		if isProviderUnavailableRecord(record) {
			continue
		}
		if record.Quality != nil && record.Quality.Score != nil {
			scored++
			scoreTotal += *record.Quality.Score
		}
	}
	if report.Summary.Valid > 0 {
		report.Summary.CostPerValid = floatPointer(report.Summary.Cost / float64(report.Summary.Valid))
		report.Summary.AmortizedEndToEndMSPerValid = floatPointer(float64(report.Summary.DurationMS) / float64(report.Summary.Valid))
	}
	if scored > 0 {
		report.Summary.ScoredRuns = scored
		report.Summary.AverageScore = floatPointer(scoreTotal / float64(scored))
	}
	var timeTotal int64
	var timeCount int
	caseOutcomes := map[string]map[bool]bool{}
	for _, record := range report.Runs {
		if isProviderUnavailableRecord(record) {
			continue
		}
		if record.Mode != "flow" || record.Validation == nil || record.Validation.Status != "completed" || record.Validation.Result == nil {
			continue
		}
		if qualitySucceeded(record) && record.TimeToValidMS != nil {
			timeTotal += *record.TimeToValidMS
			timeCount++
		}
		set := caseOutcomes[record.CaseID]
		if set == nil {
			set = map[bool]bool{}
			caseOutcomes[record.CaseID] = set
		}
		set[qualitySucceeded(record)] = true
	}
	if timeCount > 0 {
		report.Summary.AverageTimeToValidMS = floatPointer(float64(timeTotal) / float64(timeCount))
	}
	for _, values := range caseOutcomes {
		switch {
		case len(values) > 1:
			report.Summary.UnstableCases++
		case values[true]:
			report.Summary.StableValidCases++
		default:
			report.Summary.StableInvalidCases++
		}
	}
}

func intPointer(value int) *int { return &value }

func addExecutionIdentitySummary(summary *Summary, executionRecord ExecutionRecord) {
	if executionRecord.Assistant != "" {
		summary.ByAssistant[executionRecord.Assistant]++
	}
	if executionRecord.AssistantVersion != "" {
		summary.ByAssistantVersion[executionRecord.AssistantVersion]++
	}
	if key := modelKey(executionRecord.RequestedModel); key != "" {
		summary.ByRequestedModel[key]++
	}
	if key := modelKey(executionRecord.ResolvedModel); key != "" {
		summary.ByResolvedModel[key]++
	}
	if executionRecord.Usage == nil {
		return
	}
	key := executionIdentityKey(executionRecord.Assistant, executionRecord.AssistantVersion, executionRecord.RequestedModel, executionRecord.ResolvedModel)
	if key == "" {
		key = "unknown"
	}
	usage := summary.UsageByExecutionIdentity[key]
	usage.Executions++
	usage.InputTokens += executionRecord.Usage.InputTokens
	usage.OutputTokens += executionRecord.Usage.OutputTokens
	usage.Cost += executionRecord.Usage.Cost
	summary.UsageByExecutionIdentity[key] = usage
}

func executionIdentityKey(assistantName, assistantVersion string, requested, resolved *store.ModelRef) string {
	if assistantName == "" && assistantVersion == "" && requested == nil && resolved == nil {
		return ""
	}
	return strings.Join([]string{
		"assistant=" + assistantName,
		"version=" + assistantVersion,
		"requested=" + modelKey(requested),
		"resolved=" + modelKey(resolved),
	}, "|")
}

func floatPointer(value float64) *float64 { return &value }

func modelKey(model *store.ModelRef) string {
	if model == nil {
		return ""
	}
	identity := model.ID
	if model.Provider != "" {
		identity = model.Provider + "/" + identity
	}
	if model.Name != "" {
		identity = model.Name + "=" + identity
	}
	return identity
}

func writeReport(outputDir string, report *SuiteReport) error {
	return writeJSONAtomic(filepath.Join(outputDir, "report.json"), report)
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if skipWorkspacePath(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func sanitizeCaseID(value string) string {
	value = safeCaseID.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")
	if value == "" {
		return "case"
	}
	return value
}

func workflowHasNode(nodes []spec.Node, id string) bool {
	for _, node := range nodes {
		if node.ID == id {
			return true
		}
		if node.LoopGroup != nil && workflowHasNode(node.LoopGroup.Nodes, id) {
			return true
		}
	}
	return false
}

func firstAssistantNode(nodes []spec.Node) string {
	for _, node := range nodes {
		if node.Command != "" || node.Prompt != "" {
			return node.ID
		}
		if node.LoopGroup != nil {
			if id := firstAssistantNode(node.LoopGroup.Nodes); id != "" {
				return id
			}
		}
	}
	return ""
}

func hashCases(paths []string, caseIDs map[string]string) (string, error) {
	h := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		_, _ = h.Write([]byte(caseIDs[path]))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return hashFiles(path, []string{path}, false)
	}
	files, err := collectFiles(path, false)
	if err != nil {
		return "", err
	}
	return hashFiles(path, files, true)
}

func hashWorkspaceTemplate(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace template is not a directory: %s", path)
	}
	files, err := collectFiles(path, true)
	if err != nil {
		return "", err
	}
	return hashFiles(path, files, true)
}

func collectFiles(root string, workspaceFilter bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if workspaceFilter && skipWorkspacePath(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			files = append(files, current)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func hashFiles(root string, files []string, useRelativePath bool) (string, error) {
	h := sha256.New()
	for _, file := range files {
		name := filepath.Base(file)
		if useRelativePath {
			rel, err := filepath.Rel(root, file)
			if err != nil {
				return "", err
			}
			name = filepath.ToSlash(rel)
		}
		info, err := os.Stat(file)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		_, _ = h.Write([]byte(name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(fmt.Sprintf("%04o", info.Mode().Perm())))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func skipWorkspacePath(rel string) bool {
	return rel == ".takt" || strings.HasPrefix(rel, ".takt"+string(filepath.Separator)) ||
		rel == "bin" || strings.HasPrefix(rel, "bin"+string(filepath.Separator))
}

func hashJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func isInfrastructureError(err error) bool {
	if err == nil || errors.Is(err, runtime.ErrWaiting) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var runFailed *runtime.RunFailedError
	return !errors.As(err, &runFailed)
}

func LoadReport(outputDir string) (*SuiteReport, error) {
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(abs, "report.json"))
	if err != nil {
		return nil, err
	}
	var report SuiteReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("decode evaluation report: %w", err)
	}
	return &report, nil
}
