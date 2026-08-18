package evaluation

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const StatsReportVersion = "takt-evaluation-stats/v1alpha1"

type EvaluationStats struct {
	ReportVersion            string                    `json:"report_version"`
	Status                   string                    `json:"status,omitempty"`
	Complete                 bool                      `json:"complete"`
	Mode                     string                    `json:"mode"`
	Workflow                 string                    `json:"workflow"`
	OutputDir                string                    `json:"output_dir"`
	StartedAt                time.Time                 `json:"started_at"`
	FinishedAt               time.Time                 `json:"finished_at"`
	Strategy                 StrategyIdentity          `json:"strategy"`
	Benchmark                BenchmarkIdentity         `json:"benchmark"`
	Total                    int                       `json:"total"`
	Valid                    int                       `json:"valid"`
	Invalid                  int                       `json:"invalid"`
	Attempts                 int                       `json:"attempts"`
	AssistantExecutions      int                       `json:"assistant_executions"`
	RetryScheduled           int                       `json:"retry_scheduled"`
	Resumed                  int                       `json:"resumed_nodes"`
	InfrastructureErrors     int                       `json:"infrastructure_errors"`
	InputTokens              int                       `json:"input_tokens"`
	OutputTokens             int                       `json:"output_tokens"`
	TotalTokens              int                       `json:"total_tokens"`
	DurationMS               int64                     `json:"duration_ms"`
	Cost                     float64                   `json:"cost"`
	AverageTimeToValidMS     *float64                  `json:"average_time_to_valid_ms"`
	UsageByExecutionIdentity map[string]UsageBreakdown `json:"usage_by_execution_identity"`
	Flow                     *FlowSummary              `json:"flow,omitempty"`
	Outcomes                 map[string]int            `json:"outcomes"`
	Diagnostics              map[string]int            `json:"diagnostics"`
	Cases                    []StatsCase               `json:"cases"`
	AssistantSteps           []StatsAssistantStep      `json:"assistant_steps"`
	AssistantSessions        []StatsAssistantSession   `json:"assistant_sessions"`
	TotalRuns                int                       `json:"total_runs,omitempty"`
	CompletedRuns            int                       `json:"completed_runs,omitempty"`
	Current                  *FlowProgressCurrent      `json:"current,omitempty"`
	Timings                  *FlowRuntimeTimings       `json:"timings,omitempty"`
}

type StatsCase struct {
	CaseID        string `json:"case_id"`
	Repeat        int    `json:"repeat"`
	Status        string `json:"status"`
	Outcome       string `json:"outcome,omitempty"`
	CauseSource   string `json:"cause_source,omitempty"`
	Cause         string `json:"cause,omitempty"`
	Attempts      int    `json:"attempts"`
	InputTokens   int    `json:"input_tokens"`
	OutputTokens  int    `json:"output_tokens"`
	DurationMS    int64  `json:"duration_ms"`
	TimeToValidMS *int64 `json:"time_to_valid_ms,omitempty"`
}

type StatsAssistantStep struct {
	CaseID       string `json:"case_id"`
	Repeat       int    `json:"repeat"`
	Step         string `json:"step"`
	Model        string `json:"model"`
	Executions   int    `json:"executions"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	DurationMS   *int64 `json:"duration_ms"`
}

type StatsAssistantSession struct {
	CaseID          string `json:"case_id"`
	Repeat          int    `json:"repeat"`
	Step            string `json:"step"`
	Attempt         int    `json:"attempt"`
	ProviderAttempt int    `json:"provider_attempt"`
	Resumed         bool   `json:"resumed"`
	SessionID       string `json:"session_id"`
}

func BuildStats(report *SuiteReport) *EvaluationStats {
	if report == nil {
		return nil
	}
	stats := &EvaluationStats{
		ReportVersion: StatsReportVersion, Status: "completed", Complete: true, Mode: report.Mode, Workflow: report.Workflow, OutputDir: report.OutputDir,
		StartedAt: report.StartedAt, FinishedAt: report.FinishedAt, Strategy: report.Strategy, Benchmark: report.Benchmark,
		Total: report.Summary.Total, Valid: report.Summary.Valid, Invalid: report.Summary.Invalid,
		Attempts: report.Summary.Attempts, InputTokens: report.Summary.InputTokens, OutputTokens: report.Summary.OutputTokens,
		TotalTokens: report.Summary.InputTokens + report.Summary.OutputTokens, DurationMS: report.Summary.DurationMS,
		Cost: report.Summary.Cost, RetryScheduled: report.Summary.RetryScheduled, Resumed: report.Summary.Resumed,
		InfrastructureErrors: report.Summary.InfrastructureErrors, AverageTimeToValidMS: report.Summary.AverageTimeToValidMS,
		UsageByExecutionIdentity: report.Summary.UsageByExecutionIdentity, Flow: report.Summary.Flow,
		Outcomes: map[string]int{}, Diagnostics: map[string]int{}, Cases: make([]StatsCase, 0, len(report.Runs)), AssistantSteps: []StatsAssistantStep{}, AssistantSessions: []StatsAssistantSession{},
	}
	for code, count := range report.Summary.DiagnosticsByCode {
		stats.Diagnostics[code] = count
	}
	for _, run := range report.Runs {
		if run.Outcome != "" {
			stats.Outcomes[run.Outcome]++
		}
		causeSource, cause := primaryRunCause(run)
		stats.Cases = append(stats.Cases, StatsCase{CaseID: run.CaseID, Repeat: run.Repeat, Status: run.Status, Outcome: run.Outcome, CauseSource: causeSource, Cause: cause, Attempts: run.Attempts, InputTokens: run.InputTokens, OutputTokens: run.OutputTokens, DurationMS: run.DurationMS, TimeToValidMS: run.TimeToValidMS})
		nodeIDs := make([]string, 0, len(run.Nodes))
		for nodeID := range run.Nodes {
			nodeIDs = append(nodeIDs, nodeID)
		}
		sort.Strings(nodeIDs)
		for _, nodeID := range nodeIDs {
			node := run.Nodes[nodeID]
			if node.Assistant == "" {
				continue
			}
			step := StatsAssistantStep{CaseID: run.CaseID, Repeat: run.Repeat, Step: shortNodeID(nodeID), Model: nodeModel(node), DurationMS: node.DurationMS}
			for _, execution := range node.Executions {
				if execution.Assistant == "" {
					continue
				}
				step.Executions++
				if execution.SessionID != "" {
					stats.AssistantSessions = append(stats.AssistantSessions, StatsAssistantSession{CaseID: run.CaseID, Repeat: run.Repeat, Step: step.Step, Attempt: execution.Attempt, ProviderAttempt: execution.ProviderAttempt, Resumed: execution.Resumed, SessionID: execution.SessionID})
				}
				if execution.Usage != nil {
					step.InputTokens += execution.Usage.InputTokens
					step.OutputTokens += execution.Usage.OutputTokens
				}
			}
			stats.AssistantExecutions += step.Executions
			stats.AssistantSteps = append(stats.AssistantSteps, step)
		}
	}
	return stats
}

func BuildProgressStats(progress FlowProgress, now time.Time) *EvaluationStats {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	stats := &EvaluationStats{
		ReportVersion: StatsReportVersion, Status: progress.Status, Mode: "flow", Workflow: progress.Workflow, OutputDir: progress.OutputDir,
		StartedAt: progress.StartedAt, FinishedAt: now, Total: progress.TotalRuns, Valid: progress.Results.Valid, Invalid: progress.Results.Invalid,
		Attempts: progress.Runtime.NodeAttempts, InfrastructureErrors: progress.Results.InfrastructureErrors,
		InputTokens: progress.Runtime.InputTokens, OutputTokens: progress.Runtime.OutputTokens,
		TotalTokens: progress.Runtime.InputTokens + progress.Runtime.OutputTokens, Cost: progress.Runtime.Cost,
		UsageByExecutionIdentity: map[string]UsageBreakdown{}, Outcomes: map[string]int{}, Diagnostics: map[string]int{},
		Cases: []StatsCase{}, AssistantSteps: []StatsAssistantStep{}, AssistantSessions: []StatsAssistantSession{},
		TotalRuns: progress.TotalRuns, CompletedRuns: progress.CompletedRuns, Timings: cloneFlowRuntimeTimings(progress.Runtime.Timings),
	}
	ApplyLiveProgressStats(stats, progress, now)
	if stats.Valid > 0 {
		stats.Outcomes["valid"] = stats.Valid
	}
	if stats.Invalid > 0 {
		stats.Outcomes["invalid"] = stats.Invalid
	}
	if stats.InfrastructureErrors > 0 {
		stats.Outcomes["infrastructure_error"] = stats.InfrastructureErrors
	}
	return stats
}

func ApplyLiveProgressStats(stats *EvaluationStats, progress FlowProgress, now time.Time) {
	if stats == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	end := now
	if progress.Status != "running" {
		end = progress.UpdatedAt
	}
	duration := end.Sub(progress.StartedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	stats.Status = progress.Status
	stats.Complete = false
	stats.StartedAt = progress.StartedAt
	stats.FinishedAt = end
	stats.DurationMS = duration
	stats.Total = progress.TotalRuns
	stats.Valid = progress.Results.Valid
	stats.Invalid = progress.Results.Invalid
	stats.InfrastructureErrors = progress.Results.InfrastructureErrors
	stats.TotalRuns = progress.TotalRuns
	stats.CompletedRuns = progress.CompletedRuns
	stats.Current = nil
	if progress.Current != nil {
		current := *progress.Current
		stats.Current = &current
	}
	stats.Timings = cloneFlowRuntimeTimings(progress.Runtime.Timings)
}

func cloneFlowRuntimeTimings(value *FlowRuntimeTimings) *FlowRuntimeTimings {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func (s EvaluationStats) String() string {
	var output strings.Builder
	table := tabwriter.NewWriter(&output, 0, 2, 2, ' ', 0)
	fmt.Fprintln(table, "RUN")
	fmt.Fprintf(table, "  Directory\t%s\n", s.OutputDir)
	fmt.Fprintf(table, "  Workflow\t%s\n", s.Workflow)
	fmt.Fprintf(table, "  Benchmark\t%s\t%s\n", s.Benchmark.ID, shortFingerprint(s.Benchmark.Fingerprint))
	fmt.Fprintf(table, "  Validator\t%s@%s\t%s\n", s.Benchmark.Validator.ID, s.Benchmark.Validator.Version, shortFingerprint(s.Benchmark.Validator.Fingerprint))
	fmt.Fprintf(table, "  Preset\t%s\n", valueOrDash(s.Strategy.ModelPreset))
	if s.Status != "" || !s.Complete {
		fmt.Fprintln(table, "\nSTATUS")
		fmt.Fprintf(table, "  Status\t%s\n", valueOrDash(s.Status))
		fmt.Fprintf(table, "  Complete\t%s\n", yesNo(s.Complete))
		if s.TotalRuns > 0 {
			fmt.Fprintf(table, "  Progress\t%s / %s runs (%s)\n", formatNumber(int64(s.CompletedRuns)), formatNumber(int64(s.TotalRuns)), progressPercent(s.CompletedRuns, s.TotalRuns))
		}
		if s.Current != nil {
			fmt.Fprintf(table, "  Current\t%s#%d\n", s.Current.CaseID, s.Current.Repeat)
			fmt.Fprintf(table, "  Phase\t%s\n", s.Current.Phase)
		}
	}

	fmt.Fprintln(table, "\nRESULT")
	fmt.Fprintf(table, "  Cases\t%s\n", formatNumber(int64(s.Total)))
	fmt.Fprintf(table, "  Valid\t%s\n", formatNumber(int64(s.Valid)))
	fmt.Fprintf(table, "  Invalid\t%s\n", formatNumber(int64(s.Invalid)))
	fmt.Fprintf(table, "  Outcomes\t%s\n", formatCounts(s.Outcomes))
	fmt.Fprintf(table, "  Diagnostics\t%s\n", formatCounts(s.Diagnostics))
	if s.Flow != nil {
		fmt.Fprintf(table, "  Flow valid\t%s\n", formatPercent(s.Flow.ValidRate))
		fmt.Fprintf(table, "  Completion\t%s\n", formatPercent(s.Flow.FlowCompletionRate))
		fmt.Fprintf(table, "  False accept\t%s\n", formatPercent(s.Flow.FalseAcceptRate))
		fmt.Fprintf(table, "  False reject\t%s\n", formatPercent(s.Flow.FalseRejectRate))
		fmt.Fprintf(table, "  Validation errors\t%s\n", formatPercent(s.Flow.ValidationErrorRate))
	}

	fmt.Fprintln(table, "\nFAILURES")
	fmt.Fprintln(table, "  Case\tOutcome\tSource\tCause")
	failures := 0
	for _, item := range s.Cases {
		if item.Cause == "" && (item.Outcome == "" || item.Outcome == "true_accept") {
			continue
		}
		failures++
		fmt.Fprintf(table, "  %s#%d\t%s\t%s\t%s\n", item.CaseID, item.Repeat, valueOrDash(item.Outcome), valueOrDash(item.CauseSource), valueOrDash(item.Cause))
	}
	if failures == 0 {
		fmt.Fprintln(table, "  -\t-\t-\t-")
	}

	fmt.Fprintln(table, "\nRESOURCES")
	fmt.Fprintf(table, "  Duration\t%s\n", formatDurationMS(s.DurationMS))
	fmt.Fprintf(table, "  Time to valid\t%s\n", formatDurationMetric(s.AverageTimeToValidMS))
	fmt.Fprintf(table, "  Node attempts\t%s\n", formatNumber(int64(s.Attempts)))
	fmt.Fprintf(table, "  Assistant executions\t%s\n", formatNumber(int64(s.AssistantExecutions)))
	fmt.Fprintf(table, "  Retries\t%s\n", formatNumber(int64(s.RetryScheduled)))
	fmt.Fprintf(table, "  Resumes\t%s\n", formatNumber(int64(s.Resumed)))
	fmt.Fprintf(table, "  Infra errors\t%s\n", formatNumber(int64(s.InfrastructureErrors)))
	fmt.Fprintf(table, "  Tokens\t%s\n", formatNumber(int64(s.TotalTokens)))
	fmt.Fprintf(table, "  Input tokens\t%s\n", formatNumber(int64(s.InputTokens)))
	fmt.Fprintf(table, "  Output tokens\t%s\n", formatNumber(int64(s.OutputTokens)))
	fmt.Fprintf(table, "  Cost\t%g\n", s.Cost)
	if s.Timings != nil {
		fmt.Fprintln(table, "\nTIMINGS")
		fmt.Fprintf(table, "  Prepare\t%s\n", formatDurationMS(s.Timings.Phases.PrepareMS))
		fmt.Fprintf(table, "  Validator preflight\t%s\n", formatDurationMS(s.Timings.Phases.ValidatorPreflightMS))
		fmt.Fprintf(table, "  Workflow\t%s\n", formatDurationMS(s.Timings.Phases.WorkflowMS))
		fmt.Fprintf(table, "  Validator\t%s\n", formatDurationMS(s.Timings.Phases.ValidatorMS))
		fmt.Fprintf(table, "  Evidence\t%s\n", formatDurationMS(s.Timings.Phases.EvidenceMS))
		fmt.Fprintf(table, "  Cleanup\t%s\n", formatDurationMS(s.Timings.Phases.CleanupMS))
		fmt.Fprintf(table, "  LLM wait\t%s\n", formatDurationMS(s.Timings.Assistant.WaitMS))
		fmt.Fprintf(table, "  LLM stream\t%s\n", formatDurationMS(s.Timings.Assistant.StreamMS))
		fmt.Fprintf(table, "  LLM total\t%s\n", formatDurationMS(s.Timings.Assistant.TotalMS))
		fmt.Fprintf(table, "  Assistant tools\t%s\n", formatDurationMS(s.Timings.Assistant.ToolMS))
	}

	fmt.Fprintln(table, "\nMODELS")
	fmt.Fprintln(table, "  Alias\tModel")
	aliases := make([]string, 0, len(s.Strategy.Models))
	for alias := range s.Strategy.Models {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		fmt.Fprintf(table, "  %s\t%s\n", alias, s.Strategy.Models[alias])
	}
	if len(aliases) == 0 {
		fmt.Fprintln(table, "  -\t-")
	}

	fmt.Fprintln(table, "\nUSAGE")
	fmt.Fprintln(table, "  Model\tAssistant\tRuns\tInput\tOutput\tCost")
	identities := make([]string, 0, len(s.UsageByExecutionIdentity))
	for identity := range s.UsageByExecutionIdentity {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	for _, identity := range identities {
		usage := s.UsageByExecutionIdentity[identity]
		model, assistant := humanExecutionIdentity(identity)
		fmt.Fprintf(table, "  %s\t%s\t%s\t%s\t%s\t%g\n", model, assistant, formatNumber(int64(usage.Executions)), formatNumber(int64(usage.InputTokens)), formatNumber(int64(usage.OutputTokens)), usage.Cost)
	}
	if len(identities) == 0 {
		fmt.Fprintln(table, "  -\t-\t-\t-\t-\t-")
	}

	fmt.Fprintln(table, "\nASSISTANT STEPS")
	fmt.Fprintln(table, "  Case\tStep\tModel\tExecutions\tWall time\tTokens")
	for _, step := range s.AssistantSteps {
		fmt.Fprintf(table, "  %s#%d\t%s\t%s\t%s\t%s\t%s\n", step.CaseID, step.Repeat, step.Step, step.Model, formatNumber(int64(step.Executions)), formatInt64(step.DurationMS), formatNumber(int64(step.InputTokens+step.OutputTokens)))
	}
	if len(s.AssistantSteps) == 0 {
		fmt.Fprintln(table, "  -\t-\t-\t-\t-\t-")
	}

	fmt.Fprintln(table, "\nASSISTANT SESSIONS")
	fmt.Fprintln(table, "  Case\tStep\tAttempt\tProvider attempt\tMode\tSession ID")
	for _, session := range s.AssistantSessions {
		mode := "fresh"
		if session.Resumed {
			mode = "resume"
		}
		fmt.Fprintf(table, "  %s#%d\t%s\t%d\t%d\t%s\t%s\n", session.CaseID, session.Repeat, session.Step, session.Attempt, session.ProviderAttempt, mode, session.SessionID)
	}
	if len(s.AssistantSessions) == 0 {
		fmt.Fprintln(table, "  -\t-\t-\t-\t-\t-")
	}

	fmt.Fprintln(table, "\nCASES")
	fmt.Fprintln(table, "  Case\tRepeat\tStatus\tOutcome\tNode attempts\tTokens\tDuration\tTime to valid")
	for _, item := range s.Cases {
		fmt.Fprintf(table, "  %s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n", item.CaseID, item.Repeat, item.Status, valueOrDash(item.Outcome), formatNumber(int64(item.Attempts)), formatNumber(int64(item.InputTokens+item.OutputTokens)), formatDurationMS(item.DurationMS), formatInt64(item.TimeToValidMS))
	}
	if len(s.Cases) == 0 {
		fmt.Fprintln(table, "  -\t-\t-\t-\t-\t-\t-\t-")
	}
	_ = table.Flush()
	return strings.TrimSpace(output.String())
}

func primaryRunCause(run RunRecord) (string, string) {
	if run.Status != "" && run.Status != "completed" {
		if run.Error != "" || run.ErrorCode != "" {
			return "runtime", joinCause(run.ErrorCode, run.Error)
		}
		if source, cause := firstFailedNodeCause(run); source != "" {
			return source, cause
		}
	}
	if run.Validation != nil {
		if run.Validation.Result != nil && len(run.Validation.Result.Diagnostics) > 0 {
			diagnostic := run.Validation.Result.Diagnostics[0]
			return "validator", joinCause(diagnostic.Code, diagnostic.Message)
		}
		if run.Validation.Error != "" || run.Validation.ErrorCode != "" {
			return "validator", joinCause(run.Validation.ErrorCode, run.Validation.Error)
		}
	}
	if run.Quality != nil && len(run.Quality.Diagnostics) > 0 {
		diagnostic := run.Quality.Diagnostics[0]
		return "quality", joinCause(diagnostic.Code, diagnostic.Message)
	}
	if run.Error != "" || run.ErrorCode != "" {
		return "runtime", joinCause(run.ErrorCode, run.Error)
	}
	nodeIDs := make([]string, 0, len(run.Nodes))
	for nodeID := range run.Nodes {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	for _, nodeID := range nodeIDs {
		node := run.Nodes[nodeID]
		if node.Error != "" || node.ErrorCode != "" {
			return "node:" + shortNodeID(nodeID), joinCause(node.ErrorCode, node.Error)
		}
	}
	return "", ""
}

func firstFailedNodeCause(run RunRecord) (string, string) {
	nodeIDs := make([]string, 0, len(run.Nodes))
	for nodeID := range run.Nodes {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	for _, nodeID := range nodeIDs {
		node := run.Nodes[nodeID]
		switch node.Status {
		case "failed", "errored", "cancelled", "timed_out", "blocked":
			if node.Error != "" || node.ErrorCode != "" {
				return "node:" + shortNodeID(nodeID), joinCause(node.ErrorCode, node.Error)
			}
		}
	}
	return "", ""
}

func joinCause(code, message string) string {
	if code == "" {
		return message
	}
	if message == "" {
		return code
	}
	return code + ": " + message
}

func formatCounts(values map[string]int) string {
	if len(values) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, values[key]))
	}
	return strings.Join(parts, ", ")
}

func shortFingerprint(value string) string {
	if len(value) <= 12 {
		return valueOrDash(value)
	}
	return value[:12]
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func formatNumber(value int64) string {
	raw := strconv.FormatInt(value, 10)
	start := 0
	if strings.HasPrefix(raw, "-") {
		start = 1
	}
	for index := len(raw) - 3; index > start; index -= 3 {
		raw = raw[:index] + " " + raw[index:]
	}
	return raw
}

func formatInt64(value *int64) string {
	if value == nil {
		return "-"
	}
	return formatDurationMS(*value)
}

func formatDurationMetric(value *float64) string {
	if value == nil {
		return "-"
	}
	return time.Duration(*value * float64(time.Millisecond)).String()
}

func formatDurationMS(value int64) string {
	return (time.Duration(value) * time.Millisecond).String()
}

func formatPercent(value *float64) string {
	if value == nil {
		return "-"
	}
	formatted := strconv.FormatFloat(*value*100, 'f', 1, 64)
	return strings.TrimSuffix(formatted, ".0") + "%"
}

func humanExecutionIdentity(identity string) (string, string) {
	fields := map[string]string{}
	for _, part := range strings.Split(identity, "|") {
		key, value, ok := strings.Cut(part, "=")
		if ok {
			fields[key] = value
		}
	}
	model := fields["requested"]
	if resolved := fields["resolved"]; resolved != "" && resolved != model {
		model += " -> " + resolved
	}
	if model == "" {
		model = identity
	}
	assistant := fields["assistant"]
	if version := fields["version"]; version != "" {
		assistant += "@" + version
	}
	return valueOrDash(model), valueOrDash(assistant)
}

func shortNodeID(value string) string {
	if index := strings.LastIndexByte(value, '/'); index >= 0 {
		return value[index+1:]
	}
	return value
}

func nodeModel(node NodeRecord) string {
	if node.RequestedModel == nil {
		return "-"
	}
	if node.RequestedModel.Name != "" {
		return node.RequestedModel.Name
	}
	return valueOrDash(modelKey(node.RequestedModel))
}
