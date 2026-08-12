package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFlowTestFile(t *testing.T, name, body string) string {
	t.Helper()
	d := t.TempDir()
	p := filepath.Join(d, name)
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

const flowValid = `version: takt-flow-evaluation/v1alpha1
workflow: code:feature-development
config: missing.yaml
cases: {directory: cases}
validator:
  id: mini-du
  version: "1"
  command: [go]
  path: validator
  timeout: 2m
  max_output_bytes: 100
`

func TestFlowSuiteContracts(t *testing.T) {
	s, err := LoadFlowSuite(writeFlowTestFile(t, "suite.yaml", flowValid))
	if err != nil {
		t.Fatal(err)
	}
	if s.Validator.Timeout != 2*time.Minute || s.ResolvedConfig == "" {
		t.Fatal(s)
	}
	if s.Gates == nil || s.Gates.ValidRate.Min == nil {
		t.Fatal("defaults")
	}
	for _, bad := range []string{"validator: {}", "validator:\n  id: x\n  version: x\n  command: [go]\n  path: x\n  timeout: 0s\n  max_output_bytes: 1", "validator:\n  id: x\n  version: x\n  command: [go]\n  path: x\n  timeout: 1s\n  max_output_bytes: 1\ngates: {validation_error_rate: {}}", "validator:\n  id: x\n  version: x\n  command: [go]\n  path: x\n  timeout: 1s\n  max_output_bytes: 1\ngates: {unstable_cases: {}}"} {
		_, e := LoadFlowSuite(writeFlowTestFile(t, "bad.yaml", strings.Replace(flowValid, "validator:", bad, 1)))
		if e == nil {
			t.Fatalf("accepted bad")
		}
	}
}
func TestFlowExpectationStrict(t *testing.T) {
	p := writeFlowTestFile(t, "exp.yaml", "takt: {approval_answer: \" ok \"}\noracle: {x: 1}\n")
	e, err := LoadFlowExpectation(p)
	if err != nil || string(e.Oracle) == "" {
		t.Fatal(e, err)
	}
	_, err = LoadFlowExpectation(writeFlowTestFile(t, "bad.yaml", `oracle: 1
extra: x`))
	if err == nil {
		t.Fatal("unknown accepted")
	}
	_, err = LoadFlowExpectation(writeFlowTestFile(t, "nul.yaml", "takt:\n  approval_answer: \"a\\u0000b\"\noracle: 1\n"))
	if err == nil {
		t.Fatal("nul accepted")
	}
}
