package extensions

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"takt/internal/catalogload"

	cfgpkg "takt/internal/config"
	"takt/internal/domainadapter"
	"takt/internal/extensions/blockcatalog"
	"takt/internal/extensions/notification"
	"takt/internal/extensions/packagedist"
	"takt/internal/profile"
	"takt/internal/spec"
)

type AdapterFactory func(*spec.Config) domainadapter.Resolver

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
type AdapterService struct {
	configPath     string
	adapterFactory AdapterFactory
}

func NewAdapter(configPath string, factory AdapterFactory) *AdapterService {
	return &AdapterService{configPath: configPath, adapterFactory: factory}
}
func (s *AdapterService) config() (*spec.Config, error) { return cfgpkg.Load(s.configPath) }
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
	adapter, err := s.adapterFactory(cfg).Resolve(name)
	if err != nil {
		return domainadapter.Declaration{}, err
	}
	d, err := adapter.Describe(ctx)
	if err != nil {
		return domainadapter.Declaration{}, err
	}
	if err := domainadapter.ValidateDeclaration(d); err != nil {
		return domainadapter.Declaration{}, err
	}
	return d, nil
}
func (s *AdapterService) Doctor(ctx context.Context, name string) (AdapterDoctorReport, error) {
	cfg, err := s.config()
	if err != nil {
		return AdapterDoctorReport{}, err
	}
	d, err := s.Describe(ctx, name)
	if err != nil {
		return AdapterDoctorReport{}, err
	}
	sv, ok := cfg.Adapters[name]
	if !ok {
		return AdapterDoctorReport{}, fmt.Errorf("adapter %q is not configured", name)
	}
	caps, recs := map[string]bool{}, map[string]bool{}
	for _, op := range d.Capabilities {
		caps[op] = true
	}
	for _, op := range d.Reconcile {
		recs[op] = true
	}
	var problems, missing []string
	for op := range sv.Operations {
		if !caps[op] {
			problems = append(problems, "configured operation not declared: "+op)
		}
	}
	for op := range sv.ReconcileOperations {
		if !recs[op] {
			problems = append(problems, "configured reconcile operation not declared: "+op)
		}
	}
	for _, op := range domainadapter.CoreOperations(sv.Domain) {
		if !caps[op] {
			missing = append(missing, op)
		}
	}
	sort.Strings(problems)
	sort.Strings(missing)
	status := "ready"
	if len(problems) > 0 {
		status = "error"
	}
	return AdapterDoctorReport{Name: name, Status: status, Transport: sv.Transport, Declaration: d, MissingCoreOperations: missing, Problems: problems}, nil
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
type PackageInstallOptions = packagedist.InstallOptions
type LockedPackage = packagedist.LockedPackage
type PackageDoctorReport = packagedist.DoctorReport
type PackageDoctorResult struct {
	Status           string                         `json:"status"`
	Packages         []packagedist.DoctorItem       `json:"packages"`
	AdapterPreflight []blockcatalog.PreflightStatus `json:"adapter_preflight"`
}
type PackageService struct {
	workspace, configPath string
	backend               PackageBackend
	adapterFactory        AdapterFactory
}

func NewPackage(workspace, configPath string, backend PackageBackend, factory AdapterFactory) *PackageService {
	return &PackageService{workspace: workspace, configPath: configPath, backend: backend, adapterFactory: factory}
}
func (s *PackageService) manager() (PackageManager, error) { return s.backend.Manager() }
func (s *PackageService) Install(ctx context.Context, source string, opts PackageInstallOptions) (*LockedPackage, error) {
	m, e := s.manager()
	if e != nil {
		return nil, e
	}
	return m.Install(ctx, source, opts)
}
func (s *PackageService) Update(ctx context.Context, name, scope, ref string) (*LockedPackage, error) {
	m, e := s.manager()
	if e != nil {
		return nil, e
	}
	return m.Update(ctx, name, scope, ref)
}
func (s *PackageService) Uninstall(name, scope string) error {
	m, e := s.manager()
	if e != nil {
		return e
	}
	return m.Uninstall(name, scope)
}
func (s *PackageService) List() ([]LockedPackage, error) {
	m, e := s.manager()
	if e != nil {
		return nil, e
	}
	return m.List()
}
func (s *PackageService) Sync(ctx context.Context) (*PackageDoctorReport, error) {
	m, e := s.manager()
	if e != nil {
		return nil, e
	}
	return m.Sync(ctx)
}
func (s *PackageService) Sign(path, keyID, keyFile string) error {
	return s.backend.Sign(path, keyID, keyFile)
}
func (s *PackageService) Doctor(ctx context.Context) (PackageDoctorResult, error) {
	m, e := s.manager()
	if e != nil {
		return PackageDoctorResult{}, e
	}
	report, e := m.Doctor()
	if e != nil {
		return PackageDoctorResult{}, e
	}
	result := PackageDoctorResult{Status: report.Status, Packages: report.Packages}
	if report.Status != "ready" {
		return result, fmt.Errorf("package doctor found problems: %+v", report.Packages)
	}
	paths, e := s.backend.InstalledManifestPaths()
	if e != nil {
		return result, e
	}
	if len(paths) == 0 {
		return result, nil
	}
	catalog, e := blockcatalog.Load(paths)
	if e != nil {
		return result, e
	}
	if len(catalog.Requirements.Adapters) == 0 {
		return result, nil
	}
	var cfg *spec.Config
	if _, se := os.Stat(s.configPath); os.IsNotExist(se) {
		cfg = &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config", Adapters: map[string]spec.DomainAdapterSpec{}}
	} else {
		cfg, e = cfgpkg.Load(s.configPath)
		if e != nil {
			return result, fmt.Errorf("package doctor adapter config: %w", e)
		}
	}
	result.AdapterPreflight, e = blockcatalog.PreflightAdapters(ctx, catalog, cfg, s.adapterFactory(cfg))
	if e != nil {
		return result, fmt.Errorf("package doctor adapter preflight: %w", e)
	}
	return result, nil
}

type NotificationBackend interface {
	List(bool, int) ([]notification.Item, error)
	Ack(string) (*notification.Item, error)
	Test(string) (*notification.Item, error)
	Dispatch() ([]notification.Item, error)
}
type NotificationItem = notification.Item
type NotificationService struct{ backend NotificationBackend }

func NewNotification(b NotificationBackend) *NotificationService {
	return &NotificationService{backend: b}
}
func (s *NotificationService) List(unread bool, limit int) ([]notification.Item, error) {
	return s.backend.List(unread, limit)
}
func (s *NotificationService) Ack(id string) (*notification.Item, error) { return s.backend.Ack(id) }
func (s *NotificationService) Test(message string) (*notification.Item, error) {
	return s.backend.Test(message)
}
func (s *NotificationService) Dispatch() ([]notification.Item, error) { return s.backend.Dispatch() }

type BlockCatalogView struct {
	Profile     string                        `json:"profile"`
	Packages    []blockcatalog.PackageSummary `json:"packages"`
	Blocks      []blockcatalog.ResolvedBlock  `json:"blocks"`
	Templates   map[string]string             `json:"templates,omitempty"`
	Governance  blockcatalog.Governance       `json:"governance,omitempty"`
	Fingerprint string                        `json:"fingerprint"`
}
type BlockService struct{ workspace string }

func NewBlocks(workspace string) *BlockService { return &BlockService{workspace: workspace} }
func (s *BlockService) List(profileName string) (*BlockCatalogView, error) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		profileName = "code"
	}
	resolved, e := profile.Resolve(profileName, s.workspace)
	if e != nil {
		return nil, e
	}
	catalog, e := catalogload.FromResolved(resolved, s.workspace)
	if e != nil {
		return nil, e
	}
	names := make([]string, 0, len(catalog.Blocks))
	for n := range catalog.Blocks {
		names = append(names, n)
	}
	sort.Strings(names)
	v := &BlockCatalogView{Profile: profileName, Packages: catalog.Packages, Templates: catalog.Templates, Governance: catalog.Governance, Fingerprint: catalog.Fingerprint}
	for _, n := range names {
		v.Blocks = append(v.Blocks, catalog.Blocks[n])
	}
	return v, nil
}
func (s *BlockService) Describe(profileName, name string) (*blockcatalog.ResolvedBlock, error) {
	v, e := s.List(profileName)
	if e != nil {
		return nil, e
	}
	for _, b := range v.Blocks {
		if b.Name == name {
			x := b
			return &x, nil
		}
	}
	return nil, fmt.Errorf("trusted block %q was not found in profile %q", name, v.Profile)
}
func (s *BlockService) Validate(path string) (*blockcatalog.Catalog, error) {
	return blockcatalog.LoadOne(strings.TrimSpace(path))
}

type Services struct {
	Adapters      *AdapterService
	Packages      *PackageService
	Notifications *NotificationService
	Blocks        *BlockService
}
