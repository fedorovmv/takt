package application

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"takt/internal/command"
	"takt/internal/domainadapter"
	"takt/internal/dynamicplan"
	"takt/internal/hostcontrol"
	"takt/internal/learning"
	"takt/internal/notification"
	"takt/internal/packagedist"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
)

type RunnerOptions struct {
	Commands *command.Resolver
}

type RunnerFactory func(runtime.Definition, RunnerOptions) *runtime.Runner

// RunStateStore is the persistence port used by run lifecycle operations.
type RunStateStore interface {
	Save(*store.RunState) error
	Commit(*store.RunState, store.Event) error
	Load(string) (*store.RunState, error)
}

// RunQueryStore is the read/query port used by run views and artifact lookup.
type RunQueryStore interface {
	Load(string) (*store.RunState, error)
	ListRunIDs() ([]string, error)
	ReadEvents(string, uint64, int) ([]store.Event, error)
	RunDir(string) string
	ArtifactsDir(string) string
}

// RunControlStore owns durable operator-control requests and the per-run lock.
type RunControlStore interface {
	AcquireLock(string) (func() error, error)
	RequestCancel(string) error
	ClearCancel(string) error
	RequestPause(string) error
	PauseRequested(string) (bool, error)
	ClearPause(string) error
	RequestAbandon(string, string) error
	AbandonRequested(string) (bool, string, error)
	ClearAbandon(string) error
}

// RunStore is the full Run lifecycle port. RunService needs all three facets;
// other services retain narrower consumer-owned views where their use cases allow it.
type RunStore interface {
	RunStateStore
	RunQueryStore
	RunControlStore
}

// WorktreeStore is the minimal persistence/control view used by worktree cleanup.
type WorktreeStore interface {
	RunStateStore
	AcquireLock(string) (func() error, error)
}

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

type NotificationBackend interface {
	List(bool, int) ([]notification.Item, error)
	Ack(string) (*notification.Item, error)
	Test(string) (*notification.Item, error)
	Dispatch() ([]notification.Item, error)
}

type LearningBackend interface {
	Scan(context.Context, int) ([]learning.Pattern, error)
	List() ([]*learning.Proposal, error)
	Load(string) (*learning.Proposal, error)
	Propose(context.Context, learning.ProposeRequest) (*learning.Proposal, error)
	Review(string, string, string) (*learning.Proposal, error)
	Evaluate(string, string) (*learning.Proposal, error)
	Stage(string) (*learning.Proposal, error)
}

type PackageManager interface {
	Install(context.Context, string, packagedist.InstallOptions) (*packagedist.LockedPackage, error)
	Update(context.Context, string, string, string) (*packagedist.LockedPackage, error)
	Uninstall(string, string) error
	List() ([]packagedist.LockedPackage, error)
	Sync(context.Context) (*packagedist.DoctorReport, error)
	Doctor() (*packagedist.DoctorReport, error)
}

type PackageBackend interface {
	Manager() (PackageManager, error)
	Sign(string, string, string) error
	InstalledManifestPaths() ([]string, error)
}

type AdapterFactory func(*spec.Config) domainadapter.Resolver

type Dependencies struct {
	RunnerFactory    RunnerFactory
	RunStore         RunStore
	PlanStore        PlanStore
	HostStore        HostStore
	Notifications    NotificationBackend
	Learning         LearningBackend
	Packages         PackageBackend
	AdapterFactory   AdapterFactory
	EvaluationEngine EvaluationEngine
}

// Services is the immutable application graph exposed to transports. Services
// have named fields only: there is no embedded god-service compatibility layer.
type Services struct {
	Workspace  string
	ConfigPath string

	RunService       *RunService
	ForkService      *ForkService
	CatalogService   *CatalogService
	AuthoringService *AuthoringService
	WorktreeService  *WorktreeService
	CommandService   *CommandService
	PlanService      *PlanService
	TaskService      *TaskService
	ExternalService  *ExternalService
	HostService      *HostService
	Notifications    *NotificationService
	Evaluation       *EvaluationService
	Learning         *LearningService
	Compatibility    *CompatibilityService
	Adapters         *AdapterService
	Packages         *PackageService
	Maintenance      *MaintenanceService
}

type RunService struct {
	workspace      string
	configPath     string
	runnerFactory  RunnerFactory
	store          RunStore
	planStore      PlanStore
	adapterFactory AdapterFactory

	launchMu     sync.Mutex
	launchErrors map[string]error
}

type ForkService struct {
	runs  *RunService
	plans *PlanService
}

type CatalogService struct{ workspace string }

type PlanService struct {
	workspace      string
	configPath     string
	runs           *RunService
	catalogs       *CatalogService
	store          PlanStore
	adapterFactory AdapterFactory
}

type TaskService struct {
	workspace  string
	configPath string
	runs       *RunService
	plans      *PlanService
	planStore  PlanStore
}

type ExternalService struct {
	workspace string
	runs      *RunService
	store     RunStore
}

type HostService struct {
	plans *PlanService
	store HostStore
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

	switch {
	case deps.RunnerFactory == nil:
		return nil, fmt.Errorf("application runner factory is required")
	case deps.RunStore == nil:
		return nil, fmt.Errorf("application run store is required")
	case deps.PlanStore == nil:
		return nil, fmt.Errorf("application plan store is required")
	case deps.HostStore == nil:
		return nil, fmt.Errorf("application host store is required")
	case deps.Notifications == nil:
		return nil, fmt.Errorf("application notification backend is required")
	case deps.Learning == nil:
		return nil, fmt.Errorf("application learning backend is required")
	case deps.Packages == nil:
		return nil, fmt.Errorf("application package backend is required")
	case deps.AdapterFactory == nil:
		return nil, fmt.Errorf("application adapter resolver is required")
	}

	run := &RunService{
		workspace: absWorkspace, configPath: absConfig, runnerFactory: deps.RunnerFactory,
		store:          deps.RunStore,
		planStore:      deps.PlanStore,
		adapterFactory: deps.AdapterFactory,
		launchErrors:   map[string]error{},
	}
	catalog := &CatalogService{workspace: absWorkspace}
	plan := &PlanService{workspace: absWorkspace, configPath: absConfig, runs: run, catalogs: catalog, store: deps.PlanStore, adapterFactory: deps.AdapterFactory}
	fork := &ForkService{runs: run, plans: plan}
	authoringService := &AuthoringService{workspace: absWorkspace, runnerFactory: deps.RunnerFactory}
	worktrees := &WorktreeService{workspace: absWorkspace, store: deps.RunStore}
	commands := &CommandService{workspace: absWorkspace, configPath: absConfig, runnerFactory: deps.RunnerFactory, stateStore: deps.RunStore}
	task := &TaskService{workspace: absWorkspace, configPath: absConfig, runs: run, plans: plan, planStore: deps.PlanStore}
	external := &ExternalService{workspace: absWorkspace, runs: run, store: deps.RunStore}
	host := &HostService{plans: plan, store: deps.HostStore}
	notifications := &NotificationService{backend: deps.Notifications}
	evaluationService := &EvaluationService{engine: deps.EvaluationEngine}
	learningService := &LearningService{backend: deps.Learning}
	compatibilityService := &CompatibilityService{workspace: absWorkspace, configPath: absConfig}
	adapterService := &AdapterService{configPath: absConfig, adapterFactory: deps.AdapterFactory}
	packageService := &PackageService{workspace: absWorkspace, configPath: absConfig, backend: deps.Packages, adapterFactory: deps.AdapterFactory}
	maintenance := &MaintenanceService{plans: plan, external: external, notifications: notifications}

	return &Services{
		Workspace: absWorkspace, ConfigPath: absConfig,
		RunService: run, ForkService: fork, CatalogService: catalog, AuthoringService: authoringService,
		WorktreeService: worktrees, CommandService: commands, PlanService: plan, TaskService: task,
		ExternalService: external, HostService: host, Notifications: notifications, Evaluation: evaluationService,
		Learning: learningService, Compatibility: compatibilityService, Adapters: adapterService,
		Packages: packageService, Maintenance: maintenance,
	}, nil
}
