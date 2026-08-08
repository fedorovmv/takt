package learning

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"takt/internal/blockcatalog"
	"takt/internal/evaluation"
	"takt/internal/store"
)

const (
	APIVersion = "takt-learning/v1alpha1"
	Kind       = "LearningProposal"

	StatusPending          = "pending_review"
	StatusAccepted         = "accepted"
	StatusRejected         = "rejected"
	StatusEvaluationFailed = "evaluation_failed"
	StatusReady            = "ready"
)

var candidateNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var proposalIDPattern = regexp.MustCompile(`^learn-[0-9a-f]{24}$`)
var sha256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Pattern struct {
	Kind         string   `json:"kind"`
	Fingerprint  string   `json:"fingerprint"`
	Count        int      `json:"count"`
	RunIDs       []string `json:"run_ids"`
	Summary      string   `json:"summary"`
	WorkflowPath string   `json:"workflow_path,omitempty"`
	NodeID       string   `json:"node_id,omitempty"`
}

type Candidate struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	SnapshotPath string `json:"snapshot_path"`
	SHA256       string `json:"sha256"`
}

type Review struct {
	Decision string    `json:"decision"`
	Reason   string    `json:"reason"`
	At       time.Time `json:"at"`
}

type Evaluation struct {
	ReportVersion     string    `json:"report_version"`
	ReportSHA256      string    `json:"report_sha256"`
	MatrixFingerprint string    `json:"matrix_fingerprint"`
	BenchmarkID       string    `json:"benchmark_id"`
	GateCount         int       `json:"gate_count"`
	Passed            bool      `json:"passed"`
	At                time.Time `json:"at"`
}

type Proposal struct {
	APIVersion      string      `json:"apiVersion"`
	Kind            string      `json:"kind"`
	ID              string      `json:"id"`
	Status          string      `json:"status"`
	Pattern         Pattern     `json:"pattern"`
	Candidate       Candidate   `json:"candidate"`
	ExpectedBenefit string      `json:"expected_benefit"`
	Review          *Review     `json:"review,omitempty"`
	Evaluation      *Evaluation `json:"evaluation,omitempty"`
	ReadyPath       string      `json:"ready_path,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type Manager struct{ Workspace string }

type ProposeRequest struct {
	PatternFingerprint string
	CandidateKind      string
	Name               string
	CandidatePath      string
	ExpectedBenefit    string
	MinRuns            int
}

type evaluationEnvelope struct {
	ReportVersion     string                  `json:"report_version"`
	MatrixFingerprint string                  `json:"matrix_fingerprint"`
	BenchmarkID       string                  `json:"benchmark_id"`
	Passed            bool                    `json:"passed"`
	Gates             []evaluation.GateResult `json:"gates"`
}

func (m Manager) root() string                 { return filepath.Join(m.Workspace, ".takt", "learning") }
func (m Manager) proposalRoot() string         { return filepath.Join(m.root(), "proposals") }
func (m Manager) proposalDir(id string) string { return filepath.Join(m.proposalRoot(), id) }
func (m Manager) proposalPath(id string) string {
	return filepath.Join(m.proposalDir(id), "proposal.json")
}

func (m Manager) Scan(ctx context.Context, minRuns int) ([]Pattern, error) {
	if minRuns <= 0 {
		minRuns = 2
	}
	if minRuns < 2 {
		return nil, fmt.Errorf("learning scan min-runs must be at least 2")
	}
	st := store.FS{Workspace: m.Workspace}
	ids, err := st.ListRunIDs()
	if err != nil {
		return nil, err
	}
	type aggregate struct {
		pattern Pattern
		runs    map[string]bool
	}
	groups := map[string]*aggregate{}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		state, err := st.Load(id)
		if err != nil {
			return nil, fmt.Errorf("load run %s: %w", id, err)
		}
		seenDiagnostic := map[string]bool{}
		for nodeID, node := range state.Nodes {
			if node == nil || node.Diagnostic == nil || strings.TrimSpace(node.Diagnostic.Fingerprint) == "" {
				continue
			}
			fp := "diagnostic:" + strings.TrimSpace(node.Diagnostic.Fingerprint)
			if seenDiagnostic[fp] {
				continue
			}
			seenDiagnostic[fp] = true
			group := groups[fp]
			if group == nil {
				summary := strings.TrimSpace(node.Diagnostic.Code + " " + node.Diagnostic.Message)
				group = &aggregate{pattern: Pattern{Kind: "diagnostic", Fingerprint: fp, Summary: summary, WorkflowPath: state.WorkflowPath, NodeID: nodeID}, runs: map[string]bool{}}
				groups[fp] = group
			}
			if group.pattern.WorkflowPath != state.WorkflowPath || group.pattern.NodeID != nodeID {
				group.pattern.WorkflowPath = ""
				group.pattern.NodeID = ""
			}
			group.runs[id] = true
		}
		if state.Status == store.RunCompleted && strings.TrimSpace(state.WorkflowFingerprint) != "" {
			fp := "workflow:" + definitionFingerprint(state.WorkflowFingerprint, state.ConfigFingerprint, state.CommandsFingerprint)
			group := groups[fp]
			if group == nil {
				group = &aggregate{pattern: Pattern{Kind: "workflow_success", Fingerprint: fp, Summary: "repeated successful workflow " + filepath.Base(state.WorkflowPath), WorkflowPath: state.WorkflowPath}, runs: map[string]bool{}}
				groups[fp] = group
			}
			group.runs[id] = true
		}
	}
	out := make([]Pattern, 0, len(groups))
	for _, group := range groups {
		if len(group.runs) < minRuns {
			continue
		}
		for id := range group.runs {
			group.pattern.RunIDs = append(group.pattern.RunIDs, id)
		}
		sort.Strings(group.pattern.RunIDs)
		group.pattern.Count = len(group.pattern.RunIDs)
		out = append(out, group.pattern)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Fingerprint < out[j].Fingerprint
	})
	return out, nil
}

func (m Manager) Propose(ctx context.Context, req ProposeRequest) (*Proposal, error) {
	patterns, err := m.Scan(ctx, req.MinRuns)
	if err != nil {
		return nil, err
	}
	var pattern *Pattern
	for i := range patterns {
		if patterns[i].Fingerprint == strings.TrimSpace(req.PatternFingerprint) {
			copy := patterns[i]
			pattern = &copy
			break
		}
	}
	if pattern == nil {
		return nil, fmt.Errorf("learning pattern %q is not present in the current repeated-run scan", req.PatternFingerprint)
	}
	kind := strings.TrimSpace(req.CandidateKind)
	if kind != "skill" && kind != "block" {
		return nil, fmt.Errorf("candidate kind must be skill or block")
	}
	name := strings.TrimSpace(req.Name)
	if !candidateNamePattern.MatchString(name) {
		return nil, fmt.Errorf("candidate name must match %s", candidateNamePattern.String())
	}
	benefit := strings.TrimSpace(req.ExpectedBenefit)
	if benefit == "" {
		return nil, fmt.Errorf("expected benefit is required")
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	dir := m.proposalDir(id)
	candidateDir := filepath.Join(dir, "candidate")
	if err := os.MkdirAll(candidateDir, 0o700); err != nil {
		return nil, err
	}
	var snapshot string
	if kind == "skill" {
		snapshot = filepath.Join(candidateDir, "SKILL.md")
		if strings.TrimSpace(req.CandidatePath) == "" {
			if err := os.WriteFile(snapshot, []byte(generatedSkill(name, *pattern, benefit)), 0o600); err != nil {
				return nil, err
			}
		} else if err := copyRegularFile(req.CandidatePath, snapshot); err != nil {
			return nil, err
		}
		if err := validateSkill(snapshot, name); err != nil {
			return nil, err
		}
	} else {
		if strings.TrimSpace(req.CandidatePath) == "" {
			return nil, fmt.Errorf("block candidate requires --candidate path to a BlockPackage package.yaml")
		}
		original, err := filepath.Abs(req.CandidatePath)
		if err != nil {
			return nil, err
		}
		catalog, err := blockcatalog.LoadOne(original)
		if err != nil {
			return nil, fmt.Errorf("validate block candidate: %w", err)
		}
		if len(catalog.Blocks) != 1 {
			return nil, fmt.Errorf("block candidate package must contain exactly one block")
		}
		if _, ok := catalog.Blocks[name]; !ok {
			return nil, fmt.Errorf("block candidate package must contain block %q", name)
		}
		packageDir := filepath.Join(candidateDir, "package")
		if err := copyTree(filepath.Dir(original), packageDir); err != nil {
			return nil, err
		}
		snapshot = filepath.Join(packageDir, filepath.Base(original))
		if _, err := blockcatalog.LoadOne(snapshot); err != nil {
			return nil, fmt.Errorf("validate copied block candidate: %w", err)
		}
	}
	sha, err := hashCandidate(candidateDir)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	proposal := &Proposal{APIVersion: APIVersion, Kind: Kind, ID: id, Status: StatusPending, Pattern: *pattern, Candidate: Candidate{Kind: kind, Name: name, SnapshotPath: relativeToWorkspace(m.Workspace, snapshot), SHA256: sha}, ExpectedBenefit: benefit, CreatedAt: now, UpdatedAt: now}
	if err := m.Save(proposal); err != nil {
		return nil, err
	}
	return proposal, nil
}

func (m Manager) Review(id, decision, reason string) (*Proposal, error) {
	proposal, err := m.Load(id)
	if err != nil {
		return nil, err
	}
	if proposal.Status != StatusPending && proposal.Status != StatusEvaluationFailed {
		return nil, fmt.Errorf("proposal %s cannot be reviewed from status %s", id, proposal.Status)
	}
	decision = strings.TrimSpace(decision)
	if decision != "accept" && decision != "reject" {
		return nil, fmt.Errorf("review decision must be accept or reject")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("review reason is required")
	}
	now := time.Now().UTC()
	proposal.Review = &Review{Decision: decision, Reason: reason, At: now}
	proposal.Evaluation = nil
	proposal.ReadyPath = ""
	if decision == "accept" {
		proposal.Status = StatusAccepted
	} else {
		proposal.Status = StatusRejected
	}
	proposal.UpdatedAt = now
	if err := m.Save(proposal); err != nil {
		return nil, err
	}
	return proposal, nil
}

func (m Manager) Evaluate(id, reportPath string) (*Proposal, error) {
	proposal, err := m.Load(id)
	if err != nil {
		return nil, err
	}
	if proposal.Status != StatusAccepted && proposal.Status != StatusEvaluationFailed {
		return nil, fmt.Errorf("proposal %s must be accepted before evaluation", id)
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		return nil, err
	}
	var report evaluationEnvelope
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("decode evaluation report: %w", err)
	}
	if report.ReportVersion != evaluation.MatrixReportVersion && report.ReportVersion != evaluation.TaskMatrixReportVersion {
		return nil, fmt.Errorf("learning evaluation requires matrix report, got %q", report.ReportVersion)
	}
	if strings.TrimSpace(report.MatrixFingerprint) == "" || strings.TrimSpace(report.BenchmarkID) == "" {
		return nil, fmt.Errorf("learning evaluation requires matrix_fingerprint and benchmark_id provenance")
	}
	if len(report.Gates) == 0 {
		return nil, fmt.Errorf("learning evaluation report must contain at least one regression gate")
	}
	passed := report.Passed
	for _, gate := range report.Gates {
		passed = passed && gate.Passed
	}
	sum := sha256.Sum256(raw)
	evalDir := filepath.Join(m.proposalDir(id), "evaluation")
	if err := os.MkdirAll(evalDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(evalDir, "report.json"), raw, 0o600); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	proposal.Evaluation = &Evaluation{ReportVersion: report.ReportVersion, ReportSHA256: "sha256:" + hex.EncodeToString(sum[:]), MatrixFingerprint: report.MatrixFingerprint, BenchmarkID: report.BenchmarkID, GateCount: len(report.Gates), Passed: passed, At: now}
	proposal.ReadyPath = ""
	if passed {
		proposal.Status = StatusAccepted
	} else {
		proposal.Status = StatusEvaluationFailed
	}
	proposal.UpdatedAt = now
	if err := m.Save(proposal); err != nil {
		return nil, err
	}
	return proposal, nil
}

func (m Manager) Stage(id string) (*Proposal, error) {
	proposal, err := m.Load(id)
	if err != nil {
		return nil, err
	}
	if proposal.Status != StatusAccepted || proposal.Review == nil || proposal.Review.Decision != "accept" || proposal.Evaluation == nil || !proposal.Evaluation.Passed {
		return nil, fmt.Errorf("proposal %s requires human acceptance and a passing evaluation gate before staging", id)
	}
	candidateDir := filepath.Join(m.proposalDir(id), "candidate")
	sha, err := hashCandidate(candidateDir)
	if err != nil {
		return nil, err
	}
	if sha != proposal.Candidate.SHA256 {
		return nil, fmt.Errorf("proposal %s candidate snapshot changed after review", id)
	}
	ready := filepath.Join(m.root(), "ready", id)
	if err := os.RemoveAll(ready); err != nil {
		return nil, err
	}
	if err := copyTree(candidateDir, ready); err != nil {
		return nil, err
	}
	proposal.Status = StatusReady
	proposal.ReadyPath = relativeToWorkspace(m.Workspace, ready)
	proposal.UpdatedAt = time.Now().UTC()
	if err := m.Save(proposal); err != nil {
		return nil, err
	}
	return proposal, nil
}

func (m Manager) Save(proposal *Proposal) error {
	if proposal == nil || !strings.HasPrefix(proposal.ID, "learn-") || strings.ContainsAny(proposal.ID, `/\\`) {
		return fmt.Errorf("invalid learning proposal")
	}
	raw, err := json.MarshalIndent(proposal, "", "  ")
	if err != nil {
		return err
	}
	dir := m.proposalDir(proposal.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".proposal-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, m.proposalPath(proposal.ID))
}

func (m Manager) Load(id string) (*Proposal, error) {
	if !strings.HasPrefix(id, "learn-") || strings.ContainsAny(id, `/\\`) {
		return nil, fmt.Errorf("invalid learning proposal id %q", id)
	}
	raw, err := os.ReadFile(m.proposalPath(id))
	if err != nil {
		return nil, err
	}
	var proposal Proposal
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&proposal); err != nil {
		return nil, err
	}
	if proposal.ID != id {
		return nil, fmt.Errorf("invalid learning proposal contract: id mismatch")
	}
	if err := validateProposal(&proposal); err != nil {
		return nil, err
	}
	return &proposal, nil
}

func (m Manager) List() ([]*Proposal, error) {
	entries, err := os.ReadDir(m.proposalRoot())
	if errors.Is(err, os.ErrNotExist) {
		return []*Proposal{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]*Proposal, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		proposal, err := m.Load(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("load learning proposal %s: %w", entry.Name(), err)
		}
		out = append(out, proposal)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func definitionFingerprint(workflow, config, commands string) string {
	h := sha256.New()
	for _, value := range []string{workflow, config, commands} {
		h.Write([]byte(value))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func validateProposal(proposal *Proposal) error {
	if proposal == nil || proposal.APIVersion != APIVersion || proposal.Kind != Kind || !proposalIDPattern.MatchString(proposal.ID) {
		return fmt.Errorf("invalid learning proposal contract")
	}
	validStatus := map[string]bool{StatusPending: true, StatusAccepted: true, StatusRejected: true, StatusEvaluationFailed: true, StatusReady: true}
	if !validStatus[proposal.Status] {
		return fmt.Errorf("invalid learning proposal status %q", proposal.Status)
	}
	if proposal.Pattern.Kind != "diagnostic" && proposal.Pattern.Kind != "workflow_success" {
		return fmt.Errorf("invalid learning pattern kind %q", proposal.Pattern.Kind)
	}
	if strings.TrimSpace(proposal.Pattern.Fingerprint) == "" || proposal.Pattern.Count < 2 || len(proposal.Pattern.RunIDs) != proposal.Pattern.Count || strings.TrimSpace(proposal.Pattern.Summary) == "" {
		return fmt.Errorf("invalid learning pattern provenance")
	}
	seen := map[string]bool{}
	for _, id := range proposal.Pattern.RunIDs {
		if strings.TrimSpace(id) == "" || seen[id] {
			return fmt.Errorf("invalid learning pattern run ids")
		}
		seen[id] = true
	}
	if (proposal.Candidate.Kind != "skill" && proposal.Candidate.Kind != "block") || !candidateNamePattern.MatchString(proposal.Candidate.Name) || strings.TrimSpace(proposal.Candidate.SnapshotPath) == "" || !sha256Pattern.MatchString(proposal.Candidate.SHA256) {
		return fmt.Errorf("invalid learning candidate contract")
	}
	if strings.TrimSpace(proposal.ExpectedBenefit) == "" || proposal.CreatedAt.IsZero() || proposal.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid learning proposal metadata")
	}
	if proposal.Review != nil {
		if (proposal.Review.Decision != "accept" && proposal.Review.Decision != "reject") || strings.TrimSpace(proposal.Review.Reason) == "" || proposal.Review.At.IsZero() {
			return fmt.Errorf("invalid learning review contract")
		}
	}
	if proposal.Evaluation != nil {
		if proposal.Evaluation.ReportVersion != evaluation.MatrixReportVersion && proposal.Evaluation.ReportVersion != evaluation.TaskMatrixReportVersion {
			return fmt.Errorf("invalid learning evaluation report version")
		}
		if !sha256Pattern.MatchString(proposal.Evaluation.ReportSHA256) || strings.TrimSpace(proposal.Evaluation.MatrixFingerprint) == "" || strings.TrimSpace(proposal.Evaluation.BenchmarkID) == "" || proposal.Evaluation.GateCount < 1 || proposal.Evaluation.At.IsZero() {
			return fmt.Errorf("invalid learning evaluation contract")
		}
	}
	switch proposal.Status {
	case StatusPending:
		if proposal.Review != nil || proposal.Evaluation != nil || proposal.ReadyPath != "" {
			return fmt.Errorf("pending learning proposal contains completed gates")
		}
	case StatusAccepted:
		if proposal.Review == nil || proposal.Review.Decision != "accept" || proposal.ReadyPath != "" || (proposal.Evaluation != nil && !proposal.Evaluation.Passed) {
			return fmt.Errorf("accepted learning proposal has inconsistent gates")
		}
	case StatusRejected:
		if proposal.Review == nil || proposal.Review.Decision != "reject" || proposal.Evaluation != nil || proposal.ReadyPath != "" {
			return fmt.Errorf("rejected learning proposal has inconsistent gates")
		}
	case StatusEvaluationFailed:
		if proposal.Review == nil || proposal.Review.Decision != "accept" || proposal.Evaluation == nil || proposal.Evaluation.Passed || proposal.ReadyPath != "" {
			return fmt.Errorf("failed learning evaluation has inconsistent gates")
		}
	case StatusReady:
		if proposal.Review == nil || proposal.Review.Decision != "accept" || proposal.Evaluation == nil || !proposal.Evaluation.Passed || strings.TrimSpace(proposal.ReadyPath) == "" {
			return fmt.Errorf("ready learning proposal has incomplete gates")
		}
	}
	return nil
}

func generatedSkill(name string, pattern Pattern, benefit string) string {
	return fmt.Sprintf(`---
name: %s
description: Candidate learned from repeated Takt runs. Requires human review and evaluation before use.
---

# %s

## When to use

Apply this guidance when Takt observes the repeated pattern below.

- Pattern: %s
- Summary: %s
- Observed runs: %s

## Guidance

1. Inspect the repeated condition before changing code or workflow behavior.
2. Preserve the deterministic checks that exposed or confirmed the pattern.
3. Apply the smallest reusable correction and rerun the same evaluation matrix.

## Expected benefit

%s
`, name, name, pattern.Fingerprint, pattern.Summary, strings.Join(pattern.RunIDs, ", "), benefit)
}

func validateSkill(path, expectedName string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(text, "---\n") {
		return fmt.Errorf("skill candidate must contain YAML frontmatter")
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return fmt.Errorf("skill candidate must contain YAML frontmatter")
	}
	frontmatter := rest[:end]
	fields := map[string]string{}
	for _, line := range strings.Split(frontmatter, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "name" || key == "description" {
			fields[key] = value
		}
	}
	if fields["name"] == "" || fields["description"] == "" {
		return fmt.Errorf("skill candidate frontmatter requires non-empty name and description")
	}
	if fields["name"] != expectedName {
		return fmt.Errorf("skill candidate frontmatter name %q must match candidate name %q", fields["name"], expectedName)
	}
	return nil
}

func newID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "learn-" + hex.EncodeToString(buf), nil
}

func copyRegularFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("candidate %s must be a regular file", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("candidate tree contains symlink %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("candidate tree contains non-regular file %s", path)
		}
		return copyRegularFile(path, target)
	})
}

func hashCandidate(root string) (string, error) {
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("candidate contains symlink %s", path)
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, path := range files {
		rel, _ := filepath.Rel(root, path)
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		h.Write(raw)
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func relativeToWorkspace(workspace, path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return abs
	}
	if rel, err := filepath.Rel(root, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return abs
}
