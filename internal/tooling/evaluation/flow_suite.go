package evaluation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"takt/internal/yamlcodec"
)

const FlowSuiteVersion = "takt-flow-evaluation/v1alpha1"
const FlowValidatorProtocol = "takt-evaluation-validator/v1alpha1"

func IsLegacyFlowSuite(path string) (bool, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var header map[string]json.RawMessage
	if err := yamlcodec.Unmarshal(source, &header); err != nil {
		return false, err
	}
	var version string
	if raw := header["version"]; raw != nil {
		if err := json.Unmarshal(raw, &version); err != nil {
			return false, err
		}
	}
	return version == FlowSuiteVersion, nil
}

type FlowSuite struct {
	Version                                                              string            `json:"version"`
	Workflow                                                             string            `json:"workflow"`
	Config                                                               string            `json:"config"`
	Cases                                                                FlowCasesSpec     `json:"cases"`
	Approvals                                                            FlowApprovalsSpec `json:"approvals,omitempty"`
	External                                                             FlowExternalSpec  `json:"external,omitempty"`
	Validator                                                            FlowValidatorSpec `json:"validator"`
	Gates                                                                *FlowGates        `json:"gates,omitempty"`
	SuitePath, SuiteDir, ResolvedWorkflow, ResolvedConfig, ResolvedCases string            `json:"-"`
	Source                                                               []byte            `json:"-"`
}
type FlowCasesSpec struct {
	Directory string `json:"directory"`
}
type FlowApprovalsSpec struct {
	Default string `json:"default,omitempty"`
}
type FlowExternalSpec struct {
	GitHub *FlowGitHubSpec `json:"github,omitempty"`
}
type FlowGitHubSpec struct {
	Mode    string `json:"mode"`
	Require string `json:"require"`
}
type FlowValidatorSpec struct {
	ID              string        `json:"id"`
	Version         string        `json:"version"`
	Command         []string      `json:"command"`
	Path            string        `json:"path"`
	TimeoutText     string        `json:"timeout"`
	MaxOutputBytes  int           `json:"max_output_bytes"`
	Timeout         time.Duration `json:"-"`
	ResolvedCommand []string      `json:"-"`
	ResolvedPath    string        `json:"-"`
}
type FlowThreshold struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}
type FlowCountThreshold struct {
	Max *int `json:"max,omitempty"`
}
type FlowGates struct {
	ValidationErrorRate FlowThreshold      `json:"validation_error_rate,omitempty"`
	ValidRate           FlowThreshold      `json:"valid_rate,omitempty"`
	FalseAcceptRate     FlowThreshold      `json:"false_accept_rate,omitempty"`
	FalseRejectRate     FlowThreshold      `json:"false_reject_rate,omitempty"`
	FlowCompletionRate  FlowThreshold      `json:"flow_completion_rate,omitempty"`
	UnstableCases       FlowCountThreshold `json:"unstable_cases,omitempty"`
}
type FlowExpectationTakt struct {
	ApprovalAnswer string `json:"approval_answer,omitempty"`
}
type FlowExpectation struct {
	Takt   FlowExpectationTakt `json:"takt,omitempty"`
	Oracle json.RawMessage     `json:"oracle"`
}

func LoadFlowSuite(path string) (*FlowSuite, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s FlowSuite
	if err := yamlcodec.Unmarshal(src, &s); err != nil {
		return nil, err
	}
	if s.Version != FlowSuiteVersion {
		return nil, fmt.Errorf("version must be %s", FlowSuiteVersion)
	}
	if strings.TrimSpace(s.Workflow) == "" || strings.TrimSpace(s.Config) == "" || strings.TrimSpace(s.Cases.Directory) == "" {
		return nil, fmt.Errorf("workflow, config, and cases.directory are required")
	}
	if strings.TrimSpace(s.Validator.ID) == "" || strings.TrimSpace(s.Validator.Version) == "" || len(s.Validator.Command) == 0 || strings.TrimSpace(s.Validator.Command[0]) == "" {
		return nil, fmt.Errorf("validator id, version, and command are required")
	}
	if strings.TrimSpace(s.Validator.Path) == "" {
		return nil, fmt.Errorf("validator path is required")
	}
	if s.Validator.MaxOutputBytes <= 0 {
		return nil, fmt.Errorf("validator max_output_bytes must be positive")
	}
	s.Validator.Timeout, err = time.ParseDuration(s.Validator.TimeoutText)
	if err != nil || s.Validator.Timeout <= 0 {
		return nil, fmt.Errorf("validator timeout must be positive duration")
	}
	if s.External.GitHub != nil && (s.External.GitHub.Mode != "fixture" || (s.External.GitHub.Require != "repository" && s.External.GitHub.Require != "pull_request")) {
		return nil, fmt.Errorf("unsupported github mode or require")
	}
	s.SuitePath, _ = filepath.Abs(path)
	s.SuitePath = filepath.Clean(s.SuitePath)
	s.SuiteDir = filepath.Dir(s.SuitePath)
	s.Source = append([]byte(nil), src...)
	s.ResolvedConfig = resolveRelative(s.SuiteDir, s.Config)
	s.ResolvedCases = resolveRelative(s.SuiteDir, s.Cases.Directory)
	s.Validator.ResolvedPath = resolveRelative(s.SuiteDir, s.Validator.Path)
	s.Validator.ResolvedCommand = append([]string(nil), s.Validator.Command...)
	if strings.Contains(s.Validator.Command[0], "/") {
		s.Validator.ResolvedCommand[0] = resolveRelative(s.SuiteDir, s.Validator.Command[0])
	}
	if p := resolveRelative(s.SuiteDir, s.Workflow); fileRegular(p) {
		s.ResolvedWorkflow = p
	} else if strings.ContainsAny(s.Workflow, `/\\`) {
		return nil, fmt.Errorf("workflow must be profile selector or regular file")
	}
	if s.Gates == nil {
		z := float64(0)
		o := float64(1)
		s.Gates = &FlowGates{ValidationErrorRate: FlowThreshold{Max: &z}, FlowCompletionRate: FlowThreshold{Min: &o}, ValidRate: FlowThreshold{Min: &o}}
	} else if err := validateGates(*s.Gates); err != nil {
		return nil, err
	}
	if err := validateGatePresence(src); err != nil {
		return nil, err
	}
	return &s, nil
}
func LoadFlowExpectation(path string) (*FlowExpectation, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e FlowExpectation
	if err = yamlcodec.Unmarshal(src, &e); err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(normalizeJSON(src), &raw); err != nil {
		return nil, err
	}
	o, ok := raw["oracle"]
	if !ok || bytes.Equal(bytes.TrimSpace(o), []byte("null")) {
		return nil, fmt.Errorf("oracle is required")
	}
	if strings.IndexByte(e.Takt.ApprovalAnswer, 0) >= 0 {
		return nil, fmt.Errorf("approval_answer contains NUL")
	}
	return &e, nil
}
func normalizeJSON(src []byte) []byte {
	var v any
	if yamlcodec.Unmarshal(src, &v) == nil {
		b, _ := json.Marshal(v)
		return b
	}
	return src
}
func resolveRelative(dir, p string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	a, _ := filepath.Abs(p)
	return filepath.Clean(a)
}
func fileRegular(p string) bool { i, e := os.Stat(p); return e == nil && i.Mode().IsRegular() }
func validateGates(g FlowGates) error {
	if g.UnstableCases.Max != nil && *g.UnstableCases.Max < 0 {
		return fmt.Errorf("unstable_cases max must be non-negative")
	}
	for _, t := range []FlowThreshold{g.ValidationErrorRate, g.ValidRate, g.FalseAcceptRate, g.FalseRejectRate, g.FlowCompletionRate} {
		if t.Min != nil && t.Max != nil {
			return fmt.Errorf("gate must specify exactly one of min or max")
		}
		if t.Min == nil && t.Max == nil {
			continue
		}
		v := t.Min
		if v == nil {
			v = t.Max
		}
		if *v < 0 || *v > 1 {
			return fmt.Errorf("gate rate must be between 0 and 1")
		}
	}
	return nil
}
func validateGatePresence(src []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(normalizeJSON(src), &root); err != nil {
		return err
	}
	raw, ok := root["gates"]
	if !ok {
		return nil
	}
	var gates map[string]json.RawMessage
	if err := json.Unmarshal(raw, &gates); err != nil {
		return err
	}
	for _, name := range []string{"validation_error_rate", "valid_rate", "false_accept_rate", "false_reject_rate", "flow_completion_rate"} {
		if v, ok := gates[name]; ok {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(v, &m); err != nil {
				return err
			}
			if len(m) != 1 {
				return fmt.Errorf("%s must specify exactly one of min or max", name)
			}
			if _, ok := m["min"]; !ok {
				if _, ok := m["max"]; !ok {
					return fmt.Errorf("%s must specify min or max", name)
				}
			}
		}
	}
	if v, ok := gates["unstable_cases"]; ok {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(v, &m); err != nil {
			return err
		}
		if _, ok := m["max"]; !ok || len(m) != 1 {
			return fmt.Errorf("unstable_cases requires max")
		}
	}
	return nil
}
