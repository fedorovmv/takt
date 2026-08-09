package dynamicflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"takt/internal/application"
	"takt/internal/catalogload"
	"takt/internal/domainadapter"
	"takt/internal/experimental/dynamicplan"
	"takt/internal/experimental/hostcontrol"
	"takt/internal/extensions/blockcatalog"
	"takt/internal/profile"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/testsupport/runtimefixture"
)

type testAdvanceLock struct{ file *os.File }

func (l testAdvanceLock) Release() error { return dynamicplan.ReleaseAdvanceLock(l.file) }

type testPlanStore struct{ inner dynamicplan.Store }

func (s testPlanStore) Load(id string) (*dynamicplan.Record, error) { return s.inner.Load(id) }
func (s testPlanStore) Save(r *dynamicplan.Record) error            { return s.inner.Save(r) }
func (s testPlanStore) List() ([]*dynamicplan.Record, error)        { return s.inner.List() }
func (s testPlanStore) Dir(id string) string                        { return s.inner.Dir(id) }
func (s testPlanStore) AcquireAdvanceLock(ctx context.Context) (AdvanceLock, error) {
	f, e := s.inner.AcquireAdvanceLock(ctx)
	if e != nil {
		return nil, e
	}
	return testAdvanceLock{f}, nil
}
func (s testPlanStore) TryAdvanceLock() (AdvanceLock, bool, error) {
	f, ok, e := s.inner.TryAdvanceLock()
	if e != nil || !ok {
		return nil, ok, e
	}
	return testAdvanceLock{f}, true, nil
}

func testAdapterFactory(cfg *spec.Config) domainadapter.Resolver {
	return domainadapter.Factory{Config: cfg}
}

func newTestServices(workspace, configPath string) (*Services, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	var dynamic *Services
	core, err := application.NewWithDependencies(abs, configPath, application.Dependencies{
		RunStore: store.FS{Workspace: abs},
		ProfilePreflight: func(ctx context.Context, resolved *profile.Resolved, cfg *spec.Config) error {
			catalog, err := catalogload.FromResolved(resolved, workspace)
			if err != nil {
				return err
			}
			_, err = blockcatalog.PreflightAdapters(ctx, catalog, cfg, testAdapterFactory(cfg))
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
	dynamic = New(Dependencies{Workspace: core.Workspace, ConfigPath: core.ConfigPath, Runs: core.RunService, Catalogs: core.CatalogService, PlanStore: testPlanStore{inner: dynamicplan.Store{Workspace: abs}}, HostStore: hostcontrol.Store{Workspace: abs}, AdapterFactory: testAdapterFactory})
	return dynamic, nil
}

func catalogForResolved(resolved *profile.Resolved, workspace string) (*blockcatalog.Catalog, error) {
	return catalogload.FromResolved(resolved, workspace)
}

func writeControlFile(t testing.TB, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}
