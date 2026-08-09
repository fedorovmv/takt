package dynamicflow

import (
	"context"
	"strings"

	"takt/internal/application"
	"takt/internal/domainadapter"
	"takt/internal/experimental/dynamicplan"
	"takt/internal/experimental/hostcontrol"
	"takt/internal/spec"
)

type AdapterFactory func(*spec.Config) domainadapter.Resolver

type AdvanceLock interface{ Release() error }

type PlanStore interface {
	Load(string) (*dynamicplan.Record, error)
	Save(*dynamicplan.Record) error
	List() ([]*dynamicplan.Record, error)
	Dir(string) string
	AcquireAdvanceLock(context.Context) (AdvanceLock, error)
	TryAdvanceLock() (AdvanceLock, bool, error)
}

type HostStore interface {
	AcquireLock() (func() error, error)
	Save(*hostcontrol.Session) error
	Load(string) (*hostcontrol.Session, error)
	Find(string, string) (*hostcontrol.Session, error)
}

type PlanService struct {
	workspace      string
	configPath     string
	runs           *application.RunService
	catalogs       *application.CatalogService
	store          PlanStore
	adapterFactory AdapterFactory
}

type TaskService struct {
	workspace  string
	configPath string
	runs       *application.RunService
	plans      *PlanService
	planStore  PlanStore
}

type HostService struct {
	plans *PlanService
	store HostStore
}

type Services struct {
	Workspace   string
	ConfigPath  string
	RunService  *application.RunService
	PlanService *PlanService
	TaskService *TaskService
	HostService *HostService
	ForkService *ForkService
}

type Dependencies struct {
	Workspace      string
	ConfigPath     string
	Runs           *application.RunService
	Catalogs       *application.CatalogService
	PlanStore      PlanStore
	HostStore      HostStore
	AdapterFactory AdapterFactory
}

func New(deps Dependencies) *Services {
	plan := &PlanService{workspace: deps.Workspace, configPath: deps.ConfigPath, runs: deps.Runs, catalogs: deps.Catalogs, store: deps.PlanStore, adapterFactory: deps.AdapterFactory}
	task := &TaskService{workspace: deps.Workspace, configPath: deps.ConfigPath, runs: deps.Runs, plans: plan, planStore: deps.PlanStore}
	host := &HostService{plans: plan, store: deps.HostStore}
	return &Services{Workspace: deps.Workspace, ConfigPath: deps.ConfigPath, RunService: deps.Runs, PlanService: plan, TaskService: task, HostService: host, ForkService: &ForkService{runs: deps.Runs, plans: plan}}
}

// AttentionItems implements the optional stable RunService plan hook without
// leaking dynamic-plan persistence types into the stable application package.
func (s *PlanService) AttentionItems() ([]application.AttentionItem, error) {
	records, err := s.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]application.AttentionItem, 0)
	for _, plan := range records {
		if plan.CurrentRunID != "" || (plan.Status != "waiting" && plan.Status != "parked") {
			continue
		}
		reason := "question"
		if plan.Status == "parked" {
			reason = "owner_decision_required"
			if plan.Failure != nil && plan.Failure.Code != "" {
				reason = strings.ToLower(strings.TrimSpace(plan.Failure.Code))
			}
		}
		out = append(out, application.AttentionItem{Kind: "plan", PlanID: plan.ID, Status: plan.Status, Reason: reason, Message: plan.LastError, UpdatedAt: plan.UpdatedAt})
	}
	return out, nil
}

func (s *PlanService) SetOwningRunStatus(ctx context.Context, runID, status, lastError string) error {
	lock, err := s.store.AcquireAdvanceLock(ctx)
	if err != nil {
		return err
	}
	defer lock.Release()
	records, err := s.store.List()
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.CurrentRunID != runID && !containsString(record.ExecutionRunIDs, runID) {
			continue
		}
		record.Status = status
		record.LastError = lastError
		return s.savePlanRecord(record)
	}
	return nil
}
