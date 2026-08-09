package dynamicflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"takt/internal/application"
	"takt/internal/experimental/dynamicplan"
)

type ForkRequest struct {
	RunID    string `json:"run_id"`
	Input    string `json:"input,omitempty"`
	Detached bool   `json:"-"`
}

type ForkResult struct {
	SourceRunID string                   `json:"source_run_id"`
	Run         *application.StartResult `json:"run,omitempty"`
	Plan        *PlanResult              `json:"plan,omitempty"`
}

type ForkService struct {
	runs  *application.RunService
	plans *PlanService
}

func (s *ForkService) Fork(ctx context.Context, request ForkRequest) (*ForkResult, error) {
	if record, planErr := s.plans.resolvePlanRecord("", request.RunID); planErr == nil {
		candidate := latestPlan(record)
		if strings.TrimSpace(request.Input) != "" {
			candidate.Goal = strings.TrimSpace(request.Input)
		}
		result, err := s.plans.Plan(ctx, PlanRequest{Goal: candidate.Goal, Profile: record.Profile, Candidate: &candidate, TaskSource: record.TaskSource})
		if err != nil {
			return nil, err
		}
		result.Record.ForkedFromPlanID = record.ID
		result.Record.ForkSourceFingerprint = planForkFingerprint(record)
		if err := s.plans.savePlanRecord(result.Record); err != nil {
			return nil, err
		}
		return &ForkResult{SourceRunID: request.RunID, Plan: result}, nil
	}
	started, err := s.runs.ForkRun(ctx, application.RunForkRequest{RunID: request.RunID, Input: request.Input, Detached: request.Detached})
	if err != nil {
		return nil, err
	}
	return &ForkResult{SourceRunID: request.RunID, Run: started}, nil
}

func planForkFingerprint(record *dynamicplan.Record) string {
	if record == nil {
		return ""
	}
	plan := latestPlan(record)
	sum := sha256.Sum256([]byte(record.BlockCatalogFingerprint + "\x00" + dynamicplan.PlanJSON(plan) + "\x00" + fmt.Sprintf("%d", len(record.Revisions))))
	return hex.EncodeToString(sum[:])
}
