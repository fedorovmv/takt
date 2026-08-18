package evaluation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

const FlowProgressVersion = "takt-flow-evaluation-progress/v1alpha1"

const FlowProgressFile = "progress.json"

type FlowProgress struct {
	ReportVersion string               `json:"report_version"`
	Status        string               `json:"status"`
	Suite         string               `json:"suite"`
	Workflow      string               `json:"workflow"`
	OutputDir     string               `json:"output_dir"`
	StartedAt     time.Time            `json:"started_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
	TotalRuns     int                  `json:"total_runs"`
	CompletedRuns int                  `json:"completed_runs"`
	Current       *FlowProgressCurrent `json:"current,omitempty"`
	Runtime       FlowRuntimeProgress  `json:"runtime"`
	Results       FlowProgressResults  `json:"results"`
	ReportPath    string               `json:"report_path,omitempty"`
	Error         string               `json:"error,omitempty"`
}

type FlowProgressCurrent struct {
	CaseID         string    `json:"case_id"`
	Repeat         int       `json:"repeat"`
	Ordinal        int       `json:"ordinal"`
	Phase          string    `json:"phase"`
	PhaseStartedAt time.Time `json:"phase_started_at,omitempty"`
}

type FlowRuntimeProgress struct {
	RunID             string                  `json:"run_id,omitempty"`
	Status            string                  `json:"status,omitempty"`
	TotalNodes        int                     `json:"total_nodes"`
	CompletedNodes    int                     `json:"completed_nodes"`
	RunningNodes      []string                `json:"running_nodes"`
	NodeAttempts      int                     `json:"node_attempts"`
	ProviderAttempts  int                     `json:"provider_attempts"`
	InputTokens       int                     `json:"input_tokens"`
	OutputTokens      int                     `json:"output_tokens"`
	Cost              float64                 `json:"cost"`
	ContextTokens     int                     `json:"context_tokens,omitempty"`
	ContextKnown      bool                    `json:"context_known,omitempty"`
	Timings           *FlowRuntimeTimings     `json:"timings,omitempty"`
	AssistantActivity []FlowAssistantProgress `json:"assistant_activity,omitempty"`
}

type FlowRuntimeTimings struct {
	Phases    FlowPhaseTimings     `json:"phases"`
	Assistant FlowAssistantTimings `json:"assistant"`
}

type FlowPhaseTimings struct {
	PrepareMS            int64 `json:"prepare_ms"`
	ValidatorPreflightMS int64 `json:"validator_preflight_ms"`
	WorkflowMS           int64 `json:"workflow_ms"`
	ValidatorMS          int64 `json:"validator_ms"`
	EvidenceMS           int64 `json:"evidence_ms"`
	CleanupMS            int64 `json:"cleanup_ms"`
}

type FlowAssistantTimings struct {
	WaitMS   int64 `json:"wait_ms"`
	StreamMS int64 `json:"stream_ms"`
	TotalMS  int64 `json:"total_ms"`
	ToolMS   int64 `json:"tool_ms"`
}

type FlowAssistantProgress struct {
	RunID      string    `json:"run_id"`
	NodeID     string    `json:"node_id"`
	Attempt    int       `json:"attempt"`
	State      string    `json:"state"`
	Since      time.Time `json:"since"`
	Call       int       `json:"call,omitempty"`
	Retry      int       `json:"retry,omitempty"`
	MaxRetries int       `json:"max_retries,omitempty"`
	DelayMS    int       `json:"delay_ms,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}

type FlowProgressResults struct {
	Valid                int `json:"valid"`
	Invalid              int `json:"invalid"`
	InfrastructureErrors int `json:"infrastructure_errors"`
	ValidationErrors     int `json:"validation_errors"`
}

type flowProgressTracker struct {
	mu       sync.Mutex
	output   string
	now      func() time.Time
	progress FlowProgress
}

func newFlowProgressTracker(output string, progress FlowProgress, now func() time.Time) (*flowProgressTracker, error) {
	if now == nil {
		now = time.Now
	}
	progress.Runtime.RunningNodes = []string{}
	if progress.Runtime.Timings == nil {
		progress.Runtime.Timings = &FlowRuntimeTimings{}
	}
	tracker := &flowProgressTracker{output: output, now: now, progress: progress}
	tracker.progress.UpdatedAt = now().UTC()
	if err := WriteFlowProgress(output, &tracker.progress); err != nil {
		return nil, err
	}
	return tracker, nil
}

func (t *flowProgressTracker) begin(caseID string, repeat, ordinal int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now().UTC()
	t.progress.Current = &FlowProgressCurrent{CaseID: caseID, Repeat: repeat, Ordinal: ordinal, Phase: "prepare", PhaseStartedAt: now}
	t.progress.Runtime = FlowRuntimeProgress{RunningNodes: []string{}, Timings: &FlowRuntimeTimings{}}
	return t.writeLocked()
}

func (t *flowProgressTracker) phase(phase string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.progress.Current == nil {
		return fmt.Errorf("flow evaluation progress has no current run")
	}
	now := t.now().UTC()
	t.accumulatePhaseLocked(now)
	t.progress.Current.Phase = phase
	t.progress.Current.PhaseStartedAt = now
	return t.writeLocked()
}

func (t *flowProgressTracker) runtime(value FlowRuntimeProgress) (*FlowProgress, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.progress.Runtime.Timings != nil {
		if value.Timings == nil {
			value.Timings = t.progress.Runtime.Timings
		} else {
			value.Timings.Phases = t.progress.Runtime.Timings.Phases
		}
	}
	if value.RunningNodes == nil {
		value.RunningNodes = []string{}
	}
	t.progress.Runtime = value
	if err := t.writeLocked(); err != nil {
		return nil, err
	}
	return cloneFlowProgress(t.progress), nil
}

func (t *flowProgressTracker) results(completed int, summary Summary) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.progress.CompletedRuns = completed
	t.progress.Results.Valid = summary.Valid
	t.progress.Results.Invalid = summary.Invalid
	t.progress.Results.InfrastructureErrors = summary.InfrastructureErrors
	if summary.Flow != nil {
		t.progress.Results.ValidationErrors = summary.Flow.ValidationErrors
	}
	return t.writeLocked()
}

func (t *flowProgressTracker) complete(reportPath string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now().UTC()
	if t.progress.Current != nil {
		t.accumulatePhaseLocked(now)
		t.progress.Current.Phase = "finalized"
		t.progress.Current.PhaseStartedAt = now
	}
	t.progress.Status = "completed"
	t.progress.ReportPath = reportPath
	t.progress.UpdatedAt = now
	return WriteFlowProgress(t.output, &t.progress)
}

func (t *flowProgressTracker) fail(runErr error) error {
	if t == nil || runErr == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.progress.Status = "failed"
	t.progress.Error = runErr.Error()
	return t.writeLocked()
}

func (t *flowProgressTracker) writeLocked() error {
	now := t.now().UTC()
	t.accumulatePhaseLocked(now)
	t.progress.UpdatedAt = now
	return WriteFlowProgress(t.output, &t.progress)
}

func (t *flowProgressTracker) accumulatePhaseLocked(now time.Time) {
	if t.progress.Current == nil {
		return
	}
	if t.progress.Runtime.Timings == nil {
		t.progress.Runtime.Timings = &FlowRuntimeTimings{}
	}
	started := t.progress.Current.PhaseStartedAt
	if started.IsZero() {
		t.progress.Current.PhaseStartedAt = now
		return
	}
	duration := now.Sub(started).Milliseconds()
	if duration > 0 {
		addFlowPhaseTiming(&t.progress.Runtime.Timings.Phases, t.progress.Current.Phase, duration)
	}
	t.progress.Current.PhaseStartedAt = now
}

func addFlowPhaseTiming(timings *FlowPhaseTimings, phase string, duration int64) {
	if timings == nil || duration <= 0 {
		return
	}
	switch phase {
	case "prepare":
		timings.PrepareMS += duration
	case "validator_preflight":
		timings.ValidatorPreflightMS += duration
	case "workflow":
		timings.WorkflowMS += duration
	case "validator":
		timings.ValidatorMS += duration
	case "evidence":
		timings.EvidenceMS += duration
	case "cleanup":
		timings.CleanupMS += duration
	}
}

func cloneFlowProgress(value FlowProgress) *FlowProgress {
	clone := value
	if value.Current != nil {
		current := *value.Current
		clone.Current = &current
	}
	clone.Runtime.RunningNodes = append([]string(nil), value.Runtime.RunningNodes...)
	clone.Runtime.AssistantActivity = append([]FlowAssistantProgress(nil), value.Runtime.AssistantActivity...)
	if value.Runtime.Timings != nil {
		timings := *value.Runtime.Timings
		clone.Runtime.Timings = &timings
	}
	return &clone
}

func (p FlowProgress) String() string { return p.render(time.Now().UTC()) }

func WriteFlowProgress(outputDir string, progress *FlowProgress) error {
	if err := validateFlowProgress(progress); err != nil {
		return err
	}
	return writeJSONAtomic(filepath.Join(outputDir, FlowProgressFile), progress)
}

func LoadFlowProgress(outputDir string) (*FlowProgress, error) {
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(abs, FlowProgressFile))
	if err != nil {
		return nil, err
	}
	var progress FlowProgress
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&progress); err != nil {
		return nil, fmt.Errorf("decode flow evaluation progress: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode flow evaluation progress: trailing JSON value")
		}
		return nil, fmt.Errorf("decode flow evaluation progress trailing data: %w", err)
	}
	if err := validateFlowProgress(&progress); err != nil {
		return nil, err
	}
	return &progress, nil
}

func validateFlowProgress(progress *FlowProgress) error {
	if progress == nil {
		return fmt.Errorf("flow evaluation progress is required")
	}
	if progress.ReportVersion != FlowProgressVersion {
		return fmt.Errorf("flow evaluation progress version must be %s", FlowProgressVersion)
	}
	if progress.Suite == "" || progress.Workflow == "" || progress.OutputDir == "" || progress.StartedAt.IsZero() || progress.UpdatedAt.IsZero() {
		return fmt.Errorf("flow evaluation progress identity and timestamps are required")
	}
	switch progress.Status {
	case "running", "completed", "failed":
	default:
		return fmt.Errorf("invalid flow evaluation progress status %q", progress.Status)
	}
	if progress.TotalRuns < 0 || progress.CompletedRuns < 0 || progress.CompletedRuns > progress.TotalRuns {
		return fmt.Errorf("invalid flow evaluation run counts completed=%d total=%d", progress.CompletedRuns, progress.TotalRuns)
	}
	if progress.Runtime.TotalNodes < 0 || progress.Runtime.CompletedNodes < 0 || progress.Runtime.CompletedNodes > progress.Runtime.TotalNodes || progress.Runtime.NodeAttempts < 0 || progress.Runtime.ProviderAttempts < 0 || progress.Runtime.InputTokens < 0 || progress.Runtime.OutputTokens < 0 || progress.Runtime.Cost < 0 || progress.Runtime.ContextTokens < 0 {
		return fmt.Errorf("invalid flow runtime progress counters")
	}
	if progress.Runtime.RunningNodes == nil {
		return fmt.Errorf("flow runtime running_nodes is required")
	}
	if timings := progress.Runtime.Timings; timings != nil {
		if timings.Phases.PrepareMS < 0 || timings.Phases.ValidatorPreflightMS < 0 || timings.Phases.WorkflowMS < 0 || timings.Phases.ValidatorMS < 0 || timings.Phases.EvidenceMS < 0 || timings.Phases.CleanupMS < 0 || timings.Assistant.WaitMS < 0 || timings.Assistant.StreamMS < 0 || timings.Assistant.TotalMS < 0 || timings.Assistant.ToolMS < 0 {
			return fmt.Errorf("invalid flow runtime timings")
		}
	}
	for _, activity := range progress.Runtime.AssistantActivity {
		if activity.RunID == "" || activity.NodeID == "" || activity.Attempt < 0 || activity.State == "" || activity.Since.IsZero() || activity.Call < 0 || activity.Retry < 0 || activity.MaxRetries < 0 || activity.DelayMS < 0 {
			return fmt.Errorf("invalid flow assistant activity")
		}
	}
	if progress.Results.Valid < 0 || progress.Results.Invalid < 0 || progress.Results.InfrastructureErrors < 0 || progress.Results.ValidationErrors < 0 {
		return fmt.Errorf("invalid flow evaluation result counters")
	}
	if progress.Current != nil {
		if progress.Current.CaseID == "" || progress.Current.Repeat <= 0 || progress.Current.Ordinal <= 0 || progress.Current.Ordinal > progress.TotalRuns {
			return fmt.Errorf("invalid current flow evaluation run")
		}
		switch progress.Current.Phase {
		case "prepare", "validator_preflight", "workflow", "validator", "evidence", "cleanup", "finalized":
		default:
			return fmt.Errorf("invalid flow evaluation phase %q", progress.Current.Phase)
		}
	}
	return nil
}

func (p FlowProgress) render(now time.Time) string {
	var output strings.Builder
	table := tabwriter.NewWriter(&output, 0, 2, 2, ' ', 0)
	fmt.Fprintln(table, "EVALUATION")
	fmt.Fprintf(table, "  Status\t%s\n", valueOrDash(p.Status))
	fmt.Fprintf(table, "  Updated\t%s\n", progressAge(now, p.UpdatedAt))
	fmt.Fprintf(table, "  Elapsed\t%s\n", progressElapsed(p, now))
	fmt.Fprintf(table, "  Progress\t%s / %s runs (%s)\n", formatNumber(int64(p.CompletedRuns)), formatNumber(int64(p.TotalRuns)), progressPercent(p.CompletedRuns, p.TotalRuns))
	if p.Current != nil {
		fmt.Fprintf(table, "  Current\t%s#%d\n", p.Current.CaseID, p.Current.Repeat)
		fmt.Fprintf(table, "  Phase\t%s\n", p.Current.Phase)
	} else {
		fmt.Fprintln(table, "  Current\t-")
		fmt.Fprintln(table, "  Phase\t-")
	}

	fmt.Fprintln(table, "\nFLOW")
	fmt.Fprintf(table, "  Run\t%s\n", valueOrDash(p.Runtime.RunID))
	fmt.Fprintf(table, "  Status\t%s\n", valueOrDash(p.Runtime.Status))
	fmt.Fprintf(table, "  Running nodes\t%s\n", formatList(p.Runtime.RunningNodes))
	fmt.Fprintf(table, "  Nodes\t%s / %s completed (%s)\n", formatNumber(int64(p.Runtime.CompletedNodes)), formatNumber(int64(p.Runtime.TotalNodes)), progressPercent(p.Runtime.CompletedNodes, p.Runtime.TotalNodes))
	fmt.Fprintf(table, "  Node attempts\t%s\n", formatNumber(int64(p.Runtime.NodeAttempts)))
	fmt.Fprintf(table, "  Provider attempts\t%s\n", formatNumber(int64(p.Runtime.ProviderAttempts)))
	fmt.Fprintf(table, "  Tokens input\t%s\n", formatNumber(int64(p.Runtime.InputTokens)))
	fmt.Fprintf(table, "  Tokens output\t%s\n", formatNumber(int64(p.Runtime.OutputTokens)))
	fmt.Fprintf(table, "  Tokens total\t%s\n", formatNumber(int64(p.Runtime.InputTokens+p.Runtime.OutputTokens)))
	contextTokens := "n/a"
	if p.Runtime.ContextKnown {
		contextTokens = formatNumber(int64(p.Runtime.ContextTokens))
	}
	fmt.Fprintf(table, "  Context tokens\t%s\n", contextTokens)
	fmt.Fprintf(table, "  Cost measured\t%g\n", p.Runtime.Cost)
	fmt.Fprintln(table, "\nTIMINGS")
	if p.Runtime.Timings == nil {
		fmt.Fprintln(table, "  Measured\tunavailable")
	} else {
		phases := p.Runtime.Timings.Phases
		if p.Status == "running" && p.Current != nil && !p.Current.PhaseStartedAt.IsZero() {
			activeMS := now.Sub(p.Current.PhaseStartedAt).Milliseconds()
			if activeMS > 0 {
				addFlowPhaseTiming(&phases, p.Current.Phase, activeMS)
			}
		}
		fmt.Fprintf(table, "  Prepare\t%s\n", formatDurationMS(phases.PrepareMS))
		fmt.Fprintf(table, "  Validator preflight\t%s\n", formatDurationMS(phases.ValidatorPreflightMS))
		fmt.Fprintf(table, "  Workflow\t%s\n", formatDurationMS(phases.WorkflowMS))
		fmt.Fprintf(table, "  Validator\t%s\n", formatDurationMS(phases.ValidatorMS))
		fmt.Fprintf(table, "  Evidence\t%s\n", formatDurationMS(phases.EvidenceMS))
		fmt.Fprintf(table, "  Cleanup\t%s\n", formatDurationMS(phases.CleanupMS))
		assistant := p.Runtime.Timings.Assistant
		if p.Status == "running" {
			for _, activity := range p.Runtime.AssistantActivity {
				if activity.State != "awaiting_response" {
					continue
				}
				activeMS := now.Sub(activity.Since).Milliseconds()
				if activeMS > 0 {
					assistant.WaitMS += activeMS
				}
			}
		}
		fmt.Fprintf(table, "  LLM wait\t%s\n", formatDurationMS(assistant.WaitMS))
		fmt.Fprintf(table, "  LLM stream\t%s\n", formatDurationMS(assistant.StreamMS))
		fmt.Fprintf(table, "  LLM total\t%s\n", formatDurationMS(assistant.TotalMS))
		fmt.Fprintf(table, "  Assistant tools\t%s\n", formatDurationMS(assistant.ToolMS))
	}
	if len(p.Runtime.AssistantActivity) > 0 {
		fmt.Fprintln(table, "\nPROVIDER ACTIVITY")
		for _, activity := range p.Runtime.AssistantActivity {
			age := now.Sub(activity.Since)
			if age < 0 {
				age = 0
			}
			details := []string{activity.State, "for " + age.Truncate(time.Second).String()}
			if activity.Call > 0 {
				details = append(details, fmt.Sprintf("call %d", activity.Call))
			}
			if activity.Retry > 0 {
				if activity.MaxRetries > 0 {
					details = append(details, fmt.Sprintf("retry %d/%d", activity.Retry, activity.MaxRetries))
				} else {
					details = append(details, fmt.Sprintf("retry %d", activity.Retry))
				}
			}
			if activity.DelayMS > 0 {
				details = append(details, "delay "+(time.Duration(activity.DelayMS)*time.Millisecond).String())
			}
			fmt.Fprintf(table, "  %s#%d\t%s\n", activity.NodeID, activity.Attempt, strings.Join(details, " · "))
			if activity.LastError != "" {
				fmt.Fprintf(table, "    Error\t%s\n", activity.LastError)
			}
		}
	}

	fmt.Fprintln(table, "\nRESULTS SO FAR")
	fmt.Fprintf(table, "  Valid\t%s\n", formatNumber(int64(p.Results.Valid)))
	fmt.Fprintf(table, "  Invalid\t%s\n", formatNumber(int64(p.Results.Invalid)))
	fmt.Fprintf(table, "  Infrastructure errors\t%s\n", formatNumber(int64(p.Results.InfrastructureErrors)))
	fmt.Fprintf(table, "  Validation errors\t%s\n", formatNumber(int64(p.Results.ValidationErrors)))
	qualityCases := p.Results.Valid + p.Results.Invalid
	fmt.Fprintf(table, "  Quality valid\t%s / %s completed (%s)\n", formatNumber(int64(p.Results.Valid)), formatNumber(int64(qualityCases)), progressPercent(p.Results.Valid, qualityCases))
	if p.Error != "" {
		fmt.Fprintf(table, "  Error\t%s\n", p.Error)
	}
	_ = table.Flush()
	return strings.TrimSpace(output.String())
}

func progressElapsed(progress FlowProgress, now time.Time) string {
	end := now
	if progress.Status != "running" {
		end = progress.UpdatedAt
	}
	duration := end.Sub(progress.StartedAt)
	if duration < 0 {
		duration = 0
	}
	return duration.Truncate(time.Second).String()
}

func progressPercent(value, total int) string {
	if total <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", float64(value)*100/float64(total))
}

func progressAge(now, updated time.Time) string {
	if updated.IsZero() {
		return "unknown"
	}
	age := now.Sub(updated).Truncate(time.Second)
	if age < 0 {
		age = 0
	}
	return age.String() + " ago"
}

func formatList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ", ")
}
