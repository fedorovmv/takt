package bootstrap

import (
	"context"
	"os"

	"takt/internal/application"
	"takt/internal/domainadapter"
	"takt/internal/dynamicplan"
	"takt/internal/hostcontrol"
	"takt/internal/learning"
	"takt/internal/notification"
	"takt/internal/packagedist"
	"takt/internal/spec"
)

type advanceLock struct{ file *os.File }

func (l advanceLock) Release() error { return dynamicplan.ReleaseAdvanceLock(l.file) }

type planStore struct{ inner dynamicplan.Store }

func (s planStore) Load(id string) (*dynamicplan.Record, error) { return s.inner.Load(id) }
func (s planStore) Save(record *dynamicplan.Record) error       { return s.inner.Save(record) }
func (s planStore) List() ([]*dynamicplan.Record, error)        { return s.inner.List() }
func (s planStore) Dir(id string) string                        { return s.inner.Dir(id) }
func (s planStore) AcquireAdvanceLock(ctx context.Context) (application.AdvanceLock, error) {
	file, err := s.inner.AcquireAdvanceLock(ctx)
	if err != nil {
		return nil, err
	}
	return advanceLock{file: file}, nil
}
func (s planStore) TryAdvanceLock() (application.AdvanceLock, bool, error) {
	file, ok, err := s.inner.TryAdvanceLock()
	if err != nil || !ok {
		return nil, ok, err
	}
	return advanceLock{file: file}, true, nil
}

type packageBackend struct{ workspace string }

func (b packageBackend) Manager() (application.PackageManager, error) {
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

func applicationDependencies(workspace string) application.Dependencies {
	return application.Dependencies{
		PlanStore:      planStore{inner: dynamicplan.Store{Workspace: workspace}},
		HostStore:      hostcontrol.Store{Workspace: workspace},
		Notifications:  notification.Dispatcher{Workspace: workspace},
		Learning:       learning.Manager{Workspace: workspace},
		Packages:       packageBackend{workspace: workspace},
		AdapterFactory: adapterFactory,
	}
}
