package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"takt/internal/dynamicplan"
	"takt/internal/evidence"
)

func ensureEvidence(record *dynamicplan.Record) *evidence.Manifest {
	if record.Evidence == nil {
		record.Evidence = evidence.NewManifest()
	}
	if record.Evidence.Acceptance == nil {
		record.Evidence.Acceptance = map[string]evidence.Acceptance{}
	}
	return record.Evidence
}

func captureBaselineEvidence(record *dynamicplan.Record, phaseID, output, candidateSHA string) error {
	var value struct {
		BaseRef           string   `json:"base_ref"`
		PassedChecks      []string `json:"passed_checks"`
		KnownFailures     []string `json:"known_failures"`
		UnavailableChecks []string `json:"unavailable_checks"`
		Evidence          []string `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(output), &value); err != nil {
		return fmt.Errorf("decode baseline evidence: %w", err)
	}
	fingerprints := make([]string, 0, len(value.KnownFailures))
	for _, failure := range value.KnownFailures {
		fingerprints = append(fingerprints, evidence.FailureFingerprint(failure))
	}
	manifest := ensureEvidence(record)
	manifest.Baseline = &evidence.Baseline{
		PhaseID: phaseID, BaseRef: strings.TrimSpace(value.BaseRef), CandidateSHA: candidateSHA,
		PassedChecks: append([]string(nil), value.PassedChecks...), KnownFailures: append([]string(nil), value.KnownFailures...),
		FailureFingerprints: fingerprints, UnavailableChecks: append([]string(nil), value.UnavailableChecks...),
		Evidence: append([]string(nil), value.Evidence...), CapturedAt: time.Now().UTC(),
	}
	manifest.UpdatedAt = time.Now().UTC()
	return nil
}

func outputIssues(output string) []string {
	var value map[string]any
	if json.Unmarshal([]byte(output), &value) != nil {
		return nil
	}
	raw, ok := value["issues"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			out = append(out, strings.TrimSpace(text))
		}
	}
	return out
}

func outputEvidence(output string) []string {
	var value map[string]any
	if json.Unmarshal([]byte(output), &value) != nil {
		return nil
	}
	return evidence.EvidenceStrings(value)
}

func recordAcceptance(record *dynamicplan.Record, phaseID, block, check, status, failureCode, detail, candidateSHA string, proof []string) {
	manifest := ensureEvidence(record)
	id := evidence.AcceptanceID(block, check)
	manifest.Acceptance[id] = evidence.Acceptance{
		ID: id, Block: block, Check: check, PhaseID: phaseID, Status: status, FailureCode: failureCode,
		Detail: detail, Evidence: append([]string(nil), proof...), CandidateSHA: candidateSHA,
	}
	manifest.CandidateSHA = candidateSHA
	manifest.UpdatedAt = time.Now().UTC()
}

func finalizeEvidence(record *dynamicplan.Record, candidateSHA string) {
	manifest := ensureEvidence(record)
	status := evidence.VerdictPass
	reason := "all required evidence is satisfied for the current candidate"
	for _, item := range manifest.Acceptance {
		if item.Status == "failed" || item.Status == "unavailable" {
			status = evidence.VerdictFail
			reason = "one or more required evidence items are not satisfied"
			break
		}
	}
	manifest.CandidateSHA = candidateSHA
	manifest.Verdict = &evidence.Verdict{Status: status, CandidateSHA: candidateSHA, Reason: reason, CreatedAt: time.Now().UTC()}
	manifest.UpdatedAt = time.Now().UTC()
}

func parkPlan(record *dynamicplan.Record, code, message, owner, next string, retryable bool, unsafe ...string) {
	now := time.Now().UTC()
	record.Status = "parked"
	record.CurrentRunID = ""
	record.LastError = message
	record.ParkedAt = &now
	record.Failure = &evidence.Failure{Code: code, Message: message, Owner: owner, Retryable: retryable, SafeNextAction: next, UnsafeToRepeat: append([]string(nil), unsafe...), CreatedAt: now}
	record.UpdatedAt = now
}

func clearPlanFailure(record *dynamicplan.Record) {
	record.Failure = nil
	record.ParkedAt = nil
}

func (s *Service) dynamicCandidateSHA(ctx context.Context, record *dynamicplan.Record) (string, error) {
	if record == nil || strings.TrimSpace(record.ExecutionWorkspace) == "" || strings.TrimSpace(record.ExecutionBaseCommit) == "" {
		return "", nil
	}
	workspace := filepath.Clean(record.ExecutionWorkspace)
	h := sha256.New()
	_, _ = h.Write([]byte("base\x00" + record.ExecutionBaseCommit + "\x00"))
	cmd := exec.CommandContext(ctx, "git", "-C", workspace, "diff", "--binary", record.ExecutionBaseCommit, "--")
	diff, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("hash dynamic candidate diff: %w", err)
	}
	_, _ = h.Write(diff)
	cmd = exec.CommandContext(ctx, "git", "-C", workspace, "ls-files", "--others", "--exclude-standard", "-z")
	raw, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("hash dynamic candidate untracked files: %w", err)
	}
	var paths []string
	for _, item := range strings.Split(string(raw), "\x00") {
		if strings.TrimSpace(item) != "" {
			paths = append(paths, filepath.ToSlash(item))
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		full := filepath.Join(workspace, filepath.FromSlash(path))
		info, err := os.Stat(full)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			continue
		}
		content, err := os.ReadFile(full)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(content)
		_, _ = h.Write([]byte("untracked\x00" + path + "\x00" + hex.EncodeToString(sum[:]) + "\x00"))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
