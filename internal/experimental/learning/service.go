package learning

import "context"

type Backend interface {
	Scan(context.Context, int) ([]Pattern, error)
	List() ([]*Proposal, error)
	Load(string) (*Proposal, error)
	Propose(context.Context, ProposeRequest) (*Proposal, error)
	Review(string, string, string) (*Proposal, error)
	Evaluate(string, string) (*Proposal, error)
	Stage(string) (*Proposal, error)
}

type Service struct{ backend Backend }

func NewService(backend Backend) *Service { return &Service{backend: backend} }
func (s *Service) Scan(ctx context.Context, minRuns int) ([]Pattern, error) {
	return s.backend.Scan(ctx, minRuns)
}
func (s *Service) List() ([]*Proposal, error)       { return s.backend.List() }
func (s *Service) Get(id string) (*Proposal, error) { return s.backend.Load(id) }
func (s *Service) Propose(ctx context.Context, req ProposeRequest) (*Proposal, error) {
	return s.backend.Propose(ctx, req)
}
func (s *Service) Review(id, decision, reason string) (*Proposal, error) {
	return s.backend.Review(id, decision, reason)
}
func (s *Service) Evaluate(id, reportPath string) (*Proposal, error) {
	return s.backend.Evaluate(id, reportPath)
}
func (s *Service) Stage(id string) (*Proposal, error) { return s.backend.Stage(id) }
