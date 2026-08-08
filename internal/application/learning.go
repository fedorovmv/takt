package application

import (
	"context"

	"takt/internal/learning"
)

type LearningPattern = learning.Pattern
type LearningProposal = learning.Proposal
type LearningProposeRequest = learning.ProposeRequest

type LearningService struct{ *Context }

func (s *LearningService) Scan(ctx context.Context, minRuns int) ([]LearningPattern, error) {
	return (learning.Manager{Workspace: s.Workspace}).Scan(ctx, minRuns)
}

func (s *LearningService) List() ([]*LearningProposal, error) {
	return (learning.Manager{Workspace: s.Workspace}).List()
}

func (s *LearningService) Get(id string) (*LearningProposal, error) {
	return (learning.Manager{Workspace: s.Workspace}).Load(id)
}

func (s *LearningService) Propose(ctx context.Context, req LearningProposeRequest) (*LearningProposal, error) {
	return (learning.Manager{Workspace: s.Workspace}).Propose(ctx, req)
}

func (s *LearningService) Review(id, decision, reason string) (*LearningProposal, error) {
	return (learning.Manager{Workspace: s.Workspace}).Review(id, decision, reason)
}

func (s *LearningService) Evaluate(id, reportPath string) (*LearningProposal, error) {
	return (learning.Manager{Workspace: s.Workspace}).Evaluate(id, reportPath)
}

func (s *LearningService) Stage(id string) (*LearningProposal, error) {
	return (learning.Manager{Workspace: s.Workspace}).Stage(id)
}
