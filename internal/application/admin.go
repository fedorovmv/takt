package application

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"takt/internal/blockcatalog"
	"takt/internal/compatibility"
	cfgpkg "takt/internal/config"
	"takt/internal/domainadapter"
	"takt/internal/packagedist"
	"takt/internal/spec"
)

type CompatibilityMatrix = compatibility.Matrix
type CompatibilityFieldMatrix = compatibility.FieldMatrix
type CompatibilityReport = compatibility.CheckReport

type CompatibilityService struct{ *Context }

func (s *CompatibilityService) Matrix() CompatibilityMatrix { return compatibility.CurrentMatrix() }
func (s *CompatibilityService) Fields() CompatibilityFieldMatrix {
	return compatibility.CurrentFieldMatrix()
}
func (s *CompatibilityService) Check(ctx context.Context, live bool) (CompatibilityReport, error) {
	cfg, err := cfgpkg.Load(s.ConfigPath)
	if err != nil {
		return CompatibilityReport{}, err
	}
	return compatibility.Check(ctx, cfg, compatibility.CheckOptions{Workspace: s.Workspace, Live: live}), nil
}

type AdapterSummary struct {
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	Transport string `json:"transport"`
}

type AdapterDoctorReport struct {
	Name                  string                    `json:"name"`
	Status                string                    `json:"status"`
	Transport             string                    `json:"transport"`
	Declaration           domainadapter.Declaration `json:"declaration"`
	MissingCoreOperations []string                  `json:"missing_core_operations"`
	Problems              []string                  `json:"problems"`
}

type AdapterService struct{ *Context }

func (s *AdapterService) config() (*spec.Config, error) { return cfgpkg.Load(s.ConfigPath) }

func (s *AdapterService) List() ([]AdapterSummary, error) {
	cfg, err := s.config()
	if err != nil {
		return nil, err
	}
	rows := make([]AdapterSummary, 0, len(cfg.Adapters))
	for name, value := range cfg.Adapters {
		rows = append(rows, AdapterSummary{Name: name, Domain: value.Domain, Transport: value.Transport})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

func (s *AdapterService) Describe(ctx context.Context, name string) (domainadapter.Declaration, error) {
	cfg, err := s.config()
	if err != nil {
		return domainadapter.Declaration{}, err
	}
	adapter, err := (domainadapter.Factory{Config: cfg}).Resolve(name)
	if err != nil {
		return domainadapter.Declaration{}, err
	}
	declaration, err := adapter.Describe(ctx)
	if err != nil {
		return domainadapter.Declaration{}, err
	}
	if err := domainadapter.ValidateDeclaration(declaration); err != nil {
		return domainadapter.Declaration{}, err
	}
	return declaration, nil
}

func (s *AdapterService) Doctor(ctx context.Context, name string) (AdapterDoctorReport, error) {
	cfg, err := s.config()
	if err != nil {
		return AdapterDoctorReport{}, err
	}
	declaration, err := s.Describe(ctx, name)
	if err != nil {
		return AdapterDoctorReport{}, err
	}
	specValue, ok := cfg.Adapters[name]
	if !ok {
		return AdapterDoctorReport{}, fmt.Errorf("adapter %q is not configured", name)
	}
	capSet, recSet := map[string]bool{}, map[string]bool{}
	for _, op := range declaration.Capabilities {
		capSet[op] = true
	}
	for _, op := range declaration.Reconcile {
		recSet[op] = true
	}
	var problems, missingCore []string
	for op := range specValue.Operations {
		if !capSet[op] {
			problems = append(problems, "configured operation not declared: "+op)
		}
	}
	for op := range specValue.ReconcileOperations {
		if !recSet[op] {
			problems = append(problems, "configured reconcile operation not declared: "+op)
		}
	}
	for _, op := range domainadapter.CoreOperations(specValue.Domain) {
		if !capSet[op] {
			missingCore = append(missingCore, op)
		}
	}
	sort.Strings(problems)
	sort.Strings(missingCore)
	status := "ready"
	if len(problems) > 0 {
		status = "error"
	}
	return AdapterDoctorReport{Name: name, Status: status, Transport: specValue.Transport, Declaration: declaration, MissingCoreOperations: missingCore, Problems: problems}, nil
}

type PackageInstallOptions = packagedist.InstallOptions
type LockedPackage = packagedist.LockedPackage
type PackageDoctorReport = packagedist.DoctorReport

type PackageDoctorResult struct {
	Status           string                   `json:"status"`
	Packages         []packagedist.DoctorItem `json:"packages"`
	AdapterPreflight []AdapterPreflightStatus `json:"adapter_preflight"`
}

type PackageService struct{ *Context }

func (s *PackageService) manager() (*packagedist.Manager, error) { return packagedist.New(s.Workspace) }
func (s *PackageService) Install(ctx context.Context, source string, opts PackageInstallOptions) (*LockedPackage, error) {
	m, err := s.manager()
	if err != nil {
		return nil, err
	}
	return m.Install(ctx, source, opts)
}
func (s *PackageService) Update(ctx context.Context, name, scope, ref string) (*LockedPackage, error) {
	m, err := s.manager()
	if err != nil {
		return nil, err
	}
	return m.Update(ctx, name, scope, ref)
}
func (s *PackageService) Uninstall(name, scope string) error {
	m, err := s.manager()
	if err != nil {
		return err
	}
	return m.Uninstall(name, scope)
}
func (s *PackageService) List() ([]LockedPackage, error) {
	m, err := s.manager()
	if err != nil {
		return nil, err
	}
	return m.List()
}
func (s *PackageService) Sync(ctx context.Context) (*PackageDoctorReport, error) {
	m, err := s.manager()
	if err != nil {
		return nil, err
	}
	return m.Sync(ctx)
}
func (s *PackageService) Sign(path, keyID, keyFile string) error {
	return packagedist.SignPackage(path, keyID, keyFile)
}
func (s *PackageService) Doctor(ctx context.Context) (PackageDoctorResult, error) {
	m, err := s.manager()
	if err != nil {
		return PackageDoctorResult{}, err
	}
	report, err := m.Doctor()
	if err != nil {
		return PackageDoctorResult{}, err
	}
	result := PackageDoctorResult{Status: report.Status, Packages: report.Packages}
	if report.Status != "ready" {
		return result, fmt.Errorf("package doctor found problems: %+v", report.Packages)
	}
	paths, err := packagedist.InstalledManifestPaths(s.Workspace)
	if err != nil {
		return result, err
	}
	if len(paths) == 0 {
		return result, nil
	}
	catalog, err := blockcatalog.Load(paths)
	if err != nil {
		return result, err
	}
	if len(catalog.Requirements.Adapters) == 0 {
		return result, nil
	}
	var cfg *spec.Config
	if _, statErr := os.Stat(s.ConfigPath); os.IsNotExist(statErr) {
		cfg = &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config", Adapters: map[string]spec.DomainAdapterSpec{}}
	} else {
		cfg, err = cfgpkg.Load(s.ConfigPath)
		if err != nil {
			return result, fmt.Errorf("package doctor adapter config: %w", err)
		}
	}
	result.AdapterPreflight, err = PreflightCatalogAdapters(ctx, catalog, cfg)
	if err != nil {
		return result, fmt.Errorf("package doctor adapter preflight: %w", err)
	}
	return result, nil
}

func (s *CatalogService) ValidateBlockPackage(path string) (*blockcatalog.Catalog, error) {
	return blockcatalog.LoadOne(strings.TrimSpace(path))
}
