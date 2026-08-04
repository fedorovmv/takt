package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	cfgpkg "takt/internal/config"
	"takt/internal/runtime"
	"takt/internal/store"
	"takt/internal/workflow"
)

type RunOptions struct {
	WorkflowPath      string
	ConfigPath        string
	CasesDir          string
	WorkspaceTemplate string
	OutputDir         string
	Repeat            int
	ApprovalAnswer    string
	Replace           bool
}

type SuiteReport struct {
	StartedAt  time.Time   `json:"started_at"`
	FinishedAt time.Time   `json:"finished_at"`
	DurationMS int64       `json:"duration_ms"`
	Workflow   string      `json:"workflow"`
	Config     string      `json:"config"`
	CasesDir   string      `json:"cases_dir"`
	OutputDir  string      `json:"output_dir"`
	Runs       []RunRecord `json:"runs"`
	Summary    Summary     `json:"summary"`
}

type Summary struct {
	Total        int            `json:"total"`
	ByStatus     map[string]int `json:"by_status"`
	Attempts     int            `json:"attempts"`
	InputTokens  int            `json:"input_tokens"`
	OutputTokens int            `json:"output_tokens"`
	Cost         float64        `json:"cost"`
	DurationMS   int64          `json:"duration_ms"`
	Answers      int            `json:"answers"`
	Truncated    int            `json:"truncated_nodes"`
	Resumed      int            `json:"resumed_nodes"`
}

type RunRecord struct {
	CaseID       string                `json:"case_id"`
	Repeat       int                   `json:"repeat"`
	RunID        string                `json:"run_id,omitempty"`
	Status       string                `json:"status"`
	Workspace    string                `json:"workspace"`
	DurationMS   int64                 `json:"duration_ms"`
	Attempts     int                   `json:"attempts"`
	InputTokens  int                   `json:"input_tokens,omitempty"`
	OutputTokens int                   `json:"output_tokens,omitempty"`
	Cost         float64               `json:"cost,omitempty"`
	Answers      int                   `json:"answers,omitempty"`
	Truncated    int                   `json:"truncated_nodes,omitempty"`
	Resumed      int                   `json:"resumed_nodes,omitempty"`
	ErrorCode    string                `json:"error_code,omitempty"`
	Error        string                `json:"error,omitempty"`
	Nodes        map[string]NodeRecord `json:"nodes"`
}

type NodeRecord struct {
	Status           string       `json:"status"`
	Attempts         int          `json:"attempts,omitempty"`
	SessionID        string       `json:"session_id,omitempty"`
	Resumed          bool         `json:"resumed,omitempty"`
	ExitCode         int          `json:"exit_code,omitempty"`
	ErrorCode        string       `json:"error_code,omitempty"`
	Error            string       `json:"error,omitempty"`
	Feedback         string       `json:"feedback,omitempty"`
	DiagnosticOutput string       `json:"diagnostic_output,omitempty"`
	OutputTruncated  bool         `json:"output_truncated,omitempty"`
	Usage            *store.Usage `json:"usage,omitempty"`
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
	if err := os.MkdirAll(paths.OutputDir, 0o755); err != nil {
		return nil, err
	}

	report := &SuiteReport{
		StartedAt: time.Now().UTC(), Workflow: paths.WorkflowPath, Config: paths.ConfigPath,
		CasesDir: paths.CasesDir, OutputDir: paths.OutputDir,
		Summary: Summary{ByStatus: map[string]int{}},
	}
	for _, casePath := range cases {
		caseID := caseIDs[casePath]
		for repeat := 1; repeat <= opts.Repeat; repeat++ {
			workspace := filepath.Join(paths.OutputDir, "workspaces", fmt.Sprintf("%s-%03d", caseID, repeat))
			record, runErr := runOne(ctx, paths, opts, casePath, caseID, repeat, workspace)
			report.Runs = append(report.Runs, record)
			addSummary(&report.Summary, record)
			if runErr != nil && isInfrastructureError(runErr) {
				report.FinishedAt = time.Now().UTC()
				report.DurationMS = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
				_ = writeReport(paths.OutputDir, report)
				return report, runErr
			}
		}
	}
	report.FinishedAt = time.Now().UTC()
	report.DurationMS = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	if err := writeReport(paths.OutputDir, report); err != nil {
		return report, err
	}
	return report, nil
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

func runOne(ctx context.Context, paths resolvedOptions, opts RunOptions, casePath, caseID string, repeat int, workspace string) (RunRecord, error) {
	record := RunRecord{CaseID: caseID, Repeat: repeat, Workspace: workspace, Status: "not_started", Nodes: map[string]NodeRecord{}}
	if _, err := os.Stat(workspace); err == nil {
		if !opts.Replace {
			record.Status = "infrastructure_error"
			record.Error = "workspace already exists; use --replace"
			return record, fmt.Errorf("workspace %s already exists", workspace)
		}
		if err := os.RemoveAll(workspace); err != nil {
			return record, err
		}
	} else if !os.IsNotExist(err) {
		return record, err
	}
	if err := copyTree(paths.WorkspaceTemplate, workspace); err != nil {
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
	runner := runtime.New(wf, cfg, paths.WorkflowPath, paths.ConfigPath, workspace)
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
		if err := runner.Store.Commit(state, store.Event{Type: "approval.answered", NodeID: nodeID, Data: map[string]any{"value_captured": true, "source": "evaluation"}}); err != nil {
			return record, err
		}
		state, runErr = runner.Resume(ctx, state)
	}
	if state != nil {
		record = recordFromState(caseID, repeat, workspace, state)
	}
	if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) {
		if record.Error == "" {
			record.Error = runErr.Error()
		}
		return record, runErr
	}
	return record, nil
}

func recordFromState(caseID string, repeat int, workspace string, state *store.RunState) RunRecord {
	record := RunRecord{
		CaseID: caseID, Repeat: repeat, RunID: state.ID, Status: state.Status, Workspace: workspace,
		DurationMS: state.UpdatedAt.Sub(state.CreatedAt).Milliseconds(), Answers: len(state.Approvals),
		ErrorCode: state.ErrorCode, Error: state.Error, Nodes: map[string]NodeRecord{},
	}
	for id, node := range state.Nodes {
		if node == nil {
			continue
		}
		record.Attempts += node.Attempts
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
		record.Nodes[id] = NodeRecord{
			Status: node.Status, Attempts: node.Attempts, SessionID: node.SessionID, Resumed: node.Resumed,
			ExitCode: node.ExitCode, ErrorCode: node.ErrorCode, Error: node.Error, Feedback: node.Feedback,
			DiagnosticOutput: node.Output, OutputTruncated: node.OutputTruncated, Usage: node.Usage,
		}
	}
	return record
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
}

func writeReport(outputDir string, report *SuiteReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(outputDir, "report.json.tmp")
	path := filepath.Join(outputDir, "report.json")
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
		if rel == ".takt" || strings.HasPrefix(rel, ".takt"+string(filepath.Separator)) || rel == "bin" || strings.HasPrefix(rel, "bin"+string(filepath.Separator)) {
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
