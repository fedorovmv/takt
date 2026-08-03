package workflow

import (
	"fmt"
	"os"

	"takt/internal/spec"
	"takt/internal/yamlmini"
)

func Load(path string) (*spec.Workflow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workflow: %w", err)
	}
	var wf spec.Workflow
	if err := yamlmini.Unmarshal(b, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow %s: %w", path, err)
	}
	if err := Validate(&wf); err != nil {
		return nil, err
	}
	return &wf, nil
}
