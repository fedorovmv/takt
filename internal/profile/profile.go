package profile

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"takt/internal/packagedist"
	"takt/internal/yamlmini"
)

//go:embed builtin/**
var builtins embed.FS

type Metadata struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Manifest struct {
	APIVersion    string               `json:"apiVersion"`
	Kind          string               `json:"kind"`
	Metadata      Metadata             `json:"metadata"`
	Workflow      string               `json:"workflow"`
	Router        string               `json:"router,omitempty"`
	Workflows     map[string]string    `json:"workflows,omitempty"`
	Config        string               `json:"config"`
	Input         InputSpec            `json:"input,omitempty"`
	Inputs        map[string]InputSpec `json:"inputs,omitempty"`
	BlockPackages []string             `json:"block_packages,omitempty"`
}

type InputSpec struct {
	Format       string `json:"format,omitempty"`
	PreservePath bool   `json:"preserve_path,omitempty"`
}

type Resolved struct {
	Name              string
	WorkflowName      string
	ManifestPath      string
	WorkflowPath      string
	RouterPath        string
	ConfigPath        string
	Manifest          Manifest
	BlockPackagePaths []string
}

func Init(name, destination string, force bool) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("profile name is required")
	}
	root := filepath.Join(destination, ".takt", "profiles", name)
	if _, err := fs.Stat(builtins, "builtin/"+name+"/profile.yaml"); err != nil {
		return "", fmt.Errorf("unknown built-in profile %q", name)
	}
	if info, err := os.Stat(root); err == nil && info.IsDir() && !force {
		return "", fmt.Errorf("profile %q already exists at %s; use --force to replace it", name, root)
	}
	if force {
		if err := os.RemoveAll(root); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	err := fs.WalkDir(builtins, "builtin/"+name, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel("builtin/"+name, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := fs.ReadFile(builtins, path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(filepath.ToSlash(rel), "tools/") {
			mode = 0o755
		}
		return os.WriteFile(target, b, mode)
	})
	if err != nil {
		return "", err
	}
	configTarget := filepath.Join(destination, ".takt", "config.yaml")
	if _, err := os.Stat(configTarget); os.IsNotExist(err) {
		b, readErr := fs.ReadFile(builtins, "builtin/"+name+"/config.example.yaml")
		if readErr != nil {
			return "", readErr
		}
		if err := os.MkdirAll(filepath.Dir(configTarget), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(configTarget, b, 0o644); err != nil {
			return "", err
		}
	}
	return root, nil
}

func Resolve(selector, workspace string) (*Resolved, error) {
	name, workflowName := splitSelector(selector)
	candidates := []string{
		filepath.Join(workspace, ".takt", "profiles", name, "profile.yaml"),
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".takt", "profiles", name, "profile.yaml"))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			resolved, err := Load(candidate)
			if err != nil {
				return nil, err
			}
			installed, err := packagedist.InstalledManifestPaths(workspace)
			if err != nil {
				return nil, err
			}
			resolved.BlockPackagePaths = append(resolved.BlockPackagePaths, installed...)
			return resolved.SelectWorkflow(workflowName)
		}
	}
	return nil, fmt.Errorf("profile %q was not found; run 'takt init %s' first", name, name)
}

func splitSelector(selector string) (string, string) {
	selector = strings.TrimSpace(selector)
	name, workflowName, found := strings.Cut(selector, ":")
	if !found {
		return selector, ""
	}
	return strings.TrimSpace(name), strings.TrimSpace(workflowName)
}

func Load(path string) (*Resolved, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}
	var manifest Manifest
	if err := yamlmini.Unmarshal(b, &manifest); err != nil {
		return nil, fmt.Errorf("parse profile %s: %w", path, err)
	}
	if manifest.APIVersion != "takt/v1alpha1" {
		return nil, fmt.Errorf("unsupported profile apiVersion %q", manifest.APIVersion)
	}
	if manifest.Kind != "Profile" {
		return nil, fmt.Errorf("profile kind must be Profile")
	}
	if strings.TrimSpace(manifest.Metadata.Name) == "" {
		return nil, fmt.Errorf("profile metadata.name is required")
	}
	if strings.TrimSpace(manifest.Workflow) == "" {
		return nil, fmt.Errorf("profile workflow is required")
	}
	if strings.TrimSpace(manifest.Config) == "" {
		return nil, fmt.Errorf("profile config is required")
	}
	for name, workflowPath := range manifest.Workflows {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("profile workflow name is required")
		}
		if strings.Contains(name, ":") {
			return nil, fmt.Errorf("profile workflow name %q must not contain ':'", name)
		}
		if strings.TrimSpace(workflowPath) == "" {
			return nil, fmt.Errorf("profile workflow %q path is required", name)
		}
	}
	for name, input := range manifest.Inputs {
		if _, ok := manifest.Workflows[name]; !ok {
			return nil, fmt.Errorf("profile input override references unknown workflow %q", name)
		}
		if input.Format != "" && input.Format != "text" && input.Format != "markdown" && input.Format != "json" {
			return nil, fmt.Errorf("profile input %q format must be text, markdown, or json", name)
		}
	}
	dir := filepath.Dir(path)
	workflowPath, err := secureJoin(dir, manifest.Workflow)
	if err != nil {
		return nil, fmt.Errorf("profile workflow: %w", err)
	}
	configPath, err := secureJoin(dir, manifest.Config)
	if err != nil {
		return nil, fmt.Errorf("profile config: %w", err)
	}
	routerPath := ""
	if strings.TrimSpace(manifest.Router) != "" {
		routerPath, err = secureJoin(dir, manifest.Router)
		if err != nil {
			return nil, fmt.Errorf("profile router: %w", err)
		}
	}
	packagePaths := make([]string, 0, len(manifest.BlockPackages))
	for index, packagePath := range manifest.BlockPackages {
		if strings.TrimSpace(packagePath) == "" {
			return nil, fmt.Errorf("profile block_packages[%d] path is required", index)
		}
		resolvedPackage, err := secureJoin(dir, packagePath)
		if err != nil {
			return nil, fmt.Errorf("profile block package %q: %w", packagePath, err)
		}
		packagePaths = append(packagePaths, resolvedPackage)
	}
	return &Resolved{Name: manifest.Metadata.Name, ManifestPath: path, WorkflowPath: workflowPath, RouterPath: routerPath, ConfigPath: configPath, Manifest: manifest, BlockPackagePaths: packagePaths}, nil
}

func (r *Resolved) SelectWorkflow(name string) (*Resolved, error) {
	if strings.TrimSpace(name) == "" {
		clone := *r
		clone.WorkflowName = ""
		return &clone, nil
	}
	rel, ok := r.Manifest.Workflows[name]
	if !ok {
		names := make([]string, 0, len(r.Manifest.Workflows))
		for candidate := range r.Manifest.Workflows {
			names = append(names, candidate)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("profile %q has no workflow %q; available: %s", r.Name, name, strings.Join(names, ", "))
	}
	path, err := secureJoin(filepath.Dir(r.ManifestPath), rel)
	if err != nil {
		return nil, fmt.Errorf("profile workflow %q: %w", name, err)
	}
	clone := *r
	clone.WorkflowName = name
	clone.WorkflowPath = path
	return &clone, nil
}

func secureJoin(base, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return filepath.Clean(rel), nil
	}
	joined := filepath.Clean(filepath.Join(base, rel))
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func (r *Resolved) EffectiveInput() InputSpec {
	if r != nil && r.WorkflowName != "" {
		if value, ok := r.Manifest.Inputs[r.WorkflowName]; ok {
			return value
		}
	}
	if r == nil {
		return InputSpec{}
	}
	return r.Manifest.Input
}

func PrepareInput(spec InputSpec, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	b, err := os.ReadFile(value)
	if err != nil {
		return value, nil
	}
	if spec.Format == "json" {
		return string(b), nil
	}
	if spec.Format == "markdown" && spec.PreservePath {
		abs, err := filepath.Abs(value)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("# Takt input\n\nSource file: `%s`\n\nThe source file is authoritative. Update it when the workflow asks you to mark progress.\n\n---\n\n%s", abs, string(b)), nil
	}
	return string(b), nil
}
