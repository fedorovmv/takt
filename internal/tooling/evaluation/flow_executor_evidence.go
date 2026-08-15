package evaluation

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"takt/internal/redact"
	"takt/internal/store"
)

const FlowExecutorManifestVersion = "takt-evaluation-executor/v1alpha1"

const (
	maxSessionEvidenceBytes = 4 << 20
	maxSessionEvidenceTotal = 32 << 20
)

type FlowExecutorManifest struct {
	ReportVersion string                  `json:"report_version"`
	CaseID        string                  `json:"case_id"`
	Repeat        int                     `json:"repeat"`
	Executions    []FlowExecutorExecution `json:"executions"`
}

type FlowExecutorExecution struct {
	RunID                 string          `json:"run_id"`
	NodeID                string          `json:"node_id"`
	Attempt               int             `json:"attempt"`
	ProviderAttempt       int             `json:"provider_attempt"`
	Assistant             string          `json:"assistant,omitempty"`
	Adapter               string          `json:"adapter,omitempty"`
	AssistantVersion      string          `json:"assistant_version,omitempty"`
	RequestedModel        *store.ModelRef `json:"requested_model,omitempty"`
	ResolvedModel         *store.ModelRef `json:"resolved_model,omitempty"`
	SessionID             string          `json:"session_id,omitempty"`
	SessionPath           string          `json:"session_path,omitempty"`
	SessionEvidencePath   string          `json:"session_evidence_path,omitempty"`
	SessionEvidence       string          `json:"session_evidence"`
	SessionEvidenceReason string          `json:"session_evidence_reason,omitempty"`
	Resumed               bool            `json:"resumed"`
}

type flowExecutorRecord struct {
	runID, nodeID string
	execution     store.ExecutionState
}

func writeFlowExecutorManifest(repeatRoot string, item FlowEvidence, redactor *redact.Redactor) error {
	records := collectFlowExecutorRecords(item.States)
	manifest := FlowExecutorManifest{ReportVersion: FlowExecutorManifestVersion, CaseID: item.CaseID, Repeat: item.Repeat, Executions: make([]FlowExecutorExecution, 0, len(records))}
	var total int64
	aggregateExceeded := false
	usedDestinations := map[string]int{}
	for _, record := range records {
		exec := record.execution
		entry := FlowExecutorExecution{
			RunID: record.runID, NodeID: record.nodeID, Attempt: exec.Attempt, ProviderAttempt: exec.ProviderAttempt,
			Assistant: exec.Assistant, Adapter: exec.Adapter, AssistantVersion: exec.AssistantVersion,
			RequestedModel: exec.RequestedModel, ResolvedModel: exec.ResolvedModel, SessionID: exec.SessionID,
			SessionPath: exec.SessionPath, SessionEvidence: "unavailable", Resumed: exec.Resumed,
		}
		if exec.SessionPath == "" {
			entry.SessionEvidenceReason = "adapter_did_not_expose_path"
			manifest.Executions = append(manifest.Executions, entry)
			continue
		}
		if aggregateExceeded {
			entry.SessionEvidenceReason = "aggregate_limit"
			manifest.Executions = append(manifest.Executions, entry)
			continue
		}
		if !filepath.IsAbs(exec.SessionPath) {
			entry.SessionEvidenceReason = "path_not_absolute"
			manifest.Executions = append(manifest.Executions, entry)
			continue
		}
		info, err := os.Lstat(exec.SessionPath)
		if err != nil {
			if os.IsNotExist(err) {
				entry.SessionEvidenceReason = "path_missing"
			} else {
				entry.SessionEvidenceReason = "path_unreadable"
			}
			manifest.Executions = append(manifest.Executions, entry)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			entry.SessionEvidenceReason = "path_symlink_forbidden"
			manifest.Executions = append(manifest.Executions, entry)
			continue
		}
		if !info.Mode().IsRegular() {
			entry.SessionEvidenceReason = "path_not_regular"
			manifest.Executions = append(manifest.Executions, entry)
			continue
		}
		if info.Size() > maxSessionEvidenceBytes {
			entry.SessionEvidenceReason = "path_too_large"
			manifest.Executions = append(manifest.Executions, entry)
			continue
		}
		data, err := os.ReadFile(exec.SessionPath)
		if err != nil {
			entry.SessionEvidenceReason = "path_unreadable"
			manifest.Executions = append(manifest.Executions, entry)
			continue
		}
		if len(data) > maxSessionEvidenceBytes {
			entry.SessionEvidenceReason = "path_too_large"
			manifest.Executions = append(manifest.Executions, entry)
			continue
		}
		persisted, matched := data, false
		if redactor != nil {
			persisted, matched = redactor.Bytes(data)
		}
		if matched && !utf8.Valid(data) {
			return fmt.Errorf("session file contains known secret in non-UTF-8 data: %s", exec.SessionPath)
		}
		if total+int64(len(persisted)) > maxSessionEvidenceTotal {
			aggregateExceeded = true
			entry.SessionEvidenceReason = "aggregate_limit"
			manifest.Executions = append(manifest.Executions, entry)
			continue
		}
		nodeDir := sanitizeExecutorNodeID(record.nodeID)
		baseName := fmt.Sprintf("attempt-%03d-provider-%03d.jsonl", exec.Attempt, exec.ProviderAttempt)
		destination := filepath.Join("sessions", nodeDir, baseName)
		if count := usedDestinations[destination]; count > 0 {
			for n := count + 1; ; n++ {
				candidate := filepath.Join("sessions", nodeDir+"-"+sanitizeExecutorNodeID(record.runID), fmt.Sprintf("attempt-%03d-provider-%03d-%d.jsonl", exec.Attempt, exec.ProviderAttempt, n))
				if usedDestinations[candidate] == 0 {
					destination = candidate
					break
				}
			}
		}
		usedDestinations[destination]++
		if err := writeFlowRaw(filepath.Join(repeatRoot, filepath.FromSlash(destination)), persisted, 0644); err != nil {
			return err
		}
		total += int64(len(persisted))
		entry.SessionEvidence, entry.SessionEvidencePath = "recorded", filepath.ToSlash(destination)
		manifest.Executions = append(manifest.Executions, entry)
	}
	return writeFlowJSON(filepath.Join(repeatRoot, "executor-manifest.json"), manifest, redactor)
}

func collectFlowExecutorRecords(states []*store.RunState) []flowExecutorRecord {
	var records []flowExecutorRecord
	var visit func(string, string, *store.NodeState)
	visit = func(runID, nodeID string, node *store.NodeState) {
		if node == nil {
			return
		}
		for _, execution := range node.Executions {
			records = append(records, flowExecutorRecord{runID: runID, nodeID: nodeID, execution: execution})
		}
		for _, iteration := range node.LoopIterations {
			for id, nested := range iteration.Nodes {
				visit(runID, fmt.Sprintf("%s/loop-%03d/%s", nodeID, iteration.Iteration, id), &nested)
			}
		}
	}
	for _, state := range states {
		if state == nil {
			continue
		}
		ids := make([]string, 0, len(state.Nodes))
		for id := range state.Nodes {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			visit(state.ID, id, state.Nodes[id])
		}
	}
	sort.SliceStable(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if a.runID != b.runID {
			return a.runID < b.runID
		}
		if a.nodeID != b.nodeID {
			return a.nodeID < b.nodeID
		}
		if a.execution.Attempt != b.execution.Attempt {
			return a.execution.Attempt < b.execution.Attempt
		}
		return a.execution.ProviderAttempt < b.execution.ProviderAttempt
	})
	return records
}

func sanitizeExecutorNodeID(value string) string {
	value = strings.Trim(strings.NewReplacer("\\", "-", "/", "-", "..", "-", " ", "-").Replace(value), "-.")
	if value == "" {
		return "node"
	}
	return value
}
