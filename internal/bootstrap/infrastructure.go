package bootstrap

import (
	"context"
	"os"

	"takt/internal/domainadapter"
	"takt/internal/experimental/dynamicflow"
	"takt/internal/experimental/dynamicplan"
	"takt/internal/extensions"
	"takt/internal/extensions/packagedist"
	"takt/internal/spec"
)

type advanceLock struct{ file *os.File }

func (l advanceLock) Release() error { return dynamicplan.ReleaseAdvanceLock(l.file) }

type planStore struct{ inner dynamicplan.Store }

func (s planStore) Load(id string) (*dynamicplan.Record, error) { return s.inner.Load(id) }
func (s planStore) Save(record *dynamicplan.Record) error       { return s.inner.Save(record) }
func (s planStore) List() ([]*dynamicplan.Record, error)        { return s.inner.List() }
func (s planStore) Dir(id string) string                        { return s.inner.Dir(id) }
func (s planStore) AcquireAdvanceLock(ctx context.Context) (dynamicflow.AdvanceLock, error) {
	f, err := s.inner.AcquireAdvanceLock(ctx)
	if err != nil {
		return nil, err
	}
	return advanceLock{f}, nil
}
func (s planStore) TryAdvanceLock() (dynamicflow.AdvanceLock, bool, error) {
	f, ok, err := s.inner.TryAdvanceLock()
	if err != nil || !ok {
		return nil, ok, err
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
