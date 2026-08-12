package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"takt/internal/redact"
	"takt/internal/store"
)

type FlowEvidence struct {
	CaseID             string
	Repeat             int
	States             []*store.RunState
	Request            FlowValidationRequest
	Validation         FlowValidationExecution
	Diff               []byte
	Artifacts          []store.ArtifactRef
	ArtifactDirs       map[string]string
	PreparedHeadCommit string
	SCMDir             string
}

type FlowCleanupPaths struct {
	ControlWorkspace, BaselineWorkspace, BareRemote string
	Created                                         []string
	Keep, Paused                                    bool
}

type flowRunEvidence struct {
	RootRunID string            `json:"root_run_id"`
	States    []*store.RunState `json:"states"`
}

type flowArtifactManifest struct {
	Artifacts []flowArtifactEvidence `json:"artifacts"`
}

type flowArtifactEvidence struct {
	ID             string `json:"id,omitempty"`
	Type           string `json:"type,omitempty"`
	MIME           string `json:"mime,omitempty"`
	ProducerRunID  string `json:"producer_run_id,omitempty"`
	ProducerNodeID string `json:"producer_node_id,omitempty"`
	Attempt        int    `json:"attempt,omitempty"`
	CallID         string `json:"call_id,omitempty"`
	SourcePath     string `json:"source_path"`
	EvidencePath   string `json:"evidence_path"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	Mode           string `json:"mode"`
	Registered     bool   `json:"registered"`
	Redacted       bool   `json:"redacted"`
}

// WriteFlowEvidence persists only eval-owned evidence. JSON and text are
// redacted before atomic publication; binary bytes with a known secret stop the
// evaluation instead of leaving a partial secret-bearing artifact behind.
func WriteFlowEvidence(root string, item FlowEvidence, redactor *redact.Redactor) error {
	if item.CaseID == "" || item.Repeat <= 0 {
		return errors.New("flow evidence requires case ID and positive repeat")
	}
	repeatRoot := filepath.Join(root, "cases", item.CaseID, fmt.Sprintf("repeat-%03d", item.Repeat))
	if err := os.MkdirAll(repeatRoot, 0755); err != nil {
		return err
	}
	rootID := ""
	if len(item.States) > 0 && item.States[0] != nil {
		rootID = item.States[0].ID
	}
	if err := writeFlowJSON(filepath.Join(repeatRoot, "run.json"), flowRunEvidence{RootRunID: rootID, States: item.States}, redactor); err != nil {
		return err
	}
	if err := writeFlowJSON(filepath.Join(repeatRoot, "validation-request.json"), item.Request, redactor); err != nil {
		return err
	}
	record := FlowValidationRecord{Status: item.Validation.Status, ErrorCode: item.Validation.ErrorCode, Error: item.Validation.Error, Result: item.Validation.Result, DurationMS: item.Validation.Duration.Milliseconds()}
	if err := writeFlowJSON(filepath.Join(repeatRoot, "validation-result.json"), record, redactor); err != nil {
		return err
	}
	if err := writeFlowBytes(filepath.Join(repeatRoot, "validator.stderr"), item.Validation.Stderr, redactor); err != nil {
		return err
	}
	if err := writeFlowBytes(filepath.Join(repeatRoot, "diff.patch"), item.Diff, redactor); err != nil {
		return err
	}
	if item.SCMDir != "" {
		if err := copyFlowEvidenceTree(item.SCMDir, filepath.Join(repeatRoot, "scm"), redactor); err != nil {
			return fmt.Errorf("copy SCM evidence: %w", err)
		}
	}
	manifest, files, err := collectFlowArtifacts(item, redactor)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := writeFlowRaw(filepath.Join(repeatRoot, "artifacts", "files", filepath.FromSlash(file.source)), file.data, file.mode); err != nil {
			return err
		}
	}
	return writeFlowJSON(filepath.Join(repeatRoot, "artifacts", "manifest.json"), flowArtifactManifest{Artifacts: manifest}, redactor)
}

func writeFlowJSON(path string, value any, redactor *redact.Redactor) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if redactor != nil {
		var structured any
		if err := json.Unmarshal(data, &structured); err != nil {
			return err
		}
		data, err = json.MarshalIndent(redactor.Any(structured), "", "  ")
		if err != nil {
			return err
		}
	}
	if !json.Valid(data) {
		return fmt.Errorf("redacted JSON is invalid: %s", filepath.Base(path))
	}
	return writeFlowRaw(path, data, 0644)
}

func copyFlowEvidenceTree(source, destination string, redactor *redact.Redactor) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink forbidden: %s", path)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular file forbidden: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		textual := utf8.Valid(data) && !strings.ContainsRune(string(data), 0)
		if redactor != nil {
			redacted, matched := redactor.Bytes(data)
			if !textual && matched {
				return fmt.Errorf("binary file contains known secret: %s", path)
			}
			if textual {
				data = redacted
			}
		}
		return writeFlowRaw(target, data, info.Mode())
	})
}

func writeFlowBytes(path string, data []byte, redactor *redact.Redactor) error {
	if redactor != nil {
		data, _ = redactor.Bytes(data)
	}
	return writeFlowRaw(path, data, 0644)
}

func writeFlowRaw(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

type flowArtifactFile struct {
	source string
	data   []byte
	mode   fs.FileMode
}

func collectFlowArtifacts(item FlowEvidence, redactor *redact.Redactor) ([]flowArtifactEvidence, []flowArtifactFile, error) {
	ids := make([]string, 0, len(item.ArtifactDirs))
	for id := range item.ArtifactDirs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var manifest []flowArtifactEvidence
	var files []flowArtifactFile
	for _, runID := range ids {
		dir, err := filepath.Abs(item.ArtifactDirs[runID])
		if err != nil {
			return nil, nil, err
		}
		if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("artifact symlink forbidden: %s", path)
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("artifact is not a regular file: %s", path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			absolute := filepath.Clean(path)
			artifact, isRegistered := flowArtifactAt(item.Artifacts, dir, absolute)
			if isRegistered {
				hash := sha256.Sum256(data)
				if artifact.Size != int64(len(data)) || !strings.EqualFold(artifact.SHA256, hex.EncodeToString(hash[:])) {
					return fmt.Errorf("artifact provenance mismatch: %s", path)
				}
			}
			textual := isRegistered && redact.TextualMIME(artifact.MIME)
			if !isRegistered {
				textual = utf8.Valid(data) && !strings.ContainsRune(string(data), 0)
			}
			persisted, changed := data, false
			if redactor != nil {
				redacted, matched := redactor.Bytes(data)
				if textual {
					persisted, changed = redacted, matched
				} else if matched {
					return fmt.Errorf("binary artifact contains known secret: %s", path)
				}
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			source := filepath.ToSlash(filepath.Join(runID, rel))
			hash := sha256.Sum256(persisted)
			entryRecord := flowArtifactEvidence{SourcePath: source, EvidencePath: "files/" + source, SHA256: hex.EncodeToString(hash[:]), Size: int64(len(persisted)), Mode: fmt.Sprintf("%04o", info.Mode().Perm()), Registered: isRegistered, Redacted: changed}
			if isRegistered {
				entryRecord.ID, entryRecord.Type, entryRecord.MIME = artifact.ID, artifact.Type, artifact.MIME
				entryRecord.ProducerRunID, entryRecord.ProducerNodeID, entryRecord.Attempt, entryRecord.CallID = artifact.ProducerRunID, artifact.ProducerNodeID, artifact.Attempt, artifact.CallID
			}
			manifest = append(manifest, entryRecord)
			files = append(files, flowArtifactFile{source: source, data: persisted, mode: info.Mode()})
			return nil
		}); err != nil {
			return nil, nil, err
		}
	}
	sort.Slice(manifest, func(i, j int) bool { return manifest[i].SourcePath < manifest[j].SourcePath })
	sort.Slice(files, func(i, j int) bool { return files[i].source < files[j].source })
	return manifest, files, nil
}

func flowArtifactAt(artifacts []store.ArtifactRef, dir, path string) (store.ArtifactRef, bool) {
	for _, artifact := range artifacts {
		if artifact.Path == "" {
			continue
		}
		candidate := artifact.Path
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(dir, candidate)
		}
		if filepath.Clean(candidate) == path {
			return artifact, true
		}
	}
	return store.ArtifactRef{}, false
}

func CleanupFlowRepeat(root string, paths FlowCleanupPaths) error {
	if paths.Keep || paths.Paused {
		return nil
	}
	canonicalRoot, err := canonicalFlowPath(root)
	if err != nil {
		return err
	}
	created := make(map[string]bool, len(paths.Created))
	for _, path := range paths.Created {
		created[path] = true
	}
	var targets []string
	for _, target := range []string{paths.ControlWorkspace, paths.BaselineWorkspace, paths.BareRemote} {
		if target == "" {
			continue
		}
		if !created[target] {
			return fmt.Errorf("cleanup target was not created by this evaluation: %s", target)
		}
		canonicalTarget, err := canonicalFlowPath(target)
		if err != nil {
			return err
		}
		if canonicalTarget == canonicalRoot || !pathContains(canonicalRoot, canonicalTarget) {
			return fmt.Errorf("cleanup target escapes evidence root: %s", target)
		}
		if !flowCleanupLayout(canonicalRoot, canonicalTarget) {
			return fmt.Errorf("cleanup target is not an eval workspace: %s", target)
		}
		targets = append(targets, canonicalTarget)
	}
	for _, target := range targets {
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	return nil
}

func flowCleanupLayout(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 4 || parts[0] != "workspaces" || !strings.HasPrefix(parts[2], "repeat-") {
		return false
	}
	return parts[3] == "control" || parts[3] == "baseline" || parts[3] == "origin.git"
}

func canonicalFlowPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(filepath.Clean(absolute))
}
