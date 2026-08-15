package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	Version         string                 `json:"version"`
	CaseID          string                 `json:"case_id"`
	Repeat          int                    `json:"repeat"`
	EvidenceRoot    string                 `json:"evidence_root"`
	Files           []AnalysisEvidenceFile `json:"files"`
	MissingEvidence []string               `json:"missing_evidence,omitempty"`
}
type AnalysisWorkspaceRef struct {
	CaseID           string `json:"case_id"`
	Repeat           int    `json:"repeat"`
	Workspace        string `json:"workspace"`
	EvidenceManifest string `json:"evidence_manifest"`
}
type AnalysisManifest struct {
	Version             string                 `json:"version"`
	SourceEvaluationDir string                 `json:"source_evaluation_dir"`
	SelectedCases       []AnalysisCaseRef      `json:"selected_cases"`
	ConfigFingerprint   string                 `json:"config_fingerprint"`
	Model               AnalysisModel          `json:"model"`
	Workspaces          []AnalysisWorkspaceRef `json:"workspaces"`
}

const analysisManifestVersion = "takt-evaluation-analysis-manifest/v1alpha1"

func buildAnalysisEvidenceManifest(output, repeatRoot string, inspection *InspectionCase, run RunRecord) (AnalysisEvidenceManifest, error) {
	m := AnalysisEvidenceManifest{Version: analysisManifestVersion, CaseID: run.CaseID, Repeat: run.Repeat, EvidenceRoot: ""}
	if rel, err := filepath.Rel(output, repeatRoot); err == nil {
		m.EvidenceRoot = filepath.ToSlash(rel)
	}
	paths := []string{"run.json"}
	for _, p := range []string{"validation-request.json", "validation-result.json", "diff.patch", "activity.json", "executor-manifest.json", "repository.bundle"} {
		if _, err := os.Stat(filepath.Join(repeatRoot, p)); err == nil {
			paths = append(paths, p)
		}
	}
	if inspection != nil {
		for _, p := range []string{inspection.Evidence.Validation, inspection.Evidence.Diff, inspection.Evidence.Source, inspection.Evidence.Activity, inspection.Evidence.ExecutorManifest, inspection.Evidence.SCMCallsPath} {
			if p != "" {
				if rr, e := filepath.Rel(repeatRoot, filepath.Join(output, filepath.FromSlash(p))); e == nil {
					paths = append(paths, filepath.ToSlash(rr))
				}
			}
		}
		for _, p := range inspection.Evidence.Artifacts {
			if p != "" {
				if rr, e := filepath.Rel(repeatRoot, filepath.Join(output, filepath.FromSlash(p))); e == nil {
					paths = append(paths, filepath.ToSlash(rr))
				}
			}
		}
		m.MissingEvidence = append(m.MissingEvidence, inspection.MissingEvidence...)
		for _, p := range []string{"validation-request.json", "validation-result.json", "diff.patch", "activity.json", "executor-manifest.json", "repository.bundle"} {
			if _, err := os.Stat(filepath.Join(repeatRoot, p)); os.IsNotExist(err) {
				m.MissingEvidence = append(m.MissingEvidence, p)
			}
		}
	}
	sort.Strings(paths)
	seen := map[string]bool{}
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
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				m.MissingEvidence = append(m.MissingEvidence, rel)
				continue
			}
			return m, err
		}
		if info.IsDir() {
			err := filepath.Walk(path, func(child string, fi os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if fi.IsDir() {
					return nil
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
		h, err := hashPath(path)
		if err != nil {
			return m, err
		}
		m.Files = append(m.Files, AnalysisEvidenceFile{Path: rel, Size: info.Size(), SHA256: h})
	}
	sort.Strings(m.MissingEvidence)
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	return m, nil
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
