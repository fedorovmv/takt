package application

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"takt/internal/command"
	"takt/internal/profile"
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

type PlanHooks struct {
	Attention          func() ([]AttentionItem, error)
	SetOwningRunStatus func(context.Context, string, string, string) error
}

type ProfilePreflight func(context.Context, *profile.Resolved, *spec.Config) error

type Dependencies struct {
	RunnerFactory    RunnerFactory
	RunStore         RunStore
	ProfilePreflight ProfilePreflight
	PlanHooks        PlanHooks
}

// Services is the immutable application graph exposed to transports. Services
// have named fields only: there is no embedded god-service compatibility layer.
type Services struct {
	Workspace  string
	ConfigPath string

	RunService       *RunService
	CatalogService   *CatalogService
	AuthoringService *AuthoringService
	WorktreeService  *WorktreeService
	CommandService   *CommandService
}

type RunService struct {
	workspace        string
	configPath       string
	runnerFactory    RunnerFactory
	store            RunStore
	planHooks        PlanHooks
	profilePreflight ProfilePreflight

	launchMu     sync.Mutex
	launchErrors map[string]error
}

type CatalogService struct{ workspace string }

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
	}

	run := &RunService{
		workspace: absWorkspace, configPath: absConfig, runnerFactory: deps.RunnerFactory,
		store:            deps.RunStore,
		planHooks:        deps.PlanHooks,
		profilePreflight: deps.ProfilePreflight,
		launchErrors:     map[string]error{},
	}
	catalog := &CatalogService{workspace: absWorkspace}
	authoringService := &AuthoringService{workspace: absWorkspace, runnerFactory: deps.RunnerFactory}
	worktrees := &WorktreeService{workspace: absWorkspace, store: deps.RunStore}
	commands := &CommandService{workspace: absWorkspace, configPath: absConfig, runnerFactory: deps.RunnerFactory, stateStore: deps.RunStore}

	return &Services{
		Workspace: absWorkspace, ConfigPath: absConfig,
		RunService: run, CatalogService: catalog, AuthoringService: authoringService,
		WorktreeService: worktrees, CommandService: commands,
	}, nil
}
