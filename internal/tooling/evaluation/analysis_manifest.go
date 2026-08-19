package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"takt/internal/redact"
)

type AnalysisEvidenceFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type AnalysisEvidenceManifest struct {
	Version              string                       `json:"version"`
	CaseID               string                       `json:"case_id"`
	Repeat               int                          `json:"repeat"`
	EvidenceRoot         string                       `json:"evidence_root"`
	Outcome              string                       `json:"outcome,omitempty"`
	StrategyFingerprint  string                       `json:"strategy_fingerprint,omitempty"`
	BenchmarkFingerprint string                       `json:"benchmark_fingerprint,omitempty"`
	ReportedCause        InspectionCause              `json:"reported_cause"`
	CausalChain          []InspectionObservation      `json:"causal_chain"`
	Observations         []InspectionObservation      `json:"observations"`
	NonCompletedNodes    []InspectionNode             `json:"non_completed_nodes"`
	DeterministicVerdict AnalysisDeterministicVerdict `json:"deterministic_verdict"`
	Deterministic        AnalysisDeterministicVerdict `json:"deterministic"`
	ValidatorStderrPath  string                       `json:"validator_stderr_path,omitempty"`
	ValidatorStderr      string                       `json:"validator_stderr,omitempty"`
	Files                []AnalysisEvidenceFile       `json:"files"`
	MissingEvidence      []string                     `json:"missing_evidence,omitempty"`
}

type AnalysisDeterministicVerdict struct {
	Status            string `json:"status,omitempty"`
	Outcome           string `json:"outcome,omitempty"`
	QualityNodeStatus string `json:"quality_node_status,omitempty"`
	Valid             *bool  `json:"valid,omitempty"`
	RunPassed         *bool  `json:"run_passed,omitempty"`
}
type AnalysisWorkspaceRef struct {
	CaseID           string `json:"case_id"`
	Repeat           int    `json:"repeat"`
	Workspace        string `json:"workspace"`
	EvidenceManifest string `json:"evidence_manifest"`
	Trace            string `json:"trace,omitempty"`
}
type AnalysisManifest struct {
	Version             string                 `json:"version"`
	SourceEvaluationDir string                 `json:"source_evaluation_dir"`
	SelectedCases       []AnalysisCaseRef      `json:"selected_cases"`
	ConfigFingerprint   string                 `json:"config_fingerprint"`
	Language            string                 `json:"language,omitempty"`
	Model               AnalysisModel          `json:"model"`
	Trace               string                 `json:"trace,omitempty"`
	Workspaces          []AnalysisWorkspaceRef `json:"workspaces"`
}

const analysisManifestVersion = "takt-evaluation-analysis-manifest/v1alpha1"

const (
	maxAnalysisEvidenceFiles     = 8192
	maxAnalysisEvidenceBytes     = 128 << 20
	maxAnalysisEvidenceFileBytes = 4 << 20
)

// copyAnalysisEvidenceRoot creates the model-visible evidence tree. Oversized
// files are omitted and returned as missing evidence; secrets in binary data
// fail closed rather than persisting an unsafe artifact.
func copyAnalysisEvidenceRoot(source, destination string, redactor *redact.Redactor) ([]string, error) {
	var missing []string
	var total int64
	count := 0
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(destination, 0755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("evidence path is a symlink: %s", rel)
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(destination, rel), 0755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("evidence path is not a regular file: %s", rel)
		}
		if info.Size() > maxAnalysisEvidenceFileBytes || count >= maxAnalysisEvidenceFiles {
			missing = append(missing, filepath.ToSlash(rel))
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if redactor != nil {
			redacted, matched := redactor.Bytes(data)
			if matched && !isTextEvidence(data) {
				return fmt.Errorf("binary evidence contains known secret: %s", rel)
			}
			data = redacted
		}
		if len(data) > maxAnalysisEvidenceFileBytes || total+int64(len(data)) > maxAnalysisEvidenceBytes {
			missing = append(missing, filepath.ToSlash(rel))
			return nil
		}
		if err := writeFlowRaw(filepath.Join(destination, rel), data, info.Mode()); err != nil {
			return err
		}
		total += int64(len(data))
		count++
		return nil
	})
	sort.Strings(missing)
	return missing, err
}

func buildAnalysisEvidenceManifest(output, repeatRoot string, inspection *InspectionCase, run RunRecord, reports ...*SuiteReport) (AnalysisEvidenceManifest, error) {
	m := AnalysisEvidenceManifest{
		Version: analysisManifestVersion, CaseID: run.CaseID, Repeat: run.Repeat, EvidenceRoot: "",
		CausalChain: []InspectionObservation{}, Observations: []InspectionObservation{}, NonCompletedNodes: []InspectionNode{},
	}
	m.Outcome = run.Outcome
	verdict := AnalysisDeterministicVerdict{Status: run.Status, Outcome: run.Outcome, QualityNodeStatus: run.QualityNodeStatus, Valid: validationValid(run.Validation), RunPassed: run.RunPassed}
	m.DeterministicVerdict = verdict
	m.Deterministic = verdict
	if len(reports) > 0 && reports[0] != nil {
		m.StrategyFingerprint = reports[0].Strategy.Fingerprint
		m.BenchmarkFingerprint = reports[0].Benchmark.Fingerprint
	}
	if inspection != nil {
		m.ReportedCause = inspection.Cause
		m.CausalChain = append(m.CausalChain, inspection.CausalChain...)
		m.Observations = append(m.Observations, inspection.Observations...)
		m.NonCompletedNodes = append(m.NonCompletedNodes, inspection.Nodes...)
	}
	if rel, err := filepath.Rel(output, repeatRoot); err == nil {
		m.EvidenceRoot = filepath.ToSlash(rel)
	}
	paths := []string{"run.json"}
	for _, p := range []string{"validation-request.json", "validation-result.json", "validator.stderr", "diff.patch", "activity.json", "executor-manifest.json", "repository.bundle", "source", "source-unavailable.txt", "sessions", "scm", "artifacts"} {
		if _, err := os.Stat(filepath.Join(repeatRoot, p)); err == nil {
			paths = append(paths, p)
		}
	}
	if inspection != nil {
		for _, raw := range inspection.MissingEvidence {
			rel, err := normalizeInspectionEvidencePath(inspection.Evidence.Root, raw)
			if err != nil {
				return m, err
			}
			if rel == "" {
				continue
			}
			if _, err := os.Lstat(filepath.Join(repeatRoot, filepath.FromSlash(rel))); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return m, err
			}
			m.MissingEvidence = append(m.MissingEvidence, rel)
		}
		for _, p := range []string{"validation-request.json", "validation-result.json", "validator.stderr", "diff.patch", "activity.json", "executor-manifest.json", "repository.bundle"} {
			if _, err := os.Stat(filepath.Join(repeatRoot, p)); os.IsNotExist(err) {
				m.MissingEvidence = append(m.MissingEvidence, p)
			}
		}
	}
	if info, err := os.Stat(filepath.Join(repeatRoot, "validator.stderr")); err == nil && info.Mode().IsRegular() {
		m.ValidatorStderrPath = "validator.stderr"
		if data, readErr := os.ReadFile(filepath.Join(repeatRoot, "validator.stderr")); readErr == nil {
			m.ValidatorStderr = string(data)
		}
	}
	sort.Strings(paths)
	seen := map[string]bool{}
	var totalBytes int64
	for _, rel := range paths {
		rel = filepath.ToSlash(rel)
		if seen[rel] {
			continue
		}
		seen[rel] = true
		if strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			return m, fmt.Errorf("evidence path must be relative: %s", rel)
		}
		path := filepath.Join(repeatRoot, filepath.FromSlash(rel))
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				m.MissingEvidence = append(m.MissingEvidence, rel)
				continue
			}
			return m, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return m, fmt.Errorf("evidence path is a symlink: %s", rel)
		}
		if info.IsDir() {
			err := filepath.Walk(path, func(child string, fi os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if fi.IsDir() {
					return nil
				}
				if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
					return fmt.Errorf("evidence path is not a regular file: %s", child)
				}
				if len(m.Files) >= maxAnalysisEvidenceFiles {
					return fmt.Errorf("analysis evidence file limit exceeded")
				}
				if totalBytes+fi.Size() > maxAnalysisEvidenceBytes {
					return fmt.Errorf("analysis evidence byte limit exceeded")
				}
				r, e := filepath.Rel(repeatRoot, child)
				if e != nil {
					return e
				}
				data, e := os.ReadFile(child)
				if e != nil {
					return e
				}
				h := sha256Bytes(data)
				m.Files = append(m.Files, AnalysisEvidenceFile{Path: filepath.ToSlash(r), Size: fi.Size(), SHA256: h})
				totalBytes += fi.Size()
				return nil
			})
			if err != nil {
				return m, err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return m, fmt.Errorf("evidence path is not a regular file: %s", rel)
		}
		if len(m.Files) >= maxAnalysisEvidenceFiles {
			return m, fmt.Errorf("analysis evidence file limit exceeded")
		}
		if totalBytes+info.Size() > maxAnalysisEvidenceBytes {
			return m, fmt.Errorf("analysis evidence byte limit exceeded")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return m, err
		}
		m.Files = append(m.Files, AnalysisEvidenceFile{Path: rel, Size: info.Size(), SHA256: sha256Bytes(data)})
		totalBytes += info.Size()
	}
	sort.Strings(m.MissingEvidence)
	m.MissingEvidence = uniqueAnalysisStrings(m.MissingEvidence)
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	return m, nil
}

func normalizeInspectionEvidencePath(root, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.ContainsRune(raw, '\x00') || strings.Contains(raw, "\\") || filepath.IsAbs(raw) {
		return "", fmt.Errorf("inspection evidence path must be relative: %q", raw)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(raw)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("inspection evidence path escapes repeat root: %q", raw)
	}
	root = filepath.ToSlash(strings.Trim(strings.TrimSpace(root), "/"))
	if root != "" {
		if clean == root {
			return "", nil
		}
		prefix := root + "/"
		if strings.HasPrefix(clean, prefix) {
			clean = strings.TrimPrefix(clean, prefix)
		}
	}
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", nil
	}
	return clean, nil
}

func validationValid(record *FlowValidationRecord) *bool {
	if record == nil || record.Result == nil {
		return nil
	}
	value := record.Result.Valid
	return &value
}

func analysisAtomicJSON(path string, value any) error {
	b, err := jsonMarshalIndent(value)
	if err != nil {
		return err
	}
	return analysisAtomicBytes(path, b)
}

func analysisAtomicJSONRedacted(path string, value any, redactor *redact.Redactor) error {
	if redactor == nil {
		return analysisAtomicJSON(path, value)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var structured any
	if err := json.Unmarshal(data, &structured); err != nil {
		return err
	}
	data, err = json.MarshalIndent(redactor.Any(structured), "", "  ")
	if err != nil {
		return err
	}
	return analysisAtomicBytes(path, data)
}
func analysisAtomicBytes(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".analysis-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// isolated helper keeps this file independent from report redaction details.
func jsonMarshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
func sha256Bytes(data []byte) string          { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
