package definition

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"takt/internal/command"
	"takt/internal/spec"
)

type Fingerprints struct {
	Workflow string
	Config   string
	Commands string
}

func Compute(wf *spec.Workflow, cfg *spec.Config, workflowPath, configPath string, resolver command.Resolver) (Fingerprints, error) {
	workflowBytes, err := bytesOrJSON(workflowPath, wf)
	if err != nil {
		return Fingerprints{}, fmt.Errorf("fingerprint workflow: %w", err)
	}
	configBytes, err := bytesOrJSON(configPath, cfg)
	if err != nil {
		return Fingerprints{}, fmt.Errorf("fingerprint config: %w", err)
	}
	names := collectCommands(wf.Nodes)
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		cmd, err := resolver.Resolve(name)
		if err != nil {
			return Fingerprints{}, fmt.Errorf("fingerprint command %q: %w", name, err)
		}
		b, err := os.ReadFile(cmd.Path)
		if err != nil {
			return Fingerprints{}, fmt.Errorf("fingerprint command %q: %w", name, err)
		}
		_, _ = h.Write([]byte(name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{0})
	}
	return Fingerprints{
		Workflow: sum(workflowBytes),
		Config:   sum(configBytes),
		Commands: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func Verify(expected Fingerprints, actual Fingerprints) error {
	if expected.Workflow != "" && expected.Workflow != actual.Workflow {
		return &ChangedError{Kind: "workflow", Expected: expected.Workflow, Actual: actual.Workflow}
	}
	if expected.Config != "" && expected.Config != actual.Config {
		return &ChangedError{Kind: "config", Expected: expected.Config, Actual: actual.Config}
	}
	if expected.Commands != "" && expected.Commands != actual.Commands {
		return &ChangedError{Kind: "commands", Expected: expected.Commands, Actual: actual.Commands}
	}
	return nil
}

type ChangedError struct {
	Kind     string
	Expected string
	Actual   string
}

func (e *ChangedError) Error() string {
	return fmt.Sprintf("%s definition changed since run start", e.Kind)
}

func bytesOrJSON(path string, value any) ([]byte, error) {
	if path != "" && path[0] != '<' {
		if b, err := os.ReadFile(path); err == nil {
			return b, nil
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return json.Marshal(value)
}

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func collectCommands(nodes []spec.Node) []string {
	set := map[string]struct{}{}
	var visit func([]spec.Node)
	visit = func(items []spec.Node) {
		for _, node := range items {
			if node.Command != "" {
				set[node.Command] = struct{}{}
			}
			if node.LoopGroup != nil {
				visit(node.LoopGroup.Nodes)
			}
		}
	}
	visit(nodes)
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	return out
}
