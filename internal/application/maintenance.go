package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MaintenanceResult is the deterministic result of one application maintenance
// tick. Long-running transports choose only how to schedule and report it.
type MaintenanceResult struct {
	ExpiredExternal []string `json:"expired_external,omitempty"`
	Notifications   int      `json:"notifications,omitempty"`
}

type MaintenanceService struct {
	Plans         *PlanService
	External      *ExternalService
	Notifications *NotificationService
}

func (s *MaintenanceService) Tick(ctx context.Context, now time.Time) (*MaintenanceResult, error) {
	if s == nil || s.Plans == nil || s.External == nil || s.Notifications == nil {
		return nil, fmt.Errorf("maintenance service is not fully configured")
	}
	var failures []string
	if err := s.Plans.AdvanceDynamicPlans(ctx); err != nil {
		failures = append(failures, "advance plans: "+err.Error())
	}
	expired, err := s.External.ExpireIdleExternal(ctx, now.UTC())
	if err != nil {
		failures = append(failures, "expire external: "+err.Error())
	}
	emitted, err := s.Notifications.Dispatch()
	if err != nil {
		failures = append(failures, "dispatch notifications: "+err.Error())
	}
	result := &MaintenanceResult{ExpiredExternal: expired, Notifications: len(emitted)}
	if len(failures) > 0 {
		return result, fmt.Errorf("maintenance: %s", strings.Join(failures, "; "))
	}
	return result, nil
}
