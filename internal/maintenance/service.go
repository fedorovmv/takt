package maintenance

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type PlanAdvancer interface{ AdvanceDynamicPlans(context.Context) error }
type ExternalExpirer interface {
	ExpireIdle(context.Context, time.Time) ([]string, error)
}

type Result struct {
	ExpiredExternal []string `json:"expired_external,omitempty"`
	Notifications   int      `json:"notifications,omitempty"`
}

type Service struct {
	plans    PlanAdvancer
	external ExternalExpirer
	dispatch func() (int, error)
}

func New(plans PlanAdvancer, external ExternalExpirer, dispatch func() (int, error)) *Service {
	return &Service{plans: plans, external: external, dispatch: dispatch}
}

func (s *Service) Tick(ctx context.Context, now time.Time) (*Result, error) {
	if s == nil || s.plans == nil || s.external == nil || s.dispatch == nil {
		return nil, fmt.Errorf("maintenance service is not fully configured")
	}
	var failures []string
	if err := s.plans.AdvanceDynamicPlans(ctx); err != nil {
		failures = append(failures, "advance plans: "+err.Error())
	}
	expired, err := s.external.ExpireIdle(ctx, now.UTC())
	if err != nil {
		failures = append(failures, "expire external: "+err.Error())
	}
	emitted, err := s.dispatch()
	if err != nil {
		failures = append(failures, "dispatch notifications: "+err.Error())
	}
	result := &Result{ExpiredExternal: expired, Notifications: emitted}
	if len(failures) > 0 {
		return result, fmt.Errorf("maintenance: %s", strings.Join(failures, "; "))
	}
	return result, nil
}
