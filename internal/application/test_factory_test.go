package application

import (
	"context"
	"os"
	"path/filepath"

	"takt/internal/domainadapter"
	"takt/internal/dynamicplan"
	"takt/internal/hostcontrol"
	"takt/internal/learning"
	"takt/internal/notification"
	"takt/internal/packagedist"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/testsupport/runtimefixture"
)

type testAdvanceLock struct{ file *os.File }

func (l testAdvanceLock) Release() error { return dynamicplan.ReleaseAdvanceLock(l.file) }

type testPlanStore struct{ inner dynamicplan.Store }

func (s testPlanStore) Load(id string) (*dynamicplan.Record, error) { return s.inner.Load(id) }
func (s testPlanStore) Save(record *dynamicplan.Record) error       { return s.inner.Save(record) }
func (s testPlanStore) List() ([]*dynamicplan.Record, error)        { return s.inner.List() }
func (s testPlanStore) Dir(id string) string                        { return s.inner.Dir(id) }
func (s testPlanStore) AcquireAdvanceLock(ctx context.Context) (AdvanceLock, error) {
	f, err := s.inner.AcquireAdvanceLock(ctx)
	if err != nil {
		return nil, err
	}
	return testAdvanceLock{f}, nil
}
func (s testPlanStore) TryAdvanceLock() (AdvanceLock, bool, error) {
	f, ok, err := s.inner.TryAdvanceLock()
	if err != nil || !ok {
		return nil, ok, err
	}
	return testAdvanceLock{f}, true, nil
}

type testPackageBackend struct{ workspace string }

func (b testPackageBackend) Manager() (PackageManager, error) { return packagedist.New(b.workspace) }
func (b testPackageBackend) Sign(path, keyID, keyFile string) error {
	return packagedist.SignPackage(path, keyID, keyFile)
}
func (b testPackageBackend) InstalledManifestPaths() ([]string, error) {
	return packagedist.InstalledManifestPaths(b.workspace)
}

func testAdapterFactory(cfg *spec.Config) domainadapter.Resolver {
	return domainadapter.Factory{Config: cfg}
}

func New(workspace, configPath string) (*Services, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	return NewWithDependencies(abs, configPath, Dependencies{
		RunStore:       store.FS{Workspace: abs},
		PlanStore:      testPlanStore{inner: dynamicplan.Store{Workspace: abs}},
		HostStore:      hostcontrol.Store{Workspace: abs},
		Notifications:  notification.Dispatcher{Workspace: abs},
		Learning:       learning.Manager{Workspace: abs},
		Packages:       testPackageBackend{workspace: abs},
		AdapterFactory: testAdapterFactory,
		RunnerFactory: func(def runtime.Definition, options RunnerOptions) *runtime.Runner {
			return runtimefixture.Runner(def, options.Commands)
		},
	})
}
