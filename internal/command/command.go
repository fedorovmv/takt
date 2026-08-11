package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"takt/internal/yamlcodec"
)

type Command struct {
	Name         string
	Description  string
	Provider     string
	Model        string
	ArgumentHint string
	Metadata     map[string]any
	Body         string
	Path         string
	// Assistant is retained as an internal compatibility alias for runtime
	// callers while command authoring uses provider exclusively.
	Assistant string
}

type Resolver struct {
	Dirs []string
}

func (r Resolver) Resolve(name string) (*Command, error) {
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\\`) {
		return nil, fmt.Errorf("unsafe command name %q", name)
	}
	for _, dir := range r.Dirs {
		path := filepath.Join(dir, name+".md")
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		cmd, err := Parse(name, path, string(b))
		if err != nil {
			return nil, err
		}
		return cmd, nil
	}
	return nil, fmt.Errorf("command %q not found in %s", name, strings.Join(r.Dirs, ", "))
}

func Parse(name, path, src string) (*Command, error) {
	cmd := &Command{Name: name, Path: path, Metadata: map[string]any{}}
	if !strings.HasPrefix(src, "---\n") {
		cmd.Body = strings.TrimSpace(src)
		return cmd, nil
	}
	rest := src[4:]
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return nil, fmt.Errorf("command %s has unterminated frontmatter", path)
	}
	fmText := rest[:idx]
	var fm map[string]any
	if err := yamlcodec.Unmarshal([]byte(fmText), &fm); err != nil {
		return nil, fmt.Errorf("command %s frontmatter: %w", path, err)
	}
	if v, ok := fm["description"].(string); ok {
		cmd.Description = v
	}
	if _, ok := fm["assistant"]; ok {
		return nil, fmt.Errorf("command %s uses legacy frontmatter key assistant; use provider", path)
	}
	if v, ok := fm["provider"].(string); ok {
		cmd.Provider = v
		cmd.Assistant = v
	}
	if v, ok := fm["model"].(string); ok {
		cmd.Model = v
	}
	if v, ok := fm["argument-hint"].(string); ok {
		cmd.ArgumentHint = v
	}
	delete(fm, "description")
	delete(fm, "provider")
	delete(fm, "model")
	delete(fm, "argument-hint")
	cmd.Metadata = fm
	cmd.Body = strings.TrimSpace(rest[idx+5:])
	return cmd, nil
}
