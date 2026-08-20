package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"takt/internal/assessment"
	"takt/internal/execution"
	"takt/internal/flowref"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/validation"
	"takt/internal/workflow"
)

const assessmentMIME = "application/vnd.takt.assessment+json"

var errAssessmentAmbiguous = errors.New("assessment is ambiguous")

func (r *Runner) executeAssessmentAction(state *store.RunState, node spec.Node, action actionContext) (execResult, error) {
	definition := node.Assessment
	if definition == nil {
		return execResult{}, assessmentExecutionError(fmt.Errorf("node %q has no assessment definition", node.ID))
	}
	resultRef, err := flowref.Parse(definition.ResultFrom, flowref.NonShell)
	if err != nil {
		return execResult{}, assessmentExecutionError(err)
	}
	if definition.Role == assessment.RolePrimary && !r.deterministicAssessmentSource(resultRef.NodeID) {
		return execResult{}, assessmentExecutionError(fmt.Errorf("primary result_from source %q is not deterministic", resultRef.NodeID))
	}
	targetRunID, err := renderTemplate(definition.TargetRunID, state, action.local, action.feedback, action.artifacts)
	if err != nil {
		return execResult{}, assessmentExecutionError(fmt.Errorf("render target_run_id: %w", err))
	}
	target, err := r.store.Load(strings.TrimSpace(targetRunID))
	if err != nil {
		return execResult{}, assessmentExecutionError(fmt.Errorf("load target Run: %w", err))
	}
	if target.ResultRevision == 0 || target.ResultRevision > target.Revision {
		return execResult{}, assessmentExecutionError(fmt.Errorf("target Run %q has no terminal result revision", target.ID))
	}
	outcome, err := assessment.Outcome(target.Status, false)
	if err != nil {
		return execResult{}, assessmentExecutionError(err)
	}
	renderedResult, err := renderTemplate(definition.ResultFrom, state, action.local, action.feedback, action.artifacts)
	if err != nil {
		return execResult{}, assessmentExecutionError(fmt.Errorf("render result_from: %w", err))
	}
	validationResult, err := validation.Decode([]byte(renderedResult))
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindProtocol, Op: "decode assessment result", Err: err}
	}
	outcome, err = assessment.Outcome(target.Status, validationResult.Valid)
	if err != nil {
		return execResult{}, assessmentExecutionError(err)
	}
	scope, err := renderAssessmentScope(definition.Scope, state, action)
	if err != nil {
		return execResult{}, assessmentExecutionError(err)
	}
	evidence, err := r.resolveAssessmentEvidence(state, definition.Evidence, action.local)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.Kind("evidence_missing"), Op: "record assessment", Err: err}
	}

	nodeState := state.Nodes[node.ID]
	id := fmt.Sprintf("%s:%s:%d", node.ID, assessment.TypeAssessment, nodeState.Attempts)
	if existing, ok, err := r.existingAssessment(state, id, definition.Role, scope); err != nil {
		if errors.Is(err, errAssessmentAmbiguous) {
			return execResult{}, &execution.Error{Kind: execution.Kind("assessment_ambiguous"), Op: "record assessment", Err: err}
		}
		return execResult{}, assessmentExecutionError(err)
	} else if ok {
		raw, readErr := os.ReadFile(existing.Path)
		if readErr != nil {
			return execResult{}, assessmentExecutionError(readErr)
		}
		return execResult{Output: string(raw), Stdout: string(raw), Artifacts: []store.ArtifactRef{existing}}, nil
	}

	createdAt := time.Now().UTC()
	envelope := assessment.Envelope{
		ProtocolVersion: assessment.ProtocolV1Alpha1,
		Type:            assessment.TypeAssessment,
		ID:              id,
		Role:            definition.Role,
		Target: assessment.Target{
			RunID: target.ID, Revision: target.ResultRevision, Status: target.Status,
			WorkflowFingerprint: target.WorkflowFingerprint, ConfigFingerprint: target.ConfigFingerprint,
		},
		Assessor:  assessment.Assessor{RunID: state.ID, NodeID: node.ID, Revision: state.Revision + 1},
		Scope:     scope,
		Result:    *validationResult,
		Outcome:   outcome,
		Evidence:  evidence,
		CreatedAt: createdAt,
	}
	if err := envelope.Validate(); err != nil {
		return execResult{}, assessmentExecutionError(err)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return execResult{}, assessmentExecutionError(err)
	}
	if r.redactor != nil {
		if redacted, found := r.redactor.Bytes(raw); found {
			raw = redacted
			if _, err := assessment.Decode(raw); err != nil {
				return execResult{}, assessmentExecutionError(fmt.Errorf("redact assessment: %w", err))
			}
		}
	}
	currentTarget, err := r.store.Load(target.ID)
	if err != nil {
		return execResult{}, assessmentExecutionError(fmt.Errorf("reload target Run: %w", err))
	}
	if currentTarget.ResultRevision != target.ResultRevision || currentTarget.Status != target.Status {
		return execResult{}, assessmentExecutionError(fmt.Errorf("target Run %q result changed during assessment capture", target.ID))
	}
	dir := filepath.Join(r.store.ArtifactsDir(state.ID), "nodes", safeArtifactPart(node.ID), strconv.Itoa(nodeState.Attempts))
	path := filepath.Join(dir, "assessment.json")
	if err := writeAtomicArtifact(path, raw); err != nil {
		return execResult{}, assessmentExecutionError(err)
	}
	sum := sha256.Sum256(raw)
	artifact := store.ArtifactRef{
		ID: id, Type: assessment.TypeAssessment, MIME: assessmentMIME, Path: path,
		SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw)), ProducerRunID: state.ID,
		ProducerNodeID: node.ID, Attempt: nodeState.Attempts, CreatedAt: createdAt,
	}
	previousNodeArtifacts := cloneArtifacts(nodeState.Artifacts)
	previousRunArtifacts := cloneArtifacts(state.Artifacts)
	nodeState.Artifacts = appendArtifactUnique(nodeState.Artifacts, artifact)
	state.Artifacts = appendArtifactUnique(state.Artifacts, artifact)
	if err := r.commit(state, "assessment.recorded", node.ID, map[string]any{
		"assessment_id": id, "role": definition.Role, "target_run_id": target.ID, "target_revision": target.ResultRevision,
	}); err != nil {
		nodeState.Artifacts = previousNodeArtifacts
		state.Artifacts = previousRunArtifacts
		_ = os.Remove(path)
		return execResult{}, err
	}
	return execResult{Output: string(raw), Stdout: string(raw), ExitCode: 0, Artifacts: []store.ArtifactRef{artifact}}, nil
}

func assessmentExecutionError(err error) error {
	return &execution.Error{Kind: execution.KindInternal, Op: "record assessment", Err: err}
}

func renderAssessmentScope(values map[string]string, state *store.RunState, action actionContext) (assessment.Scope, error) {
	caseID, err := renderTemplate(values["case_id"], state, action.local, action.feedback, action.artifacts)
	if err != nil {
		return assessment.Scope{}, fmt.Errorf("render assessment case_id: %w", err)
	}
	repeatValue, err := renderTemplate(values["repeat"], state, action.local, action.feedback, action.artifacts)
	if err != nil {
		return assessment.Scope{}, fmt.Errorf("render assessment repeat: %w", err)
	}
	repeat := 0
	if strings.TrimSpace(repeatValue) != "" {
		repeat, err = strconv.Atoi(strings.TrimSpace(repeatValue))
		if err != nil || repeat < 0 {
			return assessment.Scope{}, fmt.Errorf("assessment scope.repeat must be a non-negative integer")
		}
	}
	return assessment.Scope{CaseID: caseID, Repeat: repeat}, nil
}

func (r *Runner) deterministicAssessmentSource(nodeID string) bool {
	return deterministicAssessmentProducer(r.workflow.Nodes, nodeID, r.workflowPath, 0)
}

func deterministicAssessmentProducer(nodes []spec.Node, nodeID, workflowPath string, depth int) bool {
	if depth > 16 {
		return false
	}
	for _, node := range nodes {
		if node.ID != nodeID {
			continue
		}
		if node.Bash != "" || node.Script != nil || node.Adapter != nil {
			return true
		}
		if node.Internal != nil && node.Internal.Mode == "result" {
			return deterministicAssessmentProducer(nodes, node.Internal.ResultFrom, workflowPath, depth+1)
		}
		if node.WorkflowRun == nil {
			return false
		}
		path := node.WorkflowRun.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(workflowPath), path)
		}
		child, err := workflow.Load(path)
		if err != nil {
			return false
		}
		outputNode := node.WorkflowRun.OutputNode
		if outputNode == "" {
			outputNode = singleTerminalNode(child.Nodes)
		}
		return outputNode != "" && deterministicAssessmentProducer(child.Nodes, outputNode, path, depth+1)
	}
	return false
}

func (r *Runner) resolveAssessmentEvidence(state *store.RunState, sources []string, local map[string]store.NodeState) ([]assessment.EvidenceRef, error) {
	values := make([]assessment.EvidenceRef, 0, len(sources))
	for _, source := range sources {
		ref, err := flowref.Parse(source, flowref.NonShell)
		if err != nil || ref.Kind != flowref.KindNode || len(ref.Path) != 2 || ref.Path[0] != "artifacts" {
			return nil, fmt.Errorf("invalid assessment evidence reference %q", source)
		}
		var node *store.NodeState
		if value := state.Nodes[ref.NodeID]; value != nil {
			node = value
		} else if value, ok := local[ref.NodeID]; ok {
			copy := value
			node = &copy
		}
		if node == nil {
			return nil, fmt.Errorf("assessment evidence node %q is missing", ref.NodeID)
		}
		var artifact *store.ArtifactRef
		for index := range node.Artifacts {
			if node.Artifacts[index].Type == ref.Path[1] {
				artifact = &node.Artifacts[index]
				break
			}
		}
		if artifact == nil {
			return nil, fmt.Errorf("assessment evidence artifact %q is missing", ref.Path[1])
		}
		if err := r.verifyAssessmentArtifact(*artifact); err != nil {
			return nil, err
		}
		values = append(values, assessment.EvidenceRef{ProducerRunID: artifact.ProducerRunID, ArtifactID: artifact.ID, SHA256: artifact.SHA256})
	}
	return values, nil
}

func (r *Runner) verifyAssessmentArtifact(artifact store.ArtifactRef) error {
	info, err := os.Lstat(artifact.Path)
	if err != nil {
		return fmt.Errorf("assessment evidence %q: %w", artifact.ID, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("assessment evidence %q is not a regular file", artifact.ID)
	}
	path, err := filepath.EvalSymlinks(artifact.Path)
	if err != nil {
		return fmt.Errorf("assessment evidence %q: %w", artifact.ID, err)
	}
	root, err := filepath.EvalSymlinks(r.store.ArtifactsDir(artifact.ProducerRunID))
	if err != nil {
		return fmt.Errorf("assessment evidence %q producer artifact directory: %w", artifact.ID, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("assessment evidence %q is outside producer Run artifacts", artifact.ID)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("assessment evidence %q: %w", artifact.ID, err)
	}
	sum := sha256.Sum256(raw)
	if artifact.Size != int64(len(raw)) || !strings.EqualFold(artifact.SHA256, hex.EncodeToString(sum[:])) {
		return fmt.Errorf("assessment evidence %q checksum or size does not match", artifact.ID)
	}
	return nil
}

func (r *Runner) existingAssessment(state *store.RunState, id, role string, scope assessment.Scope) (store.ArtifactRef, bool, error) {
	for _, artifact := range state.Artifacts {
		if artifact.Type != assessment.TypeAssessment || artifact.ProducerRunID != state.ID {
			continue
		}
		raw, err := os.ReadFile(artifact.Path)
		if err != nil {
			return store.ArtifactRef{}, false, err
		}
		value, err := assessment.Decode(raw)
		if err != nil {
			return store.ArtifactRef{}, false, err
		}
		if artifact.ID == id {
			return artifact, true, nil
		}
		if role == assessment.RolePrimary && value.Role == assessment.RolePrimary && value.Scope == scope {
			target, err := r.store.Load(value.Target.RunID)
			if err != nil {
				return store.ArtifactRef{}, false, fmt.Errorf("load existing assessment target: %w", err)
			}
			if target.ResultRevision == value.Target.Revision {
				return store.ArtifactRef{}, false, fmt.Errorf("%w: primary scope %q repeat %d", errAssessmentAmbiguous, scope.CaseID, scope.Repeat)
			}
		}
	}
	return store.ArtifactRef{}, false, nil
}

func writeAtomicArtifact(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".assessment-*.tmp")
	if err != nil {
		return err
	}
	tmp := file.Name()
	ok := false
	published := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(tmp)
			if published {
				_ = os.Remove(path)
			}
		}
	}()
	if err := file.Chmod(0o644); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Link(tmp, path); err != nil {
		return err
	}
	published = true
	if err := os.Remove(tmp); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}
