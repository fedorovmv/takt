package application

import (
	"context"

	"takt/internal/learning"
)

type LearningPattern = learning.Pattern
type LearningProposal = learning.Proposal
type LearningProposeRequest = learning.ProposeRequest

type LearningService struct{ backend LearningBackend }

func (s *LearningService) Scan(ctx context.Context, minRuns int) ([]LearningPattern, error) {
	return s.backend.Scan(ctx, minRuns)
}
func (s *LearningService) List() ([]*LearningProposal, error)       { return s.backend.List() }
func (s *LearningService) Get(id string) (*LearningProposal, error) { return s.backend.Load(id) }
func (s *LearningService) Propose(ctx context.Context, req LearningProposeRequest) (*LearningProposal, error) {
	return s.backend.Propose(ctx, req)
}
func (s *LearningService) Review(id, decision, reason string) (*LearningProposal, error) {
	return s.backend.Review(id, decision, reason)
}
func (s *LearningService) Evaluate(id, reportPath string) (*LearningProposal, error) {
	return s.backend.Evaluate(id, reportPath)
}
func (s *LearningService) Stage(id string) (*LearningProposal, error) { return s.backend.Stage(id) }
