package evaluation

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"takt/internal/config"
	"takt/internal/profile"
)

const EvaluationInputProtocol = "takt-evaluation-input/v1alpha1"
const EvaluationInputType = "evaluation_input"

type EvaluationGate struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

type EvaluationCaseInput struct {
	CaseID              string `json:"case_id"`
	Repeat              int    `json:"repeat"`
	Input               string `json:"input"`
	InputPath           string `json:"input_path"`
	ExpectedPath        string `json:"expected_path"`
	BaselinePath        string `json:"baseline_path"`
	Repository          string `json:"repository"`
	WorkflowPath        string `json:"workflow_path"`
	CaseFingerprint     string `json:"case_fingerprint"`
	WorkflowFingerprint string `json:"workflow_fingerprint"`
	PreparedFingerprint string `json:"prepared_fingerprint"`
}

type EvaluationInputIdentity struct {
	Fingerprint         string            `json:"fingerprint"`
	WorkflowFingerprint string            `json:"workflow_fingerprint"`
	ConfigFingerprint   string            `json:"config_fingerprint"`
	DatasetFingerprint  string            `json:"dataset_fingerprint"`
	Target              string            `json:"target"`
	ApprovalAnswer      string            `json:"approval_answer,omitempty"`
	ModelPreset         string            `json:"model_preset,omitempty"`
	Models              map[string]string `json:"models,omitempty"`
}

type EvaluationInput struct {
	ProtocolVersion string                    `json:"protocol_version"`
	Type            string                    `json:"type"`
	Cases           []EvaluationCaseInput     `json:"cases"`
	Gates           map[string]EvaluationGate `json:"gates"`
	Identity        EvaluationInputIdentity   `json:"identity"`
}

type EvaluationInputOptions struct {
	WorkflowPath   string
	Target         string
	ApprovalAnswer string
	ConfigPath     string
	CasesDir       string
	CaseID         string
	OutputDir      string
	Workspace      string
	Repeat         int
	Gates          map[string]EvaluationGate
	ModelPreset    string
	ModelOverrides map[string]string
	Now            func() time.Time
	HostPATH       string
}

type PreparedEvaluationInput struct {
	Input       EvaluationInput
	JSON        []byte
	ConfigPath  string
	ModelPreset string
	OutputDir   string
}

func PrepareEvaluationInput(ctx context.Context, opts EvaluationInputOptions) (*PreparedEvaluationInput, error) {
	if strings.TrimSpace(opts.WorkflowPath) == "" || strings.TrimSpace(opts.Target) == "" || strings.TrimSpace(opts.ConfigPath) == "" || strings.TrimSpace(opts.CasesDir) == "" {
		return nil, fmt.Errorf("ordinary evaluation workflow requires --target, --config, and --cases")
	}
	if opts.Repeat <= 0 {
		return nil, fmt.Errorf("repeat must be positive")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	workspace, err := filepath.Abs(opts.Workspace)
	if err != nil {
		return nil, err
	}
	workspace, err = canonicalPath(workspace)
	if err != nil {
		return nil, err
	}
	workflowPath, err := absoluteRegular(opts.WorkflowPath, workspace)
	if err != nil {
		return nil, fmt.Errorf("evaluation workflow: %w", err)
	}
	configPath, err := absoluteRegular(opts.ConfigPath, workspace)
	if err != nil {
		return nil, fmt.Errorf("evaluation config: %w", err)
	}
	casesDir, err := absoluteDirectory(opts.CasesDir, workspace)
	if err != nil {
		return nil, fmt.Errorf("evaluation cases: %w", err)
	}
	output, err := evaluationOutputPath(opts.OutputDir, workspace, workflowPath, opts.Now())
	if err != nil {
		return nil, err
	}
	if !pathInside(workspace, output) || output == workspace {
		return nil, fmt.Errorf("evaluation output must be inside invocation workspace")
	}
	suite := &FlowSuite{
		Workflow: opts.Target, Config: configPath, Cases: FlowCasesSpec{Directory: casesDir},
		SuitePath: workflowPath, SuiteDir: filepath.Dir(workflowPath), ResolvedConfig: configPath, ResolvedCases: casesDir,
	}
	if err := validateFlowOutput(output, suite, workspace); err != nil {
		return nil, err
	}
	cases, err := DiscoverFlowCases(workflowPath, suite, opts.CaseID)
	if err != nil {
		return nil, err
	}
	suite.External, err = evaluationExternal(cases)
	if err != nil {
		return nil, err
	}
	if err := validateEvaluationGates(opts.Gates); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return nil, err
	}

	items := make([]EvaluationCaseInput, 0, len(cases)*opts.Repeat)
	configFingerprint := ""
	preparedConfigPath := ""
	selectedPreset := ""
	var effectiveModels map[string]string
	for _, item := range cases {
		for repeat := 1; repeat <= opts.Repeat; repeat++ {
			prepared, prepErr := PrepareFlowRepeat(ctx, suite, item, repeat, output, opts.HostPATH, config.ModelSelection{Preset: opts.ModelPreset, Overrides: opts.ModelOverrides})
			if prepErr != nil {
				return nil, prepErr
			}
			workflow, resolveErr := preparedEvaluationWorkflow(opts.Target, prepared.ControlWorkspace)
			if resolveErr != nil {
				return nil, resolveErr
			}
			input, resolveErr := preparedEvaluationCaseInput(opts.Target, prepared.ControlWorkspace, prepared.InputValue)
			if resolveErr != nil {
				return nil, resolveErr
			}
			repository, relErr := filepath.Rel(workspace, prepared.ControlWorkspace)
			if relErr != nil || repository == ".." || strings.HasPrefix(repository, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("prepared repository escapes invocation workspace")
			}
			workflowFingerprint, hashErr := hashPath(workflow)
			if hashErr != nil {
				return nil, hashErr
			}
			currentConfigFingerprint, hashErr := hashPath(prepared.ConfigPath)
			if hashErr != nil {
				return nil, hashErr
			}
			if configFingerprint == "" {
				configFingerprint, preparedConfigPath, selectedPreset, effectiveModels = currentConfigFingerprint, prepared.ConfigPath, prepared.ModelPreset, prepared.EffectiveModels
			} else if configFingerprint != currentConfigFingerprint || selectedPreset != prepared.ModelPreset {
				return nil, fmt.Errorf("prepared configuration identity drift")
			}
			preparedFingerprint, hashErr := hashJSON(struct {
				Case, Workflow, Config, Base, Head, Preset string
				Models                                     map[string]string
				Repeat                                     int
			}{item.Fingerprint, workflowFingerprint, currentConfigFingerprint, prepared.BaseCommit, prepared.HeadCommit, prepared.ModelPreset, prepared.EffectiveModels, repeat})
			if hashErr != nil {
				return nil, hashErr
			}
			items = append(items, EvaluationCaseInput{
				CaseID: item.ID, Repeat: repeat, Input: input, InputPath: item.InputPath,
				ExpectedPath: item.ExpectedPath, BaselinePath: prepared.BaselineWorkspace,
				Repository: filepath.ToSlash(repository), WorkflowPath: workflow,
				CaseFingerprint: item.Fingerprint, WorkflowFingerprint: workflowFingerprint, PreparedFingerprint: preparedFingerprint,
			})
		}
	}
	workflowFingerprint, err := hashPath(workflowPath)
	if err != nil {
		return nil, err
	}
	datasetFingerprint := flowDatasetFingerprint(suite, cases)
	identity := EvaluationInputIdentity{WorkflowFingerprint: workflowFingerprint, ConfigFingerprint: configFingerprint, DatasetFingerprint: datasetFingerprint, Target: opts.Target, ApprovalAnswer: opts.ApprovalAnswer, ModelPreset: selectedPreset, Models: effectiveModels}
	type caseIdentity struct {
		CaseID, Case, Workflow, Prepared string
		Repeat                           int
	}
	caseIdentities := make([]caseIdentity, len(items))
	for index, item := range items {
		caseIdentities[index] = caseIdentity{item.CaseID, item.CaseFingerprint, item.WorkflowFingerprint, item.PreparedFingerprint, item.Repeat}
	}
	identity.Fingerprint, err = hashJSON(struct {
		Workflow, Config, Dataset, Target, Approval, Preset string
		Models                                              map[string]string
		Cases                                               []caseIdentity
		Gates                                               map[string]EvaluationGate
	}{workflowFingerprint, configFingerprint, datasetFingerprint, opts.Target, opts.ApprovalAnswer, selectedPreset, effectiveModels, caseIdentities, opts.Gates})
	if err != nil {
		return nil, err
	}
	input := EvaluationInput{ProtocolVersion: EvaluationInputProtocol, Type: EvaluationInputType, Cases: items, Gates: cloneEvaluationGates(opts.Gates), Identity: identity}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if _, err := DecodeEvaluationInput(raw); err != nil {
		return nil, err
	}
	return &PreparedEvaluationInput{Input: input, JSON: raw, ConfigPath: preparedConfigPath, ModelPreset: selectedPreset, OutputDir: output}, nil
}

func preparedEvaluationCaseInput(target, workspace, inputPath string) (string, error) {
	if !filepath.IsAbs(target) {
		if info, err := os.Stat(filepath.Join(workspace, target)); err == nil && info.Mode().IsRegular() {
			raw, err := os.ReadFile(inputPath)
			return string(raw), err
		}
	}
	resolved, err := profile.Resolve(target, workspace)
	if err != nil {
		return "", err
	}
	return profile.PrepareInput(resolved.EffectiveInput(), inputPath)
}

func DecodeEvaluationInput(raw []byte) (*EvaluationInput, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input EvaluationInput
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("decode evaluation input: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode evaluation input: trailing JSON value")
	}
	if input.ProtocolVersion != EvaluationInputProtocol || input.Type != EvaluationInputType {
		return nil, fmt.Errorf("unsupported evaluation input")
	}
	if len(input.Cases) == 0 {
		return nil, fmt.Errorf("evaluation input cases are required")
	}
	if !validEvaluationFingerprint(input.Identity.Fingerprint) || !validEvaluationFingerprint(input.Identity.WorkflowFingerprint) || !validEvaluationFingerprint(input.Identity.ConfigFingerprint) || !validEvaluationFingerprint(input.Identity.DatasetFingerprint) || input.Identity.Target == "" {
		return nil, fmt.Errorf("evaluation input identity is incomplete")
	}
	if err := validateEvaluationGates(input.Gates); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, item := range input.Cases {
		key := fmt.Sprintf("%s\x00%d", item.CaseID, item.Repeat)
		if strings.TrimSpace(item.CaseID) == "" || item.Repeat <= 0 || seen[key] || item.Input == "" || item.Repository == "" || item.WorkflowPath == "" || !validEvaluationFingerprint(item.CaseFingerprint) || !validEvaluationFingerprint(item.WorkflowFingerprint) || !validEvaluationFingerprint(item.PreparedFingerprint) {
			return nil, fmt.Errorf("invalid evaluation case %q repeat %d", item.CaseID, item.Repeat)
		}
		seen[key] = true
		for name, path := range map[string]string{"input_path": item.InputPath, "expected_path": item.ExpectedPath, "baseline_path": item.BaselinePath, "workflow_path": item.WorkflowPath} {
			if !filepath.IsAbs(path) {
				return nil, fmt.Errorf("evaluation case %s must be absolute", name)
			}
		}
		cleanRepository := filepath.Clean(filepath.FromSlash(item.Repository))
		if filepath.IsAbs(cleanRepository) || cleanRepository == ".." || strings.HasPrefix(cleanRepository, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("evaluation case repository must be relative")
		}
	}
	return &input, nil
}

func validEvaluationFingerprint(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

func preparedEvaluationWorkflow(target, workspace string) (string, error) {
	candidate := target
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(workspace, candidate)
	}
	if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
		candidate, err = filepath.Abs(candidate)
		if err != nil || !pathInside(workspace, candidate) {
			return "", fmt.Errorf("target workflow escapes prepared repository")
		}
		return candidate, nil
	}
	resolved, err := profile.Resolve(target, workspace)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(resolved.WorkflowPath)
	if err != nil || !pathInside(workspace, path) {
		return "", fmt.Errorf("target workflow escapes prepared repository")
	}
	return path, nil
}

func evaluationExternal(cases []FlowCase) (FlowExternalSpec, error) {
	requirement := ""
	initialized := false
	for _, item := range cases {
		current := ""
		if item.SCMPath != "" {
			current = "repository"
			if fileRegular(filepath.Join(item.SCMPath, "pull-request.yaml")) {
				current = "pull_request"
			}
		}
		if !initialized {
			requirement = current
			initialized = true
		} else if current != requirement {
			return FlowExternalSpec{}, fmt.Errorf("evaluation cases use inconsistent SCM fixtures")
		}
	}
	if requirement == "" {
		return FlowExternalSpec{}, nil
	}
	return FlowExternalSpec{GitHub: &FlowGitHubSpec{Mode: "fixture", Require: requirement}}, nil
}

func evaluationOutputPath(value, workspace, workflow string, now time.Time) (string, error) {
	if value == "" {
		name := strings.TrimSuffix(filepath.Base(workflow), filepath.Ext(workflow))
		value = filepath.Join(workspace, ".takt", "evals", name, now.UTC().Format("20060102T150405.000000000Z"))
	} else if !filepath.IsAbs(value) {
		value = filepath.Join(workspace, value)
	}
	value, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return canonicalPath(value)
}

func absoluteRegular(value, workspace string) (string, error) {
	if !filepath.IsAbs(value) {
		value = filepath.Join(workspace, value)
	}
	value, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if err := validateRegular(value); err != nil {
		return "", err
	}
	return value, nil
}

func absoluteDirectory(value, workspace string) (string, error) {
	if !filepath.IsAbs(value) {
		value = filepath.Join(workspace, value)
	}
	value, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(value)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	return value, nil
}

func pathInside(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validateEvaluationGates(gates map[string]EvaluationGate) error {
	allowed := map[string]bool{"valid_rate": true, "false_accept_rate": true, "false_reject_rate": true, "flow_completion_rate": true, "validation_error_rate": true}
	names := make([]string, 0, len(gates))
	for name := range gates {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		gate := gates[name]
		if !allowed[name] {
			return fmt.Errorf("unsupported assessment gate %q", name)
		}
		if (gate.Min == nil) == (gate.Max == nil) {
			return fmt.Errorf("assessment gate %q must define exactly one of min or max", name)
		}
		value := gate.Min
		if value == nil {
			value = gate.Max
		}
		if *value < 0 || *value > 1 {
			return fmt.Errorf("assessment gate %q threshold must be between 0 and 1", name)
		}
	}
	return nil
}

func cloneEvaluationGates(source map[string]EvaluationGate) map[string]EvaluationGate {
	if len(source) == 0 {
		return map[string]EvaluationGate{}
	}
	result := make(map[string]EvaluationGate, len(source))
	for name, gate := range source {
		result[name] = gate
	}
	return result
}
