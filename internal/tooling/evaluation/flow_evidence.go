package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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
	Events             []store.Event
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

var errUnsupportedFlowEvidenceEntry = errors.New("unsupported flow evidence entry")

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
	diff := item.Diff
	if item.PreparedHeadCommit != "" && item.Request.Workspace != "" {
		var err error
		diff, err = flowWorkspaceDiff(item.Request.Workspace, item.PreparedHeadCommit)
		if err != nil {
			return err
		}
		if err := writeFlowGitBundle(item.Request.Workspace, filepath.Join(repeatRoot, "repository.bundle"), redactor); err != nil {
			return err
		}
	}
	rootID := ""
	if len(item.States) > 0 && item.States[0] != nil {
		rootID = item.States[0].ID
	}
	if err := writeFlowJSON(filepath.Join(repeatRoot, "run.json"), flowRunEvidence{RootRunID: rootID, States: item.States}, redactor); err != nil {
		return err
	}
	if err := writeFlowExecutorManifest(repeatRoot, item, redactor); err != nil {
		return err
	}
	if err := writeFlowJSON(filepath.Join(repeatRoot, "activity.json"), flowActivityEvidence{Events: flowAssistantActivity(item.Events)}, redactor); err != nil {
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
	if err := writeFlowBytes(filepath.Join(repeatRoot, "diff.patch"), diff, redactor); err != nil {
		return err
	}
	if item.Request.Workspace != "" {
		if _, err := os.Stat(item.Request.Workspace); err == nil {
			if err := copyFlowEvidenceTree(item.Request.Workspace, filepath.Join(repeatRoot, "source"), redactor, ".git", ".takt"); err != nil {
				if !errors.Is(err, errUnsupportedFlowEvidenceEntry) {
					return fmt.Errorf("copy source evidence: %w", err)
				}
				if err := writeFlowBytes(filepath.Join(repeatRoot, "source-unavailable.txt"), []byte(err.Error()+"\n"), redactor); err != nil {
					return fmt.Errorf("write source evidence diagnostic: %w", err)
				}
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat source evidence: %w", err)
		}
	}
	if item.SCMDir != "" {
		if _, err := os.Stat(item.SCMDir); err == nil {
			if err := copyFlowEvidenceTree(item.SCMDir, filepath.Join(repeatRoot, "scm"), redactor); err != nil {
				return fmt.Errorf("copy SCM evidence: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat SCM evidence: %w", err)
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

func flowAssistantActivity(events []store.Event) []flowActivityEvent {
	activity := []flowActivityEvent{}
	for _, event := range events {
		if event.Type != "assistant.tool.started" && event.Type != "assistant.diagnostic" {
			continue
		}
		input, _ := event.Data["input"].(map[string]any)
		tool, _ := event.Data["tool"].(string)
		var data map[string]any
		if event.Type == "assistant.diagnostic" {
			data = event.Data
		}
		activity = append(activity, flowActivityEvent{Time: event.Time, Type: event.Type, RunID: event.RunID, NodeID: event.NodeID, Revision: event.Revision, Tool: tool, Input: input, Data: data})
	}
	return activity
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

func copyFlowEvidenceTree(source, destination string, redactor *redact.Redactor, excludedDirs ...string) error {
	staging, err := os.MkdirTemp(filepath.Dir(destination), "."+filepath.Base(destination)+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink forbidden: %s", errUnsupportedFlowEvidenceEntry, path)
		}
		if path != source {
			for _, name := range excludedDirs {
				if entry.Name() == name {
					if entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(staging, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: non-regular file forbidden: %s", errUnsupportedFlowEvidenceEntry, path)
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
	}); err != nil {
		return err
	}
	return os.Rename(staging, destination)
}

func flowWorkspaceDiff(workspace, baseCommit string) ([]byte, error) {
	tracked, err := exec.Command("git", "-C", workspace, "diff", "--binary", baseCommit, "--", ".", ":(exclude).takt").Output()
	if err != nil {
		return nil, fmt.Errorf("diff flow workspace: %w", err)
	}
	untracked, err := exec.Command("git", "-C", workspace, "ls-files", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("list untracked flow source: %w", err)
	}
	paths := strings.Split(string(untracked), "\x00")
	sort.Strings(paths)
	result := append([]byte(nil), tracked...)
	for _, path := range paths {
		path = filepath.Clean(filepath.FromSlash(path))
		if path == "." || path == ".takt" || strings.HasPrefix(path, ".takt"+string(filepath.Separator)) {
			continue
		}
		cmd := exec.Command("git", "diff", "--binary", "--no-index", "--", os.DevNull, path)
		cmd.Dir = workspace
		patch, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
				return nil, fmt.Errorf("diff untracked flow source %s: %w", path, err)
			}
		}
		result = append(result, patch...)
	}
	return result, nil
}

func writeFlowGitBundle(workspace, destination string, redactor *redact.Redactor) error {
	if err := scanGitHistoryForSecrets(workspace, redactor); err != nil {
		return err
	}
	tmp := destination + ".tmp"
	_ = os.Remove(tmp)
	cmd := exec.Command("git", "-C", workspace, "bundle", "create", tmp, "--all")
	if output, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("create git bundle: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.Rename(tmp, destination); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publish git bundle: %w", err)
	}
	return nil
}

func scanGitHistoryForSecrets(workspace string, redactor *redact.Redactor) error {
	if redactor == nil {
		return nil
	}
	objects, err := exec.Command("git", "-C", workspace, "rev-list", "--objects", "--all").Output()
	if err != nil {
		return fmt.Errorf("list Git history: %w", err)
	}
	seen := make(map[string]struct{})
	for _, line := range strings.Split(string(objects), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		objectID := fields[0]
		if _, ok := seen[objectID]; ok {
			continue
		}
		seen[objectID] = struct{}{}
		kind, err := exec.Command("git", "-C", workspace, "cat-file", "-t", objectID).Output()
		if err != nil {
			return fmt.Errorf("inspect Git object %s: %w", objectID, err)
		}
		if strings.TrimSpace(string(kind)) != "blob" {
			continue
		}
		data, err := exec.Command("git", "-C", workspace, "cat-file", "blob", objectID).Output()
		if err != nil {
			return fmt.Errorf("read Git blob %s: %w", objectID, err)
		}
		if _, matched := redactor.Bytes(data); matched {
			return fmt.Errorf("git blob contains known secret: %s", objectID)
		}
	}
	return nil
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
	if len(parts) != 4 || parts[0] != "workspaces" || !flowRepeatDir(parts[2]) {
		return false
	}
	return parts[3] == "control" || parts[3] == "baseline" || parts[3] == "origin.git"
}

func flowRepeatDir(value string) bool {
	if len(value) != len("repeat-")+3 || !strings.HasPrefix(value, "repeat-") {
		return false
	}
	for _, b := range value[len("repeat-"):] {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}

func canonicalFlowPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(filepath.Clean(absolute))
}
