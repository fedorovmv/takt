package profile

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"takt/internal/yamlmini"
)

//go:embed builtin/**
var builtins embed.FS

type Metadata struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Manifest struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Metadata   Metadata  `json:"metadata"`
	Workflow   string    `json:"workflow"`
	Config     string    `json:"config"`
	Input      InputSpec `json:"input,omitempty"`
}

type InputSpec struct {
	Format       string `json:"format,omitempty"`
	PreservePath bool   `json:"preserve_path,omitempty"`
}

type Resolved struct {
	Name         string
	ManifestPath string
	WorkflowPath string
	ConfigPath   string
	Manifest     Manifest
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

func Resolve(name, workspace string) (*Resolved, error) {
	candidates := []string{
		filepath.Join(workspace, ".takt", "profiles", name, "profile.yaml"),
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".takt", "profiles", name, "profile.yaml"))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return Load(candidate)
		}
	}
	return nil, fmt.Errorf("profile %q was not found; run 'takt init %s' first", name, name)
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
	dir := filepath.Dir(path)
	workflowPath, err := secureJoin(dir, manifest.Workflow)
	if err != nil {
		return nil, fmt.Errorf("profile workflow: %w", err)
	}
	configPath, err := secureJoin(dir, manifest.Config)
	if err != nil {
		return nil, fmt.Errorf("profile config: %w", err)
	}
	return &Resolved{Name: manifest.Metadata.Name, ManifestPath: path, WorkflowPath: workflowPath, ConfigPath: configPath, Manifest: manifest}, nil
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

func PrepareInput(spec InputSpec, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	b, err := os.ReadFile(value)
	if err != nil {
		return value, nil
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
