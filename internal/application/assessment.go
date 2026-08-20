package application

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"takt/internal/assessment"
	"takt/internal/store"
)

type AssessmentQuery struct {
	RunID        string `json:"run_id"`
	Role         string `json:"role,omitempty"`
	IncludeStale bool   `json:"include_stale,omitempty"`
}

type AssessmentRecord struct {
	Assessment assessment.Envelope `json:"assessment"`
	Artifact   store.ArtifactRef   `json:"artifact"`
	Relation   string              `json:"relation"`
	Stale      bool                `json:"stale"`
}

type AssessmentResult struct {
	RunID       string             `json:"run_id"`
	Assessments []AssessmentRecord `json:"assessments"`
}

func (s *RunService) Assessments(query AssessmentQuery) (*AssessmentResult, error) {
	if err := store.ValidateRunID(query.RunID); err != nil {
		return nil, err
	}
	if query.Role != "" && query.Role != assessment.RolePrimary && query.Role != assessment.RoleAdvisory {
		return nil, fmt.Errorf("assessment role must be primary or advisory")
	}
	ids, err := s.store.ListRunIDs()
	if err != nil {
		return nil, err
	}
	runs := make(map[string]*store.RunState, len(ids))
	for _, id := range ids {
		state, err := s.store.Load(id)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load Run %s: %w", id, err)
		}
		runs[id] = state
	}
	if runs[query.RunID] == nil {
		return nil, os.ErrNotExist
	}
	result := &AssessmentResult{RunID: query.RunID, Assessments: []AssessmentRecord{}}
	seen := map[string]bool{}
	for _, id := range ids {
		for _, artifact := range runs[id].Artifacts {
			if artifact.Type != assessment.TypeAssessment {
				continue
			}
			key := artifact.ProducerRunID + "\x00" + artifact.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			value, err := s.readAssessmentArtifact(artifact)
			if err != nil {
				return nil, &assessment.CorruptError{ProducerRunID: artifact.ProducerRunID, ArtifactID: artifact.ID, Err: err}
			}
			relation := ""
			switch {
			case value.Target.RunID == query.RunID:
				relation = "target"
			case value.Assessor.RunID == query.RunID:
				relation = "assessor"
			default:
				continue
			}
			if query.Role != "" && value.Role != query.Role {
				continue
			}
			target := runs[value.Target.RunID]
			if target == nil {
				return nil, &assessment.CorruptError{ProducerRunID: artifact.ProducerRunID, ArtifactID: artifact.ID, Err: fmt.Errorf("target Run %q is missing", value.Target.RunID)}
			}
			resultRevision, err := s.assessmentTargetRevision(target)
			if err != nil {
				return nil, &assessment.CorruptError{ProducerRunID: artifact.ProducerRunID, ArtifactID: artifact.ID, Err: err}
			}
			stale := resultRevision != value.Target.Revision
			if stale && !query.IncludeStale {
				continue
			}
			result.Assessments = append(result.Assessments, AssessmentRecord{Assessment: *value, Artifact: artifact, Relation: relation, Stale: stale})
		}
	}
	sort.Slice(result.Assessments, func(i, j int) bool {
		left, right := result.Assessments[i].Assessment, result.Assessments[j].Assessment
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		if left.Assessor.RunID != right.Assessor.RunID {
			return left.Assessor.RunID < right.Assessor.RunID
		}
		return left.ID < right.ID
	})
	return result, nil
}

func (s *RunService) assessmentTargetRevision(target *store.RunState) (uint64, error) {
	if target.ResultRevision > 0 {
		return target.ResultRevision, nil
	}
	events, err := s.store.ReadEvents(target.ID, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("read target Run events: %w", err)
	}
	for index := len(events) - 1; index >= 0; index-- {
		switch events[index].Type {
		case "run.completed", "run.failed", "run.cancelled", "run.abandoned":
			return events[index].Revision, nil
		}
	}
	return 0, fmt.Errorf("target_revision_unavailable for Run %q", target.ID)
}

func (s *RunService) readAssessmentArtifact(artifact store.ArtifactRef) (*assessment.Envelope, error) {
	if artifact.ProducerRunID == "" {
		return nil, fmt.Errorf("producer_run_id is missing")
	}
	if artifact.MIME != assessment.MIMEAssessment {
		return nil, fmt.Errorf("assessment artifact MIME is %q", artifact.MIME)
	}
	info, err := os.Lstat(artifact.Path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact is not a regular file")
	}
	path, err := filepath.EvalSymlinks(artifact.Path)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(s.store.ArtifactsDir(artifact.ProducerRunID))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("artifact is outside producer Run artifacts")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if artifact.Size != int64(len(raw)) || !strings.EqualFold(artifact.SHA256, hex.EncodeToString(sum[:])) {
		return nil, fmt.Errorf("artifact checksum or size does not match")
	}
	value, err := assessment.Decode(raw)
	if err != nil {
		return nil, err
	}
	if value.ID != artifact.ID || value.Assessor.RunID != artifact.ProducerRunID || value.Assessor.NodeID != artifact.ProducerNodeID {
		return nil, fmt.Errorf("assessment envelope provenance does not match artifact metadata")
	}
	return value, nil
}
