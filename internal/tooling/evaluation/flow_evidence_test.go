package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"takt/internal/redact"
	"takt/internal/store"
	"takt/internal/validation"
)

func TestFlowEvidenceWritesFilteredRedactedAssistantActivity(t *testing.T) {
	root := t.TempDir()
	item := FlowEvidence{
		CaseID: "case", Repeat: 1,
		Events: []store.Event{
			{Time: time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC), Type: "assistant.tool.started", RunID: "run-1", NodeID: "implement", Revision: 3, Data: map[string]any{"tool": "write", "input": map[string]any{"path": "/control/main.go", "content": "known-secret"}}},
			{Type: "assistant.message", RunID: "run-1", Revision: 4, Data: map[string]any{"message": "known-secret"}},
			{Type: "assistant.tool.completed", RunID: "run-1", Revision: 5, Data: map[string]any{"tool": "write", "output": "known-secret"}},
		},
	}
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	if err := WriteFlowEvidence(root, item, r); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "cases", "case", "repeat-001", "activity.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"time": "2026-08-15T10:00:00Z"`, `"type": "assistant.tool.started"`, `"tool": "write"`, `"path": "/control/main.go"`, `"content": "\u003credacted\u003e"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("activity misses %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"known-secret", "assistant.message", "assistant.tool.completed", `"output"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("activity contains %q: %s", forbidden, text)
		}
	}
}

func TestFlowEvidenceWritesRedactedAtomicRecordsAndArtifacts(t *testing.T) {
	root := t.TempDir()
	artifacts := filepath.Join(t.TempDir(), "run-a")
	if err := os.MkdirAll(artifacts, 0755); err != nil {
		t.Fatal(err)
	}
	text := []byte("token=known-secret\n")
	if err := os.WriteFile(filepath.Join(artifacts, "summary.md"), text, 0640); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(text)
	item := FlowEvidence{
		CaseID: "case-a", Repeat: 1,
		States:     []*store.RunState{{ID: "run-a", Status: store.RunCompleted, Error: "known-secret", Artifacts: []store.ArtifactRef{{ID: "summary", MIME: "text/markdown", Path: "summary.md", SHA256: hex.EncodeToString(h[:]), Size: int64(len(text)), ProducerRunID: "run-a"}}}},
		Request:    FlowValidationRequest{Workspace: "/local/known-secret", Run: FlowValidationRun{ID: "run-a", Status: "completed"}},
		Validation: FlowValidationExecution{Status: "completed", Stderr: []byte("known-secret"), Result: &validation.Result{Valid: true}},
		Diff:       []byte("known-secret"), Artifacts: []store.ArtifactRef{{ID: "summary", MIME: "text/markdown", Path: "summary.md", SHA256: hex.EncodeToString(h[:]), Size: int64(len(text)), ProducerRunID: "run-a"}},
		ArtifactDirs: map[string]string{"run-a": artifacts},
	}
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	if err := WriteFlowEvidence(root, item, r); err != nil {
		t.Fatal(err)
	}
	repeat := filepath.Join(root, "cases", "case-a", "repeat-001")
	for _, name := range []string{"run.json", "validation-request.json", "validation-result.json", "validator.stderr", "diff.patch", "artifacts/manifest.json", "artifacts/files/run-a/summary.md"} {
		data, err := os.ReadFile(filepath.Join(repeat, name))
		if err != nil {
			t.Fatal(name, err)
		}
		if strings.Contains(string(data), "known-secret") {
			t.Fatalf("%s leaked secret: %s", name, data)
		}
	}
	for _, name := range []string{"run.json", "validation-request.json", "validation-result.json", "artifacts/manifest.json"} {
		data, _ := os.ReadFile(filepath.Join(repeat, name))
		if !json.Valid(data) {
			t.Fatalf("invalid json: %s", name)
		}
		if _, err := os.Stat(filepath.Join(repeat, name+".tmp")); !os.IsNotExist(err) {
			t.Fatalf("temporary file remained: %s", name)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(repeat, "artifacts/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"registered": true`) || !strings.Contains(string(manifest), `"redacted": true`) || !strings.Contains(string(manifest), `"source_path": "run-a/summary.md"`) {
		t.Fatalf("manifest=%s", manifest)
	}
	persisted, err := os.ReadFile(filepath.Join(repeat, "artifacts/files/run-a/summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	persistedHash := sha256.Sum256(persisted)
	if !strings.Contains(string(manifest), hex.EncodeToString(persistedHash[:])) {
		t.Fatalf("manifest missing persisted hash: %s", manifest)
	}
}

func TestWriteFlowEvidenceCopiesRedactedPiSession(t *testing.T) {
	root, sessionDir := t.TempDir(), t.TempDir()
	session := filepath.Join(sessionDir, "session.jsonl")
	if err := os.WriteFile(session, []byte(`{"event":"known-secret"}\n`), 0600); err != nil {
		t.Fatal(err)
	}
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	state := &store.RunState{ID: "run", Nodes: map[string]*store.NodeState{
		"implement": {Executions: []store.ExecutionState{{Attempt: 1, ProviderAttempt: 1, Adapter: "pi", SessionPath: session}}},
	}}
	if err := WriteFlowEvidence(root, FlowEvidence{CaseID: "case", Repeat: 1, States: []*store.RunState{state}}, r); err != nil {
		t.Fatal(err)
	}
	repeatRoot := filepath.Join(root, "cases", "case", "repeat-001")
	manifest := readFlowTestJSON(t, filepath.Join(repeatRoot, "executor-manifest.json"))
	executions := manifest["executions"].([]any)
	got := executions[0].(map[string]any)
	if got["adapter"] != "pi" || got["session_evidence"] != "recorded" || got["session_evidence_path"] != "sessions/implement/attempt-001-provider-001.jsonl" {
		t.Fatalf("unexpected executor manifest entry: %#v", got)
	}
	data, err := os.ReadFile(filepath.Join(repeatRoot, "sessions", "implement", "attempt-001-provider-001.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "known-secret") {
		t.Fatal("session evidence contains a redacted secret")
	}
}

func TestWriteFlowEvidenceRecordsUnavailableExecutorSessions(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	missing := filepath.Join(dir, "missing.jsonl")
	symlinkTarget := filepath.Join(dir, "target.jsonl")
	if err := os.WriteFile(symlinkTarget, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(symlinkTarget, symlink); err != nil {
		t.Skip(err)
	}
	large := filepath.Join(dir, "large.jsonl")
	if err := os.WriteFile(large, make([]byte, maxSessionEvidenceBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	state := &store.RunState{ID: "run", Nodes: map[string]*store.NodeState{
		"a": {Executions: []store.ExecutionState{{Attempt: 1, SessionPath: missing}}},
		"b": {Executions: []store.ExecutionState{{Attempt: 1, SessionPath: symlink}}},
		"c": {Executions: []store.ExecutionState{{Attempt: 1, SessionPath: large}}},
		"d": {Executions: []store.ExecutionState{{Attempt: 1}}},
	}}
	if err := WriteFlowEvidence(root, FlowEvidence{CaseID: "case", Repeat: 1, States: []*store.RunState{state}}, &redact.Redactor{}); err != nil {
		t.Fatal(err)
	}
	entries := readFlowTestJSON(t, filepath.Join(root, "cases", "case", "repeat-001", "executor-manifest.json"))["executions"].([]any)
	want := map[string]string{"a": "path_missing", "b": "path_symlink_forbidden", "c": "path_too_large", "d": "adapter_did_not_expose_path"}
	for _, raw := range entries {
		entry := raw.(map[string]any)
		if got := entry["session_evidence_reason"]; got != want[entry["node_id"].(string)] {
			t.Fatalf("entry=%#v", entry)
		}
		if entry["session_evidence"] != "unavailable" {
			t.Fatalf("entry recorded unavailable path: %#v", entry)
		}
	}
}

func TestWriteFlowEvidenceSeparatesExecutorDestinationCollisions(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	first, second := filepath.Join(dir, "one.jsonl"), filepath.Join(dir, "two.jsonl")
	if err := os.WriteFile(first, []byte("one\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("two\n"), 0600); err != nil {
		t.Fatal(err)
	}
	states := []*store.RunState{
		{ID: "run-a", Nodes: map[string]*store.NodeState{"implement": {Executions: []store.ExecutionState{{Attempt: 1, ProviderAttempt: 1, SessionPath: first}}}}},
		{ID: "run-b", Nodes: map[string]*store.NodeState{"implement": {Executions: []store.ExecutionState{{Attempt: 1, ProviderAttempt: 1, SessionPath: second}}}}},
	}
	if err := WriteFlowEvidence(root, FlowEvidence{CaseID: "case", Repeat: 1, States: states}, &redact.Redactor{}); err != nil {
		t.Fatal(err)
	}
	entries := readFlowTestJSON(t, filepath.Join(root, "cases", "case", "repeat-001", "executor-manifest.json"))["executions"].([]any)
	if len(entries) != 2 {
		t.Fatalf("entries=%#v", entries)
	}
	paths := map[string]bool{}
	for _, raw := range entries {
		path := raw.(map[string]any)["session_evidence_path"].(string)
		if paths[path] {
			t.Fatalf("duplicate destination %q", path)
		}
		paths[path] = true
		if _, err := os.Stat(filepath.Join(root, "cases", "case", "repeat-001", filepath.FromSlash(path))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWriteFlowEvidenceLatchesAggregateSessionLimit(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	paths := make([]string, 9)
	for i, size := range []int{4 << 20, 4 << 20, 4 << 20, 4 << 20, 4 << 20, 4 << 20, 4 << 20, 4 << 20, 1 << 20} {
		paths[i] = filepath.Join(dir, fmt.Sprintf("%d.jsonl", i))
		if err := os.WriteFile(paths[i], make([]byte, size), 0600); err != nil {
			t.Fatal(err)
		}
	}
	node := func(path string) *store.NodeState {
		return &store.NodeState{Executions: []store.ExecutionState{{Attempt: 1, ProviderAttempt: 1, SessionPath: path}}}
	}
	nodes := map[string]*store.NodeState{}
	for i, path := range paths {
		nodes[fmt.Sprintf("n%02d", i)] = node(path)
	}
	state := &store.RunState{ID: "run", Nodes: nodes}
	if err := WriteFlowEvidence(root, FlowEvidence{CaseID: "case", Repeat: 1, States: []*store.RunState{state}}, &redact.Redactor{}); err != nil {
		t.Fatal(err)
	}
	entries := readFlowTestJSON(t, filepath.Join(root, "cases", "case", "repeat-001", "executor-manifest.json"))["executions"].([]any)
	for _, raw := range entries {
		entry := raw.(map[string]any)
		if entry["node_id"].(string) < "n08" && entry["session_evidence"] != "recorded" {
			t.Fatalf("first entry=%#v", entry)
		}
		if entry["node_id"].(string) >= "n08" && entry["session_evidence_reason"] != "aggregate_limit" {
			t.Fatalf("latched entry=%#v", entry)
		}
	}
}

func readFlowTestJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFlowEvidenceRejectsBinarySecretBeforeCopy(t *testing.T) {
	root := t.TempDir()
	artifacts := filepath.Join(t.TempDir(), "run-a")
	if err := os.MkdirAll(artifacts, 0755); err != nil {
		t.Fatal(err)
	}
	data := []byte{0, 'k', 'n', 'o', 'w', 'n', '-', 's', 'e', 'c', 'r', 'e', 't'}
	if err := os.WriteFile(filepath.Join(artifacts, "secret.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	err := WriteFlowEvidence(root, FlowEvidence{CaseID: "case", Repeat: 1, States: []*store.RunState{{ID: "run-a"}}, ArtifactDirs: map[string]string{"run-a": artifacts}}, r)
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "cases", "case", "repeat-001", "artifacts", "files", "run-a", "secret.bin")); !os.IsNotExist(err) {
		t.Fatal("binary artifact was persisted")
	}
}

func TestFlowEvidenceRejectsSecretInGitHistoryBeforeBundle(t *testing.T) {
	workspace := t.TempDir()
	gitOutput(t, workspace, "init")
	gitOutput(t, workspace, "config", "user.name", "Takt Test")
	gitOutput(t, workspace, "config", "user.email", "takt@example.test")
	if err := os.WriteFile(filepath.Join(workspace, "historical.txt"), []byte("known-secret\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, workspace, "add", "historical.txt")
	gitOutput(t, workspace, "commit", "-m", "secret commit")
	if err := os.Remove(filepath.Join(workspace, "historical.txt")); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, workspace, "add", "-u")
	gitOutput(t, workspace, "commit", "-m", "remove secret")
	base := strings.TrimSpace(gitOutput(t, workspace, "rev-parse", "HEAD"))
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	err := WriteFlowEvidence(t.TempDir(), FlowEvidence{
		CaseID: "case", Repeat: 1, PreparedHeadCommit: base,
		Request: FlowValidationRequest{Workspace: workspace},
	}, r)
	if err == nil || !strings.Contains(err.Error(), "git blob contains known secret") {
		t.Fatalf("historical secret was not rejected: %v", err)
	}
}

func TestFlowEvidenceRedactsSCMAndStructuredJSON(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "receipt.txt"), []byte("quote\"secret"), 0644); err != nil {
		t.Fatal(err)
	}
	r := &redact.Redactor{}
	r.AddSecret(`quote"secret`)
	item := FlowEvidence{
		CaseID: "case", Repeat: 1, SCMDir: source,
		States: []*store.RunState{{ID: "run", Error: `quote"secret`}},
	}
	if err := WriteFlowEvidence(root, item, r); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "cases", "case", "repeat-001", "run.json"),
		filepath.Join(root, "cases", "case", "repeat-001", "scm", "receipt.txt"),
	} {
		data, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(data), `quote"secret`) {
			t.Fatalf("%s: data=%q err=%v", path, data, err)
		}
		if strings.HasSuffix(path, ".json") && !strings.Contains(string(data), `\u003credacted\u003e`) {
			t.Fatalf("json was not redacted: %s", data)
		}
		if strings.HasSuffix(path, ".txt") && !strings.Contains(string(data), "<redacted>") {
			t.Fatalf("text was not redacted: %s", data)
		}
	}
}

func TestFlowEvidencePreservesRedactedProductSourceOnly(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	for _, path := range []string{
		filepath.Join(workspace, "cmd", "mini-du", "main.go"),
		filepath.Join(workspace, ".takt", "profiles", "code", "profile.yaml"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("known-secret\n"), 0750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, ".git"), []byte("gitdir: hidden\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	if err := WriteFlowEvidence(root, FlowEvidence{CaseID: "case", Repeat: 1, Request: FlowValidationRequest{Workspace: workspace}}, r); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "cases", "case", "repeat-001", "source")
	data, err := os.ReadFile(filepath.Join(source, "cmd", "mini-du", "main.go"))
	if err != nil || string(data) != "<redacted>\n" {
		t.Fatalf("product source=%q err=%v", data, err)
	}
	info, err := os.Stat(filepath.Join(source, "cmd", "mini-du", "main.go"))
	if err != nil || info.Mode().Perm() != 0750 {
		t.Fatalf("product source mode=%v err=%v", info, err)
	}
	for _, excluded := range []string{".git", ".takt"} {
		if _, err := os.Stat(filepath.Join(source, excluded)); !os.IsNotExist(err) {
			t.Fatalf("%s was copied into source evidence: %v", excluded, err)
		}
	}
}

func TestFlowEvidenceRejectsUnsafeProductSource(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, workspace string)
	}{
		{"symlink", func(t *testing.T, workspace string) {
			if err := os.Symlink(t.TempDir(), filepath.Join(workspace, "link")); err != nil {
				t.Skip(err)
			}
		}},
		{"binary secret", func(t *testing.T, workspace string) {
			if err := os.WriteFile(filepath.Join(workspace, "secret.bin"), append([]byte{0}, []byte("known-secret")...), 0600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			tc.setup(t, workspace)
			r := &redact.Redactor{}
			r.AddSecret("known-secret")
			err := WriteFlowEvidence(t.TempDir(), FlowEvidence{CaseID: "case", Repeat: 1, Request: FlowValidationRequest{Workspace: workspace}}, r)
			if err == nil {
				t.Fatal("unsafe product source was accepted")
			}
		})
	}
}

func TestFlowEvidenceAllowsAbsentSCMState(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "not-created")
	err := WriteFlowEvidence(root, FlowEvidence{CaseID: "case", Repeat: 1, SCMDir: missing}, &redact.Redactor{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "cases", "case", "repeat-001", "scm")); !os.IsNotExist(err) {
		t.Fatalf("scm evidence unexpectedly exists: %v", err)
	}
}

func TestFlowEvidenceRejectsUnsafeSCMAndArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name, file string
		setup      func(t *testing.T, dir string)
		item       func(dir string) FlowEvidence
	}{
		{"scm symlink", "link", func(t *testing.T, dir string) {
			if err := os.Symlink(t.TempDir(), filepath.Join(dir, "link")); err != nil {
				t.Skip(err)
			}
		}, func(dir string) FlowEvidence { return FlowEvidence{CaseID: "case", Repeat: 1, SCMDir: dir} }},
		{"artifact symlink", "link", func(t *testing.T, dir string) {
			if err := os.Symlink(t.TempDir(), filepath.Join(dir, "link")); err != nil {
				t.Skip(err)
			}
		}, func(dir string) FlowEvidence {
			return FlowEvidence{CaseID: "case", Repeat: 1, ArtifactDirs: map[string]string{"run": dir}}
		}},
		{"artifact fifo", "pipe", func(t *testing.T, dir string) {
			if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
				t.Skip(err)
			}
		}, func(dir string) FlowEvidence {
			return FlowEvidence{CaseID: "case", Repeat: 1, ArtifactDirs: map[string]string{"run": dir}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			if err := WriteFlowEvidence(t.TempDir(), tc.item(dir), &redact.Redactor{}); err == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
}

func TestFlowEvidenceRejectsBinarySecretInSCM(t *testing.T) {
	scm := t.TempDir()
	if err := os.WriteFile(filepath.Join(scm, "receipt.bin"), append([]byte{0}, []byte("known-secret")...), 0600); err != nil {
		t.Fatal(err)
	}
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	err := WriteFlowEvidence(t.TempDir(), FlowEvidence{CaseID: "case", Repeat: 1, SCMDir: scm}, r)
	if err == nil || !strings.Contains(err.Error(), "known secret") {
		t.Fatalf("err=%v", err)
	}
}

func TestFlowEvidenceRejectsArtifactProvenanceMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "artifact.txt"), []byte("actual"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []store.ArtifactRef{
		{Path: "artifact.txt", MIME: "text/plain", SHA256: strings.Repeat("0", 64), Size: 6},
		{Path: "artifact.txt", MIME: "text/plain", SHA256: evidenceSHA256Hex([]byte("actual")), Size: 7},
	} {
		err := WriteFlowEvidence(t.TempDir(), FlowEvidence{CaseID: "case", Repeat: 1, Artifacts: []store.ArtifactRef{artifact}, ArtifactDirs: map[string]string{"run": dir}}, &redact.Redactor{})
		if err == nil || !strings.Contains(err.Error(), "provenance mismatch") {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestCleanupFlowRepeatRequiresContainedExactCreatedTargets(t *testing.T) {
	root := t.TempDir()
	control := filepath.Join(root, "workspaces", "case", "repeat-001", "control")
	baseline := filepath.Join(root, "workspaces", "case", "repeat-001", "baseline")
	if err := os.MkdirAll(control, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(baseline, 0755); err != nil {
		t.Fatal(err)
	}
	if err := CleanupFlowRepeat(root, FlowCleanupPaths{ControlWorkspace: control, BaselineWorkspace: baseline, Created: []string{control}}); err == nil {
		t.Fatal("uncreated baseline was removed")
	}
	if _, err := os.Stat(control); err != nil {
		t.Fatal(err)
	}
	if err := CleanupFlowRepeat(root, FlowCleanupPaths{ControlWorkspace: control, Created: []string{control}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(control); !os.IsNotExist(err) {
		t.Fatal("control not removed")
	}
}

func TestCleanupFlowRepeatPreservesKeepPausedAndRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	suite := filepath.Join(root, "suite")
	caseRoot := filepath.Join(root, "cases", "case")
	invocation := filepath.Join(root, "invocation")
	for _, path := range []string{suite, caseRoot, invocation} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name    string
		paths   FlowCleanupPaths
		wantErr bool
	}{
		{"keep", FlowCleanupPaths{ControlWorkspace: root, Created: []string{root}, Keep: true}, false},
		{"paused", FlowCleanupPaths{ControlWorkspace: root, Created: []string{root}, Paused: true}, false},
		{"root", FlowCleanupPaths{ControlWorkspace: root, Created: []string{root}}, true},
		{"outside", FlowCleanupPaths{ControlWorkspace: outside, Created: []string{outside}}, true},
		{"suite root", FlowCleanupPaths{ControlWorkspace: suite, Created: []string{suite}}, true},
		{"case root", FlowCleanupPaths{ControlWorkspace: caseRoot, Created: []string{caseRoot}}, true},
		{"invocation root", FlowCleanupPaths{ControlWorkspace: invocation, Created: []string{invocation}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := CleanupFlowRepeat(root, tc.paths); (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
		})
	}
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := CleanupFlowRepeat(root, FlowCleanupPaths{ControlWorkspace: escape, Created: []string{escape}}); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestCleanupFlowRepeatRejectsNonCanonicalRepeatDirectory(t *testing.T) {
	root := t.TempDir()
	for _, repeat := range []string{"repeat-x", "repeat-1", "repeat-0000"} {
		target := filepath.Join(root, "workspaces", "case", repeat, "control")
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatal(err)
		}
		if err := CleanupFlowRepeat(root, FlowCleanupPaths{ControlWorkspace: target, Created: []string{target}}); err == nil {
			t.Fatalf("accepted non-canonical repeat path %q", repeat)
		}
	}
}

func evidenceSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
