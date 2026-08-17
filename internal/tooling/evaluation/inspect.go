package evaluation

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"takt/internal/store"
)

const InspectionReportVersion = "takt-evaluation-inspection/v1alpha1"

type EvaluationInspection struct {
	ReportVersion string           `json:"report_version"`
	OutputDir     string           `json:"output_dir"`
	Workflow      string           `json:"workflow"`
	Cases         []InspectionCase `json:"cases"`
}

type InspectionCase struct {
	CaseID          string                  `json:"case_id"`
	Repeat          int                     `json:"repeat"`
	RunID           string                  `json:"run_id,omitempty"`
	Status          string                  `json:"status"`
	Outcome         string                  `json:"outcome,omitempty"`
	Cause           InspectionCause         `json:"reported_cause"`
	Nodes           []InspectionNode        `json:"non_completed_nodes"`
	Evidence        InspectionEvidence      `json:"evidence"`
	CausalChain     []InspectionObservation `json:"causal_chain"`
	Observations    []InspectionObservation `json:"observations"`
	MissingEvidence []string                `json:"-"`
}

type InspectionCause struct {
	Confidence string `json:"confidence"`
	Source     string `json:"source,omitempty"`
	Message    string `json:"message,omitempty"`
}

type InspectionNode struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code,omitempty"`
	Error     string `json:"error,omitempty"`
}

type InspectionEvidence struct {
	Root             string   `json:"root"`
	Run              string   `json:"run,omitempty"`
	Validation       string   `json:"validation,omitempty"`
	Diff             string   `json:"diff,omitempty"`
	DiffBytes        int64    `json:"diff_bytes"`
	Source           string   `json:"source,omitempty"`
	SourcePresent    bool     `json:"source_present"`
	RepositoryBundle string   `json:"repository_bundle,omitempty"`
	Activity         string   `json:"activity,omitempty"`
	SCMCallsPath     string   `json:"scm_calls_path,omitempty"`
	SCMCallsRecorded bool     `json:"scm_calls_recorded"`
	SCMCalls         int      `json:"scm_calls"`
	Artifacts        []string `json:"artifacts"`
	ExecutorManifest string   `json:"executor_manifest,omitempty"`
}

type InspectionObservation struct {
	Code       string `json:"code"`
	Confidence string `json:"confidence"`
	Message    string `json:"message"`
	Evidence   string `json:"evidence,omitempty"`
}

type flowActivityEvidence struct {
	Events []flowActivityEvent `json:"events"`
}

type flowActivityEvent struct {
	Time     time.Time      `json:"time"`
	Type     string         `json:"type"`
	RunID    string         `json:"run_id"`
	NodeID   string         `json:"node_id,omitempty"`
	Revision uint64         `json:"revision"`
	Tool     string         `json:"tool,omitempty"`
	Input    map[string]any `json:"input,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

func InspectFlowEvaluation(outputDir, caseID string, repeat int) (*EvaluationInspection, error) {
	if repeat < 0 {
		return nil, fmt.Errorf("repeat cannot be negative")
	}
	report, err := LoadReport(outputDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		progress, progressErr := LoadFlowProgress(outputDir)
		if progressErr == nil && progress.Status == "running" {
			return inspectRunningFlowProgress(progress, caseID, repeat)
		}
		return nil, err
	}
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, err
	}
	abs, err = canonicalPath(abs)
	if err != nil {
		return nil, err
	}
	inspection := &EvaluationInspection{ReportVersion: InspectionReportVersion, OutputDir: report.OutputDir, Workflow: report.Workflow, Cases: []InspectionCase{}}
	for _, run := range report.Runs {
		if caseID != "" && run.CaseID != caseID || repeat > 0 && run.Repeat != repeat {
			continue
		}
		item, err := inspectFlowCase(abs, run)
		if err != nil {
			return nil, err
		}
		inspection.Cases = append(inspection.Cases, item)
	}
	sort.SliceStable(inspection.Cases, func(i, j int) bool {
		if inspection.Cases[i].CaseID != inspection.Cases[j].CaseID {
			return inspection.Cases[i].CaseID < inspection.Cases[j].CaseID
		}
		return inspection.Cases[i].Repeat < inspection.Cases[j].Repeat
	})
	if len(inspection.Cases) == 0 {
		return nil, fmt.Errorf("no evaluation cases match case=%q repeat=%d", caseID, repeat)
	}
	return inspection, nil
}

func inspectRunningFlowProgress(progress *FlowProgress, caseID string, repeat int) (*EvaluationInspection, error) {
	if progress == nil || progress.Current == nil {
		return nil, fmt.Errorf("running evaluation has no current case")
	}
	current := progress.Current
	if caseID != "" && caseID != current.CaseID || repeat > 0 && repeat != current.Repeat {
		return nil, fmt.Errorf("no evaluation cases match case=%q repeat=%d", caseID, repeat)
	}
	status := progress.Runtime.Status
	if status == "" {
		status = progress.Status
	}
	nodes := make([]InspectionNode, 0, len(progress.Runtime.RunningNodes))
	for _, id := range progress.Runtime.RunningNodes {
		nodes = append(nodes, InspectionNode{ID: id, Status: "running"})
	}
	message := fmt.Sprintf("evaluation is still running: phase=%s nodes=%d/%d completed; evidence is available after the case checkpoint", current.Phase, progress.Runtime.CompletedNodes, progress.Runtime.TotalNodes)
	return &EvaluationInspection{
		ReportVersion: InspectionReportVersion,
		OutputDir:     progress.OutputDir,
		Workflow:      progress.Workflow,
		Cases: []InspectionCase{{
			CaseID: current.CaseID, Repeat: current.Repeat, RunID: progress.Runtime.RunID, Status: status,
			Cause: InspectionCause{Confidence: "UNAVAILABLE", Source: "runtime", Message: message},
			Nodes: nodes, Evidence: InspectionEvidence{Artifacts: []string{}}, CausalChain: []InspectionObservation{},
			Observations: []InspectionObservation{{Code: "live_progress", Confidence: "UNAVAILABLE", Message: message}},
		}},
	}, nil
}

func inspectFlowCase(output string, run RunRecord) (InspectionCase, error) {
	var err error
	source, cause := primaryRunCause(run)
	confidence := "CONFIRMED"
	if cause == "" {
		confidence = "UNAVAILABLE"
	}
	item := InspectionCase{CaseID: run.CaseID, Repeat: run.Repeat, RunID: run.RunID, Status: run.Status, Outcome: run.Outcome, Cause: InspectionCause{Confidence: confidence, Source: source, Message: cause}, Nodes: []InspectionNode{}, CausalChain: []InspectionObservation{}, Observations: []InspectionObservation{}}
	nodeIDs := make([]string, 0, len(run.Nodes))
	for id := range run.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	for _, id := range nodeIDs {
		node := run.Nodes[id]
		if node.Status == "completed" {
			continue
		}
		item.Nodes = append(item.Nodes, InspectionNode{ID: shortNodeID(id), Status: node.Status, ErrorCode: node.ErrorCode, Error: node.Error})
	}

	repeatRoot := filepath.Join(output, "cases", run.CaseID, fmt.Sprintf("repeat-%03d", run.Repeat))
	if err := validateInspectionEvidencePath(output, repeatRoot); err != nil {
		return item, err
	}
	item.Evidence = InspectionEvidence{Root: relativeEvidencePath(output, repeatRoot), Artifacts: []string{}}
	item.Evidence.Run, err = existingEvidencePath(output, filepath.Join(repeatRoot, "run.json"))
	if err != nil {
		return item, err
	}
	if item.Evidence.Run != "" {
		chain, err := inspectRunCausalChain(filepath.Join(repeatRoot, "run.json"), item.Evidence.Run)
		if err != nil {
			return item, err
		}
		item.CausalChain = chain
	}
	item.Evidence.Validation, err = existingEvidencePath(output, filepath.Join(repeatRoot, "validation-result.json"))
	if err != nil {
		return item, err
	}
	item.Evidence.Diff, err = existingEvidencePath(output, filepath.Join(repeatRoot, "diff.patch"))
	if err != nil {
		return item, err
	}
	if info, err := os.Stat(filepath.Join(repeatRoot, "diff.patch")); err == nil {
		item.Evidence.DiffBytes = info.Size()
	} else if !os.IsNotExist(err) {
		return item, err
	}
	sourcePath := filepath.Join(repeatRoot, "source")
	if err := validateInspectionEvidencePath(output, sourcePath); err != nil {
		return item, err
	}
	if info, err := os.Stat(sourcePath); err == nil && info.IsDir() {
		item.Evidence.Source, item.Evidence.SourcePresent = relativeEvidencePath(output, sourcePath), true
	} else if err != nil && !os.IsNotExist(err) {
		return item, err
	}
	item.Evidence.RepositoryBundle, err = existingEvidencePath(output, filepath.Join(repeatRoot, "repository.bundle"))
	if err != nil {
		return item, err
	}
	item.Evidence.Activity, err = existingEvidencePath(output, filepath.Join(repeatRoot, "activity.json"))
	if err != nil {
		return item, err
	}
	manifestEvidence, err := existingEvidencePath(output, filepath.Join(repeatRoot, "executor-manifest.json"))
	if err != nil {
		return item, err
	}
	if manifestEvidence == "" {
		item.MissingEvidence = append(item.MissingEvidence, "executor-manifest.json")
	} else {
		item.Evidence.ExecutorManifest = manifestEvidence
		data, readErr := os.ReadFile(filepath.Join(repeatRoot, "executor-manifest.json"))
		if readErr != nil {
			return item, readErr
		}
		var executor FlowExecutorManifest
		if decodeErr := json.Unmarshal(data, &executor); decodeErr != nil {
			return item, fmt.Errorf("decode executor manifest: %w", decodeErr)
		}
		if executor.ReportVersion != FlowExecutorManifestVersion {
			return item, fmt.Errorf("unsupported executor manifest version %q", executor.ReportVersion)
		}
	}
	callsPath := filepath.Join(repeatRoot, "scm", "calls.log")
	if err := validateInspectionEvidencePath(output, callsPath); err != nil {
		return item, err
	}
	if data, err := os.ReadFile(callsPath); err == nil {
		item.Evidence.SCMCallsPath = relativeEvidencePath(output, callsPath)
		item.Evidence.SCMCallsRecorded = true
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) != "" {
				item.Evidence.SCMCalls++
			}
		}
	} else if !os.IsNotExist(err) {
		return item, err
	}
	manifestPath := filepath.Join(repeatRoot, "artifacts", "manifest.json")
	if err := validateInspectionEvidencePath(output, manifestPath); err != nil {
		return item, err
	}
	if data, err := os.ReadFile(manifestPath); err == nil {
		var manifest flowArtifactManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return item, fmt.Errorf("decode artifact manifest: %w", err)
		}
		for _, artifact := range manifest.Artifacts {
			evidencePath := filepath.FromSlash(artifact.EvidencePath)
			if filepath.IsAbs(evidencePath) || filepath.VolumeName(evidencePath) != "" {
				return item, fmt.Errorf("artifact evidence path escapes evaluation output: %s", artifact.EvidencePath)
			}
			artifactPath := filepath.Join(repeatRoot, "artifacts", evidencePath)
			if err := validateInspectionEvidencePath(output, artifactPath); err != nil {
				return item, err
			}
			item.Evidence.Artifacts = append(item.Evidence.Artifacts, relativeEvidencePath(output, artifactPath))
		}
	} else if !os.IsNotExist(err) {
		return item, err
	}
	if item.Evidence.Activity != "" && item.Evidence.Run != "" {
		observations, err := inspectWorkspaceActivity(output, repeatRoot)
		if err != nil {
			return item, err
		}
		item.Observations = append(item.Observations, observations...)
	}
	return item, nil
}

type assistantTraceSummary struct {
	OutputLimitTokens int
	Tools             map[string]int
}

func inspectRunCausalChain(path, evidence string) ([]InspectionObservation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var runs flowRunEvidence
	if err := json.Unmarshal(data, &runs); err != nil {
		return nil, fmt.Errorf("decode run evidence: %w", err)
	}
	chain := []InspectionObservation{}
	skipped := []string{}
	for _, state := range runs.States {
		if state == nil {
			continue
		}
		nodeIDs := make([]string, 0, len(state.Nodes))
		for id := range state.Nodes {
			nodeIDs = append(nodeIDs, id)
		}
		sort.Strings(nodeIDs)
		for _, id := range nodeIDs {
			node := state.Nodes[id]
			if node == nil {
				continue
			}
			shortID := shortNodeID(id)
			trace := inspectAssistantTrace(node.Stdout)
			if trace.OutputLimitTokens > 0 {
				chain = append(chain, InspectionObservation{Code: "assistant_output_limit", Confidence: "CONFIRMED", Message: fmt.Sprintf("node %s reached model output limit after %d output tokens", shortID, trace.OutputLimitTokens), Evidence: evidence})
				if len(trace.Tools) > 0 && !hasDirectWriteTool(trace.Tools) {
					chain = append(chain, InspectionObservation{Code: "no_direct_write_tools", Confidence: "CONFIRMED", Message: fmt.Sprintf("node %s started tools %s but no direct file-write tool", shortID, formatToolCounts(trace.Tools)), Evidence: evidence})
				}
				if node.Status == store.NodeCompleted && strings.TrimSpace(node.Output) == "" {
					chain = append(chain, InspectionObservation{Code: "completed_without_result", Confidence: "CONFIRMED", Message: fmt.Sprintf("node %s was recorded completed with empty assistant output", shortID), Evidence: evidence})
				}
			}
			if shortID == "validate" && node.FailedLike() {
				chain = append(chain, InspectionObservation{Code: "deterministic_validation_failed", Confidence: "CONFIRMED", Message: fmt.Sprintf("node %s %s: %s", shortID, node.Status, valueOrDash(joinCause(node.ErrorCode, node.Error))), Evidence: evidence})
			}
			if node.Status == store.NodeSkipped {
				skipped = append(skipped, shortID)
			}
		}
	}
	if len(skipped) > 0 {
		sort.Strings(skipped)
		chain = append(chain, InspectionObservation{Code: "downstream_skipped", Confidence: "CONFIRMED", Message: "downstream nodes were skipped: " + strings.Join(skipped, ", "), Evidence: evidence})
	}
	return chain, nil
}

func inspectAssistantTrace(stdout string) assistantTraceSummary {
	summary := assistantTraceSummary{Tools: map[string]int{}}
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var record struct {
			Type     string `json:"type"`
			ToolName string `json:"toolName"`
			Message  struct {
				Role       string `json:"role"`
				StopReason string `json:"stopReason"`
				Content    []struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"content"`
				Usage struct {
					Output int `json:"output"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			continue
		}
		if record.Type == "tool_execution_start" && record.ToolName != "" {
			summary.Tools[record.ToolName]++
		}
		if record.Type == "message_end" && record.Message.Role == "assistant" {
			for _, content := range record.Message.Content {
				if content.Type == "toolCall" && content.Name != "" {
					summary.Tools[content.Name]++
				}
			}
		}
		if record.Type == "message_end" && record.Message.Role == "assistant" && record.Message.StopReason == "length" {
			summary.OutputLimitTokens = record.Message.Usage.Output
		}
	}
	return summary
}

func hasDirectWriteTool(tools map[string]int) bool {
	for _, name := range []string{"write", "edit", "apply_patch"} {
		if tools[name] > 0 {
			return true
		}
	}
	return false
}

func formatToolCounts(tools map[string]int) string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	values := make([]string, 0, len(names))
	for _, name := range names {
		values = append(values, fmt.Sprintf("%s=%d", name, tools[name]))
	}
	return strings.Join(values, ", ")
}

func inspectWorkspaceActivity(output, repeatRoot string) ([]InspectionObservation, error) {
	activityData, err := os.ReadFile(filepath.Join(repeatRoot, "activity.json"))
	if err != nil {
		return nil, err
	}
	var activity flowActivityEvidence
	if err := json.Unmarshal(activityData, &activity); err != nil {
		return nil, fmt.Errorf("decode activity evidence: %w", err)
	}
	runData, err := os.ReadFile(filepath.Join(repeatRoot, "run.json"))
	if err != nil {
		return nil, err
	}
	var runs flowRunEvidence
	if err := json.Unmarshal(runData, &runs); err != nil {
		return nil, fmt.Errorf("decode run evidence: %w", err)
	}
	workspaces := map[string][2]string{}
	for _, state := range runs.States {
		if state != nil {
			workspaces[state.ID] = [2]string{state.Workspace, state.ExecutionWorkspace}
		}
	}
	for _, event := range activity.Events {
		if event.Type != "assistant.tool.started" || event.Tool != "write" && event.Tool != "edit" {
			continue
		}
		path, _ := event.Input["path"].(string)
		workspace := workspaces[event.RunID]
		if path != "" && workspace[1] != "" && !filepath.IsAbs(path) {
			path = filepath.Join(workspace[1], path)
		}
		if path != "" && workspace[0] != "" && workspace[1] != "" && workspace[0] != workspace[1] && pathContains(workspace[0], path) && !pathContains(workspace[1], path) {
			artifacts := filepath.Join(workspace[0], ".takt", "runs", event.RunID, "artifacts")
			if pathContains(artifacts, path) {
				continue
			}
			return []InspectionObservation{{Code: "control_workspace_mutation", Confidence: "CONFIRMED", Message: "assistant mutation targeted control workspace instead of execution workspace", Evidence: relativeEvidencePath(output, filepath.Join(repeatRoot, "activity.json"))}}, nil
		}
	}
	return nil, nil
}

func existingEvidencePath(root, path string) (string, error) {
	if err := validateInspectionEvidencePath(root, path); err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return relativeEvidencePath(root, path), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return "", nil
}

func validateInspectionEvidencePath(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if !pathContains(root, path) {
		return fmt.Errorf("evidence path escapes evaluation output: %s", path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("evidence path contains symlink: %s", current)
		}
	}
	return nil
}

func relativeEvidencePath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func (i EvaluationInspection) String() string {
	var output strings.Builder
	table := tabwriter.NewWriter(&output, 0, 2, 2, ' ', 0)
	fmt.Fprintln(table, "INSPECTION")
	fmt.Fprintf(table, "  Directory\t%s\n", i.OutputDir)
	fmt.Fprintf(table, "  Workflow\t%s\n", valueOrDash(i.Workflow))
	fmt.Fprintf(table, "  Cases\t%d\n", len(i.Cases))
	for _, item := range i.Cases {
		fmt.Fprintf(table, "\nCASE %s#%d\n", item.CaseID, item.Repeat)
		fmt.Fprintf(table, "  Run\t%s\n", valueOrDash(item.RunID))
		fmt.Fprintf(table, "  Status\t%s\n", valueOrDash(item.Status))
		fmt.Fprintf(table, "  Outcome\t%s\n", valueOrDash(item.Outcome))
		fmt.Fprintln(table, "\n  REPORTED CAUSE")
		fmt.Fprintf(table, "    %s\t%s\t%s\n", item.Cause.Confidence, valueOrDash(item.Cause.Source), valueOrDash(item.Cause.Message))
		fmt.Fprintln(table, "\n  CAUSAL CHAIN")
		for index, observation := range item.CausalChain {
			fmt.Fprintf(table, "    %d\t%s\t%s\t%s\n", index+1, observation.Code, observation.Confidence, observation.Message)
		}
		if len(item.CausalChain) == 0 {
			fmt.Fprintln(table, "    -\t-\t-\t-")
		}
		fmt.Fprintln(table, "\n  NODES")
		fmt.Fprintln(table, "    Node\tStatus\tCause")
		for _, node := range item.Nodes {
			fmt.Fprintf(table, "    %s\t%s\t%s\n", node.ID, node.Status, valueOrDash(joinCause(node.ErrorCode, node.Error)))
		}
		if len(item.Nodes) == 0 {
			fmt.Fprintln(table, "    -\t-\t-")
		}
		fmt.Fprintln(table, "\n  EVIDENCE")
		fmt.Fprintf(table, "    Root\t%s\n", valueOrDash(item.Evidence.Root))
		fmt.Fprintf(table, "    Run\t%s\n", valueOrDash(item.Evidence.Run))
		fmt.Fprintf(table, "    Validation\t%s\n", valueOrDash(item.Evidence.Validation))
		fmt.Fprintf(table, "    Diff\t%s (%d bytes)\n", valueOrDash(item.Evidence.Diff), item.Evidence.DiffBytes)
		fmt.Fprintf(table, "    Source\t%s\n", valueOrDash(item.Evidence.Source))
		fmt.Fprintf(table, "    Git bundle\t%s\n", valueOrDash(item.Evidence.RepositoryBundle))
		fmt.Fprintf(table, "    Activity\t%s\n", valueOrDash(item.Evidence.Activity))
		manifest := item.Evidence.ExecutorManifest
		if manifest == "" {
			manifest = "UNAVAILABLE"
		}
		fmt.Fprintf(table, "    Executor manifest\t%s\n", manifest)
		if item.Evidence.SCMCallsRecorded {
			fmt.Fprintf(table, "    SCM calls\t%d (%s)\n", item.Evidence.SCMCalls, item.Evidence.SCMCallsPath)
		} else {
			fmt.Fprintln(table, "    SCM calls\t0 (not recorded)")
		}
		fmt.Fprintf(table, "    Artifacts\t%d\n", len(item.Evidence.Artifacts))
		for _, artifact := range item.Evidence.Artifacts {
			fmt.Fprintf(table, "    Artifact\t%s\n", artifact)
		}
		fmt.Fprintln(table, "\n  OBSERVATIONS")
		for _, observation := range item.Observations {
			fmt.Fprintf(table, "    %s\t%s\t%s\n", observation.Code, observation.Confidence, observation.Message)
		}
		if len(item.Observations) == 0 {
			fmt.Fprintln(table, "    -\t-\t-")
		}
	}
	_ = table.Flush()
	return strings.TrimSpace(output.String())
}
