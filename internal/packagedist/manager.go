package packagedist

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"takt/internal/blockcatalog"
	"takt/internal/version"
	"takt/internal/yamlmini"
)

type Manager struct {
	Workspace, Home string
	saveLock        func(string, Lock) error
}

func New(workspace string) (*Manager, error) {
	w, e := filepath.Abs(workspace)
	if e != nil {
		return nil, e
	}
	h, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	return &Manager{Workspace: w, Home: h, saveLock: saveLock}, nil
}

func (m *Manager) projectLockPath() string {
	return filepath.Join(m.Workspace, ".takt", "takt.lock.json")
}
func (m *Manager) globalLockPath() string { return filepath.Join(m.Home, ".takt", "takt.lock.json") }
func (m *Manager) lockPath(scope string) string {
	if scope == "global" {
		return m.globalLockPath()
	}
	return m.projectLockPath()
}
func (m *Manager) installBase(scope string) string {
	if scope == "global" {
		return filepath.Join(m.Home, ".takt", "packages", "global")
	}
	return filepath.Join(m.Workspace, ".takt", "packages", scope)
}
func (m *Manager) installDir(p LockedPackage) string {
	return filepath.Join(m.installBase(p.Scope), p.Name, p.Version)
}
func (m *Manager) manifestPath(p LockedPackage) string {
	return filepath.Join(m.installDir(p), "package.yaml")
}

func (m *Manager) Install(ctx context.Context, source string, opts InstallOptions) (*LockedPackage, error) {
	if opts.Scope == "" {
		opts.Scope = "project"
	}
	if !validScope(opts.Scope) {
		return nil, fmt.Errorf("package scope must be global, corporate, or project")
	}
	staged, cleanup, src, err := m.stageSource(ctx, source, opts.Ref, "")
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return m.installStaged(staged, src, opts.Scope, false, nil)
}

func (m *Manager) Update(ctx context.Context, name, scope, ref string) (*LockedPackage, error) {
	entry, err := m.findLocked(name, scope)
	if err != nil {
		return nil, err
	}
	sourceArg := entry.Source.Location
	if entry.Source.Type == "git" {
		sourceArg = "git+" + sourceArg
	}
	updateRef := entry.Source.Ref
	if strings.TrimSpace(ref) != "" {
		updateRef = strings.TrimSpace(ref)
	}
	staged, cleanup, src, err := m.stageSource(ctx, sourceArg, updateRef, "")
	if err != nil {
		return nil, err
	}
	defer cleanup()
	src.Type = entry.Source.Type
	return m.installStaged(staged, src, entry.Scope, false, &entry)
}

func (m *Manager) Sync(ctx context.Context) (*DoctorReport, error) {
	entries, err := m.List()
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		root := m.installDir(entry)
		sum, _ := TreeChecksum(root)
		if sum == entry.Checksum {
			continue
		}
		sourceArg := entry.Source.Location
		if entry.Source.Type == "git" {
			sourceArg = "git+" + sourceArg
		}
		staged, cleanup, src, stageErr := m.stageSource(ctx, sourceArg, entry.Source.Ref, entry.Source.Commit)
		if stageErr != nil {
			return nil, fmt.Errorf("sync %s: %w", entry.Name, stageErr)
		}
		if src.Type == "local" {
			src = entry.Source
		}
		_, installErr := m.installStaged(staged, src, entry.Scope, true, &entry)
		cleanup()
		if installErr != nil {
			return nil, fmt.Errorf("sync %s: %w", entry.Name, installErr)
		}
	}
	return m.Doctor()
}

func (m *Manager) Uninstall(name, scope string) error {
	entry, err := m.findLocked(name, scope)
	if err != nil {
		return err
	}
	if err := m.validateDependencyGraph(nil, entry.Name, entry.Scope); err != nil {
		return err
	}
	lockPath := m.lockPath(entry.Scope)
	lock, err := loadLock(lockPath)
	if err != nil {
		return err
	}
	out := lock.Packages[:0]
	for _, p := range lock.Packages {
		if !(p.Name == entry.Name && p.Scope == entry.Scope) {
			out = append(out, p)
		}
	}
	lock.Packages = out
	if err := m.writeLock(lockPath, lock); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(m.installBase(entry.Scope), entry.Name))
}

func (m *Manager) List() ([]LockedPackage, error) {
	var out []LockedPackage
	for _, p := range []string{m.globalLockPath(), m.projectLockPath()} {
		l, e := loadLock(p)
		if e != nil {
			return nil, e
		}
		out = append(out, l.Packages...)
	}
	sort.Slice(out, func(i, j int) bool {
		if scopeRank(out[i].Scope) != scopeRank(out[j].Scope) {
			return scopeRank(out[i].Scope) < scopeRank(out[j].Scope)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (m *Manager) Doctor() (*DoctorReport, error) {
	entries, err := m.List()
	if err != nil {
		return nil, err
	}
	report := &DoctorReport{Status: "ready"}
	policies, err := m.policies()
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		item := DoctorItem{Name: entry.Name, Version: entry.Version, Scope: entry.Scope, Status: "ready", Checksum: entry.Checksum}
		if policyErr := validateSourcePolicy(entry.Source, policies); policyErr != nil {
			item.Problems = append(item.Problems, policyErr.Error())
		}
		manifest := m.manifestPath(entry)
		pkg, root, readErr := readManifest(manifest)
		if readErr != nil {
			item.Problems = append(item.Problems, readErr.Error())
		} else {
			sum, sumErr := TreeChecksum(root)
			if sumErr != nil {
				item.Problems = append(item.Problems, sumErr.Error())
			} else if sum != entry.Checksum {
				item.Problems = append(item.Problems, "installed content checksum differs from lock")
			}
			if pkg.Metadata.Name != entry.Name || pkg.Metadata.Version != entry.Version || pkg.Metadata.Scope != entry.Scope {
				item.Problems = append(item.Problems, "installed manifest identity differs from lock")
			}
			if !Satisfies(version.Value, pkg.Requirements.Takt) {
				item.Problems = append(item.Problems, fmt.Sprintf("requires Takt %s, current %s", pkg.Requirements.Takt, version.Value))
			}
			for _, dep := range pkg.Dependencies {
				p, depErr := resolveLockedDependency(entries, dep)
				if depErr != nil {
					item.Problems = append(item.Problems, depErr.Error())
				} else if !Satisfies(p.Version, dep.Version) {
					item.Problems = append(item.Problems, fmt.Sprintf("dependency %s %s is not satisfied by %s/%s %s", dep.Name, dep.Version, p.Scope, p.Name, p.Version))
				}
			}
			verified, key, verifyErr := verifyPackageSignature(root, entry.Scope, policies)
			if verifyErr != nil {
				item.Problems = append(item.Problems, verifyErr.Error())
			} else if verified {
				item.Signature = "verified:" + key
			} else {
				item.Signature = "not-required"
			}
		}
		if len(item.Problems) > 0 {
			item.Status = "error"
			report.Status = "error"
		}
		report.Packages = append(report.Packages, item)
	}
	return report, nil
}

func resolveLockedDependency(entries []LockedPackage, dep blockcatalog.Dependency) (LockedPackage, error) {
	var matches []LockedPackage
	for _, p := range entries {
		if p.Name != dep.Name {
			continue
		}
		if dep.Scope != "" && p.Scope != dep.Scope {
			continue
		}
		matches = append(matches, p)
	}
	if len(matches) == 0 {
		if dep.Scope != "" {
			return LockedPackage{}, fmt.Errorf("dependency %s %s in scope %s is not installed", dep.Name, dep.Version, dep.Scope)
		}
		return LockedPackage{}, fmt.Errorf("dependency %s %s is not installed", dep.Name, dep.Version)
	}
	if dep.Scope == "" && len(matches) > 1 {
		return LockedPackage{}, fmt.Errorf("dependency %s is ambiguous across scopes; specify dependency.scope", dep.Name)
	}
	return matches[0], nil
}

func (m *Manager) installStaged(staged string, src Source, scope string, exact bool, previous *LockedPackage) (*LockedPackage, error) {
	manifest := filepath.Join(staged, "package.yaml")
	pkg, root, err := readManifest(manifest)
	if err != nil {
		return nil, err
	}
	if pkg.Metadata.Scope != scope {
		return nil, fmt.Errorf("package %s metadata.scope %q does not match install scope %q", pkg.Metadata.Name, pkg.Metadata.Scope, scope)
	}
	if !Satisfies(version.Value, pkg.Requirements.Takt) {
		return nil, fmt.Errorf("package %s requires Takt %s, current %s", pkg.Metadata.Name, pkg.Requirements.Takt, version.Value)
	}
	if _, err := blockcatalog.LoadOne(manifest); err != nil {
		return nil, err
	}
	checksum, err := TreeChecksum(root)
	if err != nil {
		return nil, err
	}
	if exact && previous != nil && (checksum != previous.Checksum || pkg.Metadata.Version != previous.Version) {
		return nil, fmt.Errorf("source no longer reproduces locked package %s %s checksum %s", previous.Name, previous.Version, previous.Checksum)
	}
	policies, err := m.policies()
	if err != nil {
		return nil, err
	}
	if err := validateSourcePolicy(src, policies); err != nil {
		return nil, err
	}
	verified, key, err := verifyPackageSignature(root, scope, policies)
	if err != nil {
		return nil, err
	}
	candidate := &packageCandidate{Name: pkg.Metadata.Name, Version: pkg.Metadata.Version, Scope: scope, Manifest: pkg}
	if err := m.validateDependencyGraph(candidate, "", ""); err != nil {
		return nil, err
	}
	entry := LockedPackage{Name: pkg.Metadata.Name, Version: pkg.Metadata.Version, Scope: scope, Source: src, Checksum: checksum, SignatureKeyID: key, SignatureVerified: verified, InstalledAt: time.Now().UTC()}
	target := m.installDir(entry)
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(parent, ".install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := copyTree(root, tmp); err != nil {
		return nil, err
	}
	if got, err := TreeChecksum(tmp); err != nil || got != checksum {
		return nil, fmt.Errorf("staged package checksum changed during copy")
	}
	backup := ""
	if _, statErr := os.Stat(target); statErr == nil {
		backup = target + ".previous"
		if err := os.RemoveAll(backup); err != nil {
			return nil, err
		}
		if err := os.Rename(target, backup); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if err := os.Rename(tmp, target); err != nil {
		if backup != "" {
			_ = os.Rename(backup, target)
		}
		return nil, err
	}
	rollbackTarget := func() {
		_ = os.RemoveAll(target)
		if backup != "" {
			_ = os.Rename(backup, target)
		}
	}
	lockPath := m.lockPath(scope)
	lock, err := loadLock(lockPath)
	if err != nil {
		rollbackTarget()
		return nil, err
	}
	out := lock.Packages[:0]
	for _, p := range lock.Packages {
		if !(p.Name == entry.Name && p.Scope == scope) {
			out = append(out, p)
		}
	}
	lock.Packages = append(out, entry)
	sort.Slice(lock.Packages, func(i, j int) bool { return lock.Packages[i].Name < lock.Packages[j].Name })
	if err := m.writeLock(lockPath, lock); err != nil {
		rollbackTarget()
		return nil, err
	}
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	if previous != nil && previous.Version != entry.Version {
		_ = os.RemoveAll(m.installDir(*previous))
	}
	return &entry, nil
}

func (m *Manager) findLocked(name, scope string) (LockedPackage, error) {
	entries, err := m.List()
	if err != nil {
		return LockedPackage{}, err
	}
	var found []LockedPackage
	for _, p := range entries {
		if p.Name == name && (scope == "" || p.Scope == scope) {
			found = append(found, p)
		}
	}
	if len(found) == 0 {
		return LockedPackage{}, fmt.Errorf("package %q is not installed", name)
	}
	if len(found) > 1 {
		return LockedPackage{}, fmt.Errorf("package %q exists in multiple scopes; specify --scope", name)
	}
	return found[0], nil
}

func (m *Manager) stageSource(ctx context.Context, source, ref, commit string) (string, func(), Source, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", func() {}, Source{}, fmt.Errorf("package source is required")
	}
	explicitGit := strings.HasPrefix(source, "git+")
	if explicitGit {
		source = strings.TrimPrefix(source, "git+")
	}
	if info, err := os.Stat(source); err == nil && !explicitGit {
		abs, _ := filepath.Abs(source)
		root := abs
		if !info.IsDir() {
			root = filepath.Dir(abs)
		}
		return root, func() {}, Source{Type: "local", Location: abs}, nil
	}
	tmp, err := os.MkdirTemp("", "takt-package-git-")
	if err != nil {
		return "", func() {}, Source{}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", source, tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", func() {}, Source{}, fmt.Errorf("clone package source: %w: %s", err, strings.TrimSpace(string(out)))
	}
	checkout := commit
	if checkout == "" {
		checkout = ref
	}
	if checkout != "" {
		cmd = exec.CommandContext(ctx, "git", "-C", tmp, "checkout", "--quiet", checkout)
		if out, err := cmd.CombinedOutput(); err != nil {
			cleanup()
			return "", func() {}, Source{}, fmt.Errorf("checkout package %s: %w: %s", checkout, err, strings.TrimSpace(string(out)))
		}
	}
	cmd = exec.CommandContext(ctx, "git", "-C", tmp, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		cleanup()
		return "", func() {}, Source{}, err
	}
	return tmp, cleanup, Source{Type: "git", Location: source, Ref: ref, Commit: strings.TrimSpace(string(out))}, nil
}

func readManifest(path string) (blockcatalog.Package, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return blockcatalog.Package{}, "", fmt.Errorf("read package manifest: %w", err)
	}
	var pkg blockcatalog.Package
	if err := yamlmini.Unmarshal(b, &pkg); err != nil {
		return pkg, "", fmt.Errorf("parse package manifest: %w", err)
	}
	return pkg, filepath.Dir(path), nil
}

func loadLock(path string) (Lock, error) {
	l := Lock{APIVersion: LockAPIVersion, Kind: LockKind}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return l, fmt.Errorf("parse package lock %s: %w", path, err)
	}
	if l.APIVersion != LockAPIVersion || l.Kind != LockKind {
		return l, fmt.Errorf("invalid package lock %s", path)
	}
	return l, nil
}
func (m *Manager) writeLock(path string, l Lock) error {
	if m.saveLock != nil {
		return m.saveLock(path, l)
	}
	return saveLock(path, l)
}

func saveLock(path string, l Lock) error {
	l.APIVersion = LockAPIVersion
	l.Kind = LockKind
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(l, "", "  ")
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func TreeChecksum(root string) (string, error) {
	h := sha256.New()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		if d.IsDir() {
			if d.Name() == ".git" && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "package.sig" {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		io.WriteString(h, rel)
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(src, path)
		if e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		return os.WriteFile(target, b, mode)
	})
}

func (m *Manager) policies() ([]Policy, error) {
	var out []Policy
	for _, p := range []string{filepath.Join(m.Home, ".takt", "package-policy.yaml"), filepath.Join(m.Workspace, ".takt", "package-policy.yaml")} {
		b, e := os.ReadFile(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		var v Policy
		if e := yamlmini.Unmarshal(b, &v); e != nil {
			return nil, fmt.Errorf("parse package policy %s: %w", p, e)
		}
		if v.APIVersion != LockAPIVersion || v.Kind != PolicyKind {
			return nil, fmt.Errorf("invalid package policy %s", p)
		}
		out = append(out, v)
	}
	return out, nil
}
func validateSourcePolicy(src Source, policies []Policy) error {
	actual := src.Type + ":" + src.Location
	for _, p := range policies {
		if len(p.AllowedSources) == 0 {
			continue
		}
		ok := false
		for _, allowed := range p.AllowedSources {
			if sourceAllowed(src, strings.TrimSpace(allowed)) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("package source %s is not allowed by policy", actual)
		}
	}
	return nil
}

func sourceAllowed(src Source, allowed string) bool {
	prefix := src.Type + ":"
	if !strings.HasPrefix(allowed, prefix) {
		return false
	}
	want := strings.TrimSpace(strings.TrimPrefix(allowed, prefix))
	if want == "" {
		return false
	}
	if src.Type != "local" {
		actual := strings.TrimRight(strings.TrimSpace(src.Location), "/")
		base := strings.TrimRight(want, "/")
		return actual == base || strings.HasPrefix(actual, base+"/")
	}
	actual, err := filepath.Abs(src.Location)
	if err != nil {
		return false
	}
	base, err := filepath.Abs(want)
	if err != nil {
		return false
	}
	actual = filepath.Clean(actual)
	base = filepath.Clean(base)
	rel, err := filepath.Rel(base, actual)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func verifyPackageSignature(root, scope string, policies []Policy) (bool, string, error) {
	required := false
	keys := map[string]string{}
	for _, p := range policies {
		for _, s := range p.RequireSignatureScopes {
			if s == scope {
				required = true
			}
		}
		for k, v := range p.TrustedKeys {
			if old, ok := keys[k]; ok && old != v {
				return false, "", fmt.Errorf("trusted key %s differs between policies", k)
			}
			keys[k] = v
		}
	}
	path := filepath.Join(root, "package.sig")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if required {
			return false, "", fmt.Errorf("package scope %s requires a signature", scope)
		}
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	var sig Signature
	if err := yamlmini.Unmarshal(b, &sig); err != nil {
		return false, "", err
	}
	if sig.APIVersion != LockAPIVersion || sig.Kind != SignatureKind || sig.Algorithm != "ed25519" {
		return false, "", fmt.Errorf("invalid package signature envelope")
	}
	digest, err := TreeChecksum(root)
	if err != nil {
		return false, "", err
	}
	if sig.Digest != digest {
		return false, "", fmt.Errorf("package signature digest does not match content")
	}
	pubText, ok := keys[sig.KeyID]
	if !ok {
		if required {
			return false, "", fmt.Errorf("package signature key %s is not trusted", sig.KeyID)
		}
		return false, sig.KeyID, nil
	}
	pub, err := base64.StdEncoding.DecodeString(pubText)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false, "", fmt.Errorf("invalid trusted Ed25519 public key %s", sig.KeyID)
	}
	raw, err := base64.StdEncoding.DecodeString(sig.Signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), []byte(sig.Digest), raw) {
		return false, "", fmt.Errorf("invalid package signature for key %s", sig.KeyID)
	}
	return true, sig.KeyID, nil
}

func SignPackage(path, keyID, keyFile string) error {
	root := path
	if info, err := os.Stat(path); err != nil {
		return err
	} else if !info.IsDir() {
		root = filepath.Dir(path)
	}
	digest, err := TreeChecksum(root)
	if err != nil {
		return err
	}
	keyText, err := os.ReadFile(keyFile)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyText)))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return fmt.Errorf("private key must be base64 Ed25519 private key")
	}
	sig := ed25519.Sign(ed25519.PrivateKey(raw), []byte(digest))
	value := Signature{APIVersion: LockAPIVersion, Kind: SignatureKind, KeyID: keyID, Algorithm: "ed25519", Digest: digest, Signature: base64.StdEncoding.EncodeToString(sig)}
	b, _ := json.MarshalIndent(value, "", "  ")
	return os.WriteFile(filepath.Join(root, "package.sig"), append(b, '\n'), 0o644)
}

type packageCandidate struct {
	Name, Version, Scope string
	Manifest             blockcatalog.Package
}

func packageKey(scope, name string) string { return scope + "\x00" + name }

func (m *Manager) validateDependencyGraph(candidate *packageCandidate, removingName, removingScope string) error {
	entries, err := m.List()
	if err != nil {
		return err
	}
	versions := map[string]string{}
	manifests := map[string]blockcatalog.Package{}
	for _, entry := range entries {
		if entry.Name == removingName && entry.Scope == removingScope {
			continue
		}
		pkg, _, readErr := readManifest(m.manifestPath(entry))
		if readErr != nil {
			return readErr
		}
		key := packageKey(entry.Scope, entry.Name)
		versions[key] = entry.Version
		manifests[key] = pkg
	}
	if candidate != nil {
		key := packageKey(candidate.Scope, candidate.Name)
		versions[key] = candidate.Version
		manifests[key] = candidate.Manifest
	}
	for key, pkg := range manifests {
		for _, dep := range pkg.Dependencies {
			if dep.Scope != "" && !validScope(dep.Scope) {
				return fmt.Errorf("package %s dependency %s has invalid scope %q", key, dep.Name, dep.Scope)
			}
			var depKey, installedVersion string
			if dep.Scope != "" {
				depKey = packageKey(dep.Scope, dep.Name)
				installedVersion = versions[depKey]
			} else {
				var matches []string
				for candidateKey := range versions {
					parts := strings.SplitN(candidateKey, "\x00", 2)
					if len(parts) == 2 && parts[1] == dep.Name {
						matches = append(matches, candidateKey)
					}
				}
				sort.Strings(matches)
				if len(matches) > 1 {
					return fmt.Errorf("package %s dependency %s is ambiguous across scopes; specify dependency.scope", key, dep.Name)
				}
				if len(matches) == 1 {
					depKey = matches[0]
					installedVersion = versions[depKey]
				}
			}
			if depKey == "" || !Satisfies(installedVersion, dep.Version) {
				return fmt.Errorf("package %s requires dependency %s %s; installed version is %q", key, dep.Name, dep.Version, installedVersion)
			}
		}
	}
	return nil
}

func validScope(s string) bool { return s == "global" || s == "corporate" || s == "project" }
func scopeRank(s string) int {
	switch s {
	case "global":
		return 1
	case "corporate":
		return 2
	case "project":
		return 3
	}
	return 0
}

// InstalledManifestPaths resolves locked package manifests in increasing
// precedence. blockcatalog applies project > corporate > global > builtin for
// duplicate block names while merging governance fail-closed.
func InstalledManifestPaths(workspace string) ([]string, error) {
	m, err := New(workspace)
	if err != nil {
		return nil, err
	}
	report, err := m.Doctor()
	if err != nil {
		return nil, err
	}
	if report.Status != "ready" {
		return nil, fmt.Errorf("installed package integrity check failed; run 'takt package doctor' or 'takt package sync': %+v", report.Packages)
	}
	entries, err := m.List()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range entries {
		manifest := m.manifestPath(p)
		if _, err := os.Stat(manifest); err != nil {
			return nil, fmt.Errorf("locked package %s is missing; run 'takt package sync'", p.Name)
		}
		out = append(out, manifest)
	}
	return out, nil
}
