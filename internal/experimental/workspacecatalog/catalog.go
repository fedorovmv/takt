package workspacecatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"takt/internal/yamlcodec"
)

const (
	APIVersion = "takt/v1alpha1"
	Kind       = "Workspace"
)

type Repository struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	DependsOn []string `json:"depends_on,omitempty"`
}

type Manifest struct {
	APIVersion   string       `json:"apiVersion"`
	Kind         string       `json:"kind"`
	Repositories []Repository `json:"repositories"`
}

type ResolvedRepository struct {
	ID           string   `json:"id"`
	Path         string   `json:"path"`
	AbsolutePath string   `json:"absolute_path"`
	DependsOn    []string `json:"depends_on,omitempty"`
	Head         string   `json:"head,omitempty"`
}

type Catalog struct {
	Root         string               `json:"root"`
	Source       string               `json:"source"`
	Repositories []ResolvedRepository `json:"repositories"`
	Fingerprint  string               `json:"fingerprint"`
}

func Load(ctx context.Context, workspace string) (*Catalog, error) {
	root, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	manifestPath := filepath.Join(root, ".takt", "workspace.yaml")
	if raw, err := os.ReadFile(manifestPath); err == nil {
		var manifest Manifest
		if err := yamlcodec.Unmarshal(raw, &manifest); err != nil {
			return nil, fmt.Errorf("parse workspace manifest: %w", err)
		}
		return resolve(ctx, root, manifest, manifestPath)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	manifest, err := Discover(ctx, root)
	if err != nil {
		return nil, err
	}
	return resolve(ctx, root, manifest, "auto")
}

func Discover(ctx context.Context, root string) (Manifest, error) {
	manifest := Manifest{APIVersion: APIVersion, Kind: Kind}
	if ok, _ := isRepositoryRoot(ctx, root); ok {
		manifest.Repositories = append(manifest.Repositories, Repository{ID: safeID(filepath.Base(root)), Path: "."})
		return manifest, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return manifest, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == ".takt" || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if ok, _ := isRepositoryRoot(ctx, path); ok {
			manifest.Repositories = append(manifest.Repositories, Repository{ID: safeID(entry.Name()), Path: entry.Name()})
		}
	}
	sort.Slice(manifest.Repositories, func(i, j int) bool { return manifest.Repositories[i].ID < manifest.Repositories[j].ID })
	return manifest, nil
}

func resolve(ctx context.Context, root string, manifest Manifest, source string) (*Catalog, error) {
	if source == "auto" {
		manifest.APIVersion = APIVersion
		manifest.Kind = Kind
	} else if manifest.APIVersion == "" || manifest.Kind == "" {
		return nil, fmt.Errorf("workspace manifest must declare apiVersion %s and kind %s", APIVersion, Kind)
	}
	if manifest.APIVersion != APIVersion || manifest.Kind != Kind {
		return nil, fmt.Errorf("workspace must use apiVersion %s and kind %s", APIVersion, Kind)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return nil, err
	}
	resolvedRoot = filepath.Clean(resolvedRoot)
	if len(manifest.Repositories) == 0 {
		if source != "auto" {
			return nil, fmt.Errorf("workspace manifest repositories must contain at least one repository")
		}
		return &Catalog{Root: resolvedRoot, Source: source, Fingerprint: fingerprint(nil)}, nil
	}
	seen := map[string]bool{}
	resolved := make([]ResolvedRepository, 0, len(manifest.Repositories))
	for _, repo := range manifest.Repositories {
		repo.ID = strings.TrimSpace(repo.ID)
		repo.Path = strings.TrimSpace(repo.Path)
		if !validRepositoryID(repo.ID) {
			return nil, fmt.Errorf("invalid repository id %q (expected [a-z][a-z0-9-]{0,62})", repo.ID)
		}
		if seen[repo.ID] {
			return nil, fmt.Errorf("duplicate repository id %q", repo.ID)
		}
		seen[repo.ID] = true
		if repo.Path == "" {
			return nil, fmt.Errorf("repository %s path is required", repo.ID)
		}
		abs := repo.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, abs)
		}
		absPath, err := filepath.Abs(abs)
		if err != nil {
			return nil, err
		}
		abs = filepath.Clean(absPath)
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("repository %s path %s escapes workspace %s", repo.ID, repo.Path, root)
		}
		resolvedAbs, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("resolve repository %s path %s: %w", repo.ID, repo.Path, err)
		}
		resolvedAbs, err = filepath.Abs(resolvedAbs)
		if err != nil {
			return nil, err
		}
		resolvedAbs = filepath.Clean(resolvedAbs)
		physicalRel, err := filepath.Rel(resolvedRoot, resolvedAbs)
		if err != nil || physicalRel == ".." || strings.HasPrefix(physicalRel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("repository %s path %s escapes workspace after resolving symlinks", repo.ID, repo.Path)
		}
		ok, actual := isRepositoryRoot(ctx, resolvedAbs)
		if !ok {
			return nil, fmt.Errorf("repository %s path %s is not a Git repository root (resolved %s)", repo.ID, repo.Path, actual)
		}
		head, _ := gitOutput(ctx, resolvedAbs, "rev-parse", "HEAD")
		resolved = append(resolved, ResolvedRepository{ID: repo.ID, Path: filepath.ToSlash(rel), AbsolutePath: resolvedAbs, DependsOn: unique(repo.DependsOn), Head: head})
	}
	for _, repo := range resolved {
		for _, dep := range repo.DependsOn {
			if !seen[dep] {
				return nil, fmt.Errorf("repository %s depends on unknown repository %s", repo.ID, dep)
			}
		}
	}
	if err := validateAcyclic(resolved); err != nil {
		return nil, err
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].ID < resolved[j].ID })
	return &Catalog{Root: root, Source: source, Repositories: resolved, Fingerprint: fingerprint(resolved)}, nil
}

func (c *Catalog) Get(id string) (ResolvedRepository, bool) {
	for _, r := range c.Repositories {
		if r.ID == id {
			return r, true
		}
	}
	return ResolvedRepository{}, false
}
func (c *Catalog) PlannerView() []map[string]any {
	out := make([]map[string]any, 0, len(c.Repositories))
	for _, r := range c.Repositories {
		out = append(out, map[string]any{"id": r.ID, "path": r.Path, "depends_on": r.DependsOn, "head": r.Head})
	}
	return out
}

func isRepositoryRoot(ctx context.Context, path string) (bool, string) {
	actual, err := gitOutput(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return false, ""
	}
	a, err := canonicalPath(actual)
	if err != nil {
		return false, ""
	}
	p, err := canonicalPath(path)
	if err != nil {
		return false, a
	}
	return a == p, a
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
func safeID(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		return "repo"
	}
	if id[0] < 'a' || id[0] > 'z' {
		id = "repo-" + id
	}
	if len(id) > 63 {
		id = strings.TrimRight(id[:63], "-")
	}
	return id
}

func validRepositoryID(id string) bool {
	if len(id) == 0 || len(id) > 63 || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}
func unique(in []string) []string {
	m := map[string]bool{}
	var out []string
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" && !m[v] {
			m[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
func validateAcyclic(repos []ResolvedRepository) error {
	deps := map[string][]string{}
	for _, r := range repos {
		deps[r.ID] = r.DependsOn
	}
	vis := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if vis[id] == 1 {
			return fmt.Errorf("repository dependency cycle contains %s", id)
		}
		if vis[id] == 2 {
			return nil
		}
		vis[id] = 1
		for _, d := range deps[id] {
			if err := visit(d); err != nil {
				return err
			}
		}
		vis[id] = 2
		return nil
	}
	for id := range deps {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
func fingerprint(repos []ResolvedRepository) string {
	h := sha256.New()
	for _, r := range repos {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\n", r.ID, r.Path, strings.Join(r.DependsOn, ","), r.Head)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
