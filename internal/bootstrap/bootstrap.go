package bootstrap

import (
	"context"
	"path/filepath"
	"time"

	"takt/internal/appapi"
	"takt/internal/application"
	"takt/internal/assistant"
	"takt/internal/catalogload"
	"takt/internal/domainadapter"
	"takt/internal/experimental/dynamicflow"
	"takt/internal/experimental/dynamicplan"
	"takt/internal/experimental/hostcontrol"
	experimentallearning "takt/internal/experimental/learning"
	"takt/internal/extensions"
	assistantproviders "takt/internal/extensions/assistants"
	"takt/internal/extensions/blockcatalog"
	"takt/internal/extensions/notification"
	"takt/internal/externalworker"
	"takt/internal/maintenance"
	"takt/internal/profile"
	"takt/internal/redact"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/tooling"
)

// App is the process composition root. Stable core, extensions, experimental
// features and developer tooling are wired here and remain separate elsewhere.
type App struct {
	Workspace    string
	ConfigPath   string
	Core         *application.Services
	External     *externalworker.Service
	Extensions   *extensions.Services
	Experimental *dynamicflow.Services
	Learning     *experimentallearning.Service
	Tooling      *tooling.Services
	Maintenance  *maintenance.Service
	API          *appapi.Registry
}

func New(workspace, configPath string) (*App, error) {
	return newApp(workspace, configPath, nil, 0)
}

func newApp(workspace, configPath string, assistantEvents func(string, string, assistant.Event), assistantIdleTimeout time.Duration) (*App, error) {
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}

	providers := assistant.MustRegistry(assistantproviders.Registrations()...)
	var dynamic *dynamicflow.Services
	coreDeps := application.Dependencies{
		RunStore: store.FS{Workspace: absWorkspace},
		ProfilePreflight: func(ctx context.Context, resolved *profile.Resolved, cfg *spec.Config) error {
			catalog, err := catalogload.FromResolved(resolved, workspace)
			if err != nil {
				return err
			}
			_, err = blockcatalog.PreflightAdapters(ctx, catalog, cfg, adapterFactory(cfg))
			return err
		},
		PlanHooks: application.PlanHooks{
			Attention: func() ([]application.AttentionItem, error) {
				if dynamic == nil || dynamic.PlanService == nil {
					return nil, nil
				}
				return dynamic.PlanService.AttentionItems()
			},
			SetOwningRunStatus: func(ctx context.Context, runID, status, lastError string) error {
				if dynamic == nil || dynamic.PlanService == nil {
					return nil
				}
				return dynamic.PlanService.SetOwningRunStatus(ctx, runID, status, lastError)
			},
		},
	}
	coreDeps.RunnerFactory = func(def runtime.Definition, options application.RunnerOptions) *runtime.Runner {
		deps := runtime.Dependencies{
			Commands:             runtime.NewCommandResolver(def.WorkflowPath, def.ControlWorkspace, def.ControlWorkspace),
			Store:                store.FS{Workspace: def.ControlWorkspace},
			Assistants:           assistant.Factory{Config: def.Config, Providers: providers},
			Adapters:             domainadapter.Factory{Config: def.Config},
			Redactor:             redact.NewFromConfig(def.Config),
			AssistantEvents:      assistantEvents,
			AssistantIdleTimeout: assistantIdleTimeout,
		}
		if options.Commands != nil {
			deps.Commands = *options.Commands
		}
		return runtime.NewWithDependencies(def, deps)
	}
	core, err := application.NewWithDependencies(absWorkspace, configPath, coreDeps)
	if err != nil {
		return nil, err
	}
	external := externalworker.New(core.Workspace, core.RunService, store.FS{Workspace: core.Workspace})

	ext := &extensions.Services{
		Adapters:      extensions.NewAdapter(core.ConfigPath, adapterFactory),
		Packages:      extensions.NewPackage(core.Workspace, core.ConfigPath, packageBackend{workspace: core.Workspace}, adapterFactory),
		Notifications: extensions.NewNotification(notification.Dispatcher{Workspace: core.Workspace}),
		Blocks:        extensions.NewBlocks(core.Workspace),
	}
	dynamic = dynamicflow.New(dynamicflow.Dependencies{
		Workspace: core.Workspace, ConfigPath: core.ConfigPath,
		Runs: core.RunService, Catalogs: core.CatalogService,
		PlanStore:      planStore{inner: dynamicplan.Store{Workspace: core.Workspace}},
		HostStore:      hostcontrol.Store{Workspace: core.Workspace},
		AdapterFactory: adapterFactory,
	})
	learn := experimentallearning.NewService(experimentallearning.Manager{Workspace: core.Workspace})
	tools := &tooling.Services{
		Evaluation:    tooling.NewEvaluation(evaluationEngine{providers: providers}),
		Compatibility: tooling.NewCompatibility(core.Workspace, core.ConfigPath, providers),
	}
	maint := maintenance.New(dynamic.PlanService, external, func() (int, error) {
		items, err := ext.Notifications.Dispatch()
		return len(items), err
	})
	api := appapi.New(appapi.Dependencies{Core: core, Dynamic: dynamic, Blocks: ext.Blocks, Notifications: ext.Notifications})
	return &App{Workspace: core.Workspace, ConfigPath: core.ConfigPath, Core: core, External: external, Extensions: ext, Experimental: dynamic, Learning: learn, Tooling: tools, Maintenance: maint, API: api}, nil
}
