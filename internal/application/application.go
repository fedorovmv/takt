package application

import (
	"fmt"
	"path/filepath"
	"sync"

	"takt/internal/command"
	"takt/internal/runtime"
	"takt/internal/store"
)

// Context contains the small set of process-local dependencies shared by
// application services. It is an implementation detail, not a service locator.
type Context struct {
	Workspace  string
	ConfigPath string

	mu            sync.Mutex
	dynamicMu     sync.Mutex
	hostMu        sync.Mutex
	launchErrors  map[string]error
	runnerFactory RunnerFactory
	runStore      RunStore
}

type RunnerOptions struct {
	Commands *command.Resolver
}

type RunnerFactory func(runtime.Definition, RunnerOptions) *runtime.Runner

type RunStore interface {
	store.Repository
	ListRunIDs() ([]string, error)
	ReadEvents(id string, afterRevision uint64, limit int) ([]store.Event, error)
	AcquireLock(id string) (func() error, error)
	RequestCancel(id string) error
	ClearCancel(id string) error
	RequestPause(id string) error
	PauseRequested(id string) (bool, error)
	ClearPause(id string) error
	RequestAbandon(id, reason string) error
	AbandonRequested(id string) (bool, string, error)
	ClearAbandon(id string) error
}

type Dependencies struct {
	RunnerFactory    RunnerFactory
	RunStore         RunStore
	EvaluationEngine EvaluationEngine
}

// Services is the composition boundary for Takt use cases. The embedded
// services keep source compatibility inside the repository while transports
// should depend on the narrow service they actually use.
type Services struct {
	Workspace  string
	ConfigPath string
	*RunService
	*CatalogService
	*AuthoringService
	*WorktreeService
	*CommandService
	*PlanService
	*TaskService
	*ExternalService
	*HostService
	Notifications *NotificationService
	Evaluation    *EvaluationService
	Learning      *LearningService
	Compatibility *CompatibilityService
	Adapters      *AdapterService
	Packages      *PackageService
	Maintenance   *MaintenanceService
}

type RunService struct {
	*Context
	Plans *PlanService
}

type CatalogService struct{ *Context }

type PlanService struct {
	*Context
	Runs     *RunService
	Catalogs *CatalogService
}

type TaskService struct {
	*Context
	Runs  *RunService
	Plans *PlanService
}

type ExternalService struct {
	*Context
	Runs *RunService
}

type HostService struct {
	*Context
	Plans *PlanService
}

func NewWithDependencies(workspace, configPath string, deps Dependencies) (*Services, error) {
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	absConfig := configPath
	if absConfig == "" {
		absConfig = ".takt/config.yaml"
	}
	if !filepath.IsAbs(absConfig) {
		absConfig = filepath.Join(absWorkspace, absConfig)
	}
	absConfig, err = filepath.Abs(absConfig)
	if err != nil {
		return nil, err
	}

	if deps.RunnerFactory == nil {
		return nil, fmt.Errorf("application runner factory is required")
	}
	if deps.RunStore == nil {
		return nil, fmt.Errorf("application run store is required")
	}
	shared := &Context{Workspace: absWorkspace, ConfigPath: absConfig, launchErrors: map[string]error{}, runnerFactory: deps.RunnerFactory, runStore: deps.RunStore}
	run := &RunService{Context: shared}
	catalog := &CatalogService{Context: shared}
	authoringService := &AuthoringService{Context: shared}
	worktrees := &WorktreeService{Context: shared}
	commands := &CommandService{Context: shared}
	plan := &PlanService{Context: shared, Runs: run, Catalogs: catalog}
	task := &TaskService{Context: shared, Runs: run, Plans: plan}
	external := &ExternalService{Context: shared, Runs: run}
	host := &HostService{Context: shared, Plans: plan}
	notifications := &NotificationService{Workspace: absWorkspace}
	evaluationService := &EvaluationService{engine: deps.EvaluationEngine}
	learningService := &LearningService{Context: shared}
	compatibilityService := &CompatibilityService{Context: shared}
	adapterService := &AdapterService{Context: shared}
	packageService := &PackageService{Context: shared}
	maintenance := &MaintenanceService{Plans: plan, External: external, Notifications: notifications}
	run.Plans = plan

	return &Services{
		Workspace: absWorkspace, ConfigPath: absConfig,
		RunService: run, CatalogService: catalog, AuthoringService: authoringService, WorktreeService: worktrees, CommandService: commands, PlanService: plan,
		TaskService: task, ExternalService: external, HostService: host,
		Notifications: notifications, Evaluation: evaluationService, Learning: learningService, Compatibility: compatibilityService, Adapters: adapterService, Packages: packageService, Maintenance: maintenance,
	}, nil
}
