package appfixture

import (
	"context"
	"os"
	"path/filepath"

	"takt/internal/application"
	"takt/internal/catalogload"
	"takt/internal/domainadapter"
	"takt/internal/experimental/dynamicflow"
	"takt/internal/experimental/dynamicplan"
	"takt/internal/experimental/hostcontrol"
	"takt/internal/extensions"
	"takt/internal/extensions/blockcatalog"
	"takt/internal/extensions/notification"
	"takt/internal/extensions/packagedist"
	"takt/internal/externalworker"
	"takt/internal/maintenance"
	"takt/internal/profile"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/testsupport/runtimefixture"
)

type advanceLock struct{ file *os.File }

func (l advanceLock) Release() error { return dynamicplan.ReleaseAdvanceLock(l.file) }

type planStore struct{ inner dynamicplan.Store }

func (s planStore) Load(id string) (*dynamicplan.Record, error) { return s.inner.Load(id) }
func (s planStore) Save(r *dynamicplan.Record) error            { return s.inner.Save(r) }
func (s planStore) List() ([]*dynamicplan.Record, error)        { return s.inner.List() }
func (s planStore) Dir(id string) string                        { return s.inner.Dir(id) }
func (s planStore) AcquireAdvanceLock(ctx context.Context) (dynamicflow.AdvanceLock, error) {
	f, e := s.inner.AcquireAdvanceLock(ctx)
	if e != nil {
		return nil, e
	}
	return advanceLock{f}, nil
}
func (s planStore) TryAdvanceLock() (dynamicflow.AdvanceLock, bool, error) {
	f, ok, e := s.inner.TryAdvanceLock()
	if e != nil || !ok {
		return nil, ok, e
	}
	return advanceLock{f}, true, nil
}

type packageBackend struct{ workspace string }

func (b packageBackend) Manager() (extensions.PackageManager, error) {
	return packagedist.New(b.workspace)
}
func (b packageBackend) Sign(path, keyID, keyFile string) error {
	return packagedist.SignPackage(path, keyID, keyFile)
}
func (b packageBackend) InstalledManifestPaths() ([]string, error) {
	return packagedist.InstalledManifestPaths(b.workspace)
}
func adapterFactory(cfg *spec.Config) domainadapter.Resolver {
	return domainadapter.Factory{Config: cfg}
}

type Fixture struct {
	Core        *application.Services
	External    *externalworker.Service
	Dynamic     *dynamicflow.Services
	Extensions  *extensions.Services
	Maintenance *maintenance.Service
}

func New(workspace, configPath string) (*Fixture, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	var dynamic *dynamicflow.Services
	core, err := application.NewWithDependencies(abs, configPath, application.Dependencies{
		RunStore: store.FS{Workspace: abs},
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
				if dynamic == nil {
					return nil, nil
				}
				return dynamic.PlanService.AttentionItems()
			},
			SetOwningRunStatus: func(ctx context.Context, runID, status, lastError string) error {
				if dynamic == nil {
					return nil
				}
				return dynamic.PlanService.SetOwningRunStatus(ctx, runID, status, lastError)
			},
		},
		RunnerFactory: func(def runtime.Definition, options application.RunnerOptions) *runtime.Runner {
			return runtimefixture.Runner(def, options.Commands)
		},
	})
	if err != nil {
		return nil, err
	}
	dynamic = dynamicflow.New(dynamicflow.Dependencies{
		Workspace: core.Workspace, ConfigPath: core.ConfigPath, Runs: core.RunService, Catalogs: core.CatalogService,
		PlanStore: planStore{inner: dynamicplan.Store{Workspace: abs}}, HostStore: hostcontrol.Store{Workspace: abs}, AdapterFactory: adapterFactory,
	})
	ext := &extensions.Services{
		Adapters:      extensions.NewAdapter(core.ConfigPath, adapterFactory),
		Packages:      extensions.NewPackage(core.Workspace, core.ConfigPath, packageBackend{abs}, adapterFactory),
		Notifications: extensions.NewNotification(notification.Dispatcher{Workspace: abs}), Blocks: extensions.NewBlocks(abs),
	}
	external := externalworker.New(core.Workspace, core.RunService, store.FS{Workspace: core.Workspace})
	maint := maintenance.New(dynamic.PlanService, external, func() (int, error) { items, err := ext.Notifications.Dispatch(); return len(items), err })
	return &Fixture{Core: core, External: external, Dynamic: dynamic, Extensions: ext, Maintenance: maint}, nil
}
