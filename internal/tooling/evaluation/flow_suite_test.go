package evaluation

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
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
	for _, bad := range []string{
		strings.Replace(flowValid, "path: validator", "path: validator\ntimeout: 0s", 1),
		flowValid + "gates: {validation_error_rate: {}}\n",
		flowValid + "gates: {unstable_cases: {}}\n",
		flowValid + "gates: {unstable_cases: {max: -1}}\n",
	} {
		if _, e := LoadFlowSuite(writeFlowTestFile(t, "bad.yaml", bad)); e == nil {
			t.Fatalf("accepted bad: %q", bad)
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

func TestFlowSuiteExplicitGatesReplaceDefaults(t *testing.T) {
	s, err := LoadFlowSuite(writeFlowTestFile(t, "suite.yaml", flowValid+"gates:\n  validation_error_rate: {max: 0}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Gates.ValidRate.Min != nil || s.Gates.ValidationErrorRate.Max == nil {
		t.Fatalf("gates=%+v", s.Gates)
	}
}

func TestFlowSchemasCompileOffline(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		good, preflight, bad []byte
	}{
		{"flow-evaluation-suite.schema.json", []byte(`{"version":"takt-flow-evaluation/v1alpha1","workflow":"x","config":"c","cases":{"directory":"cases"},"validator":{"id":"v","version":"1","command":["go"],"path":"p","timeout":"1s","max_output_bytes":1}}`), nil, []byte(`{"version":"bad"}`)},
		{"evaluation-validator-request.schema.json", []byte(`{"protocol_version":"takt-evaluation-validator/v1alpha1","type":"validation_request","case_id":"c","repeat":1,"workspace":"w","baseline_workspace":"b","expected_path":"e","run":{"id":"i","status":"completed","artifacts_dir":"a"}}`), []byte(`{"protocol_version":"takt-evaluation-validator/v1alpha1","type":"validation_request","case_id":"c","repeat":0,"workspace":"w","baseline_workspace":"b","expected_path":"e","run":{"id":"preflight","status":"not_started","artifacts_dir":""}}`), []byte(`{"type":"validation_request"}`)},
	} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", tc.name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) == "" {
			t.Fatal(tc.name)
		}
		c := jsonschema.NewCompiler()
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource(tc.name, doc); err != nil {
			t.Fatal(err)
		}
		sch, err := c.Compile(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		json.Unmarshal(tc.good, &v)
		if err := sch.Validate(v); err != nil {
			t.Fatal(err)
		}
		if tc.preflight != nil {
			json.Unmarshal(tc.preflight, &v)
			if err := sch.Validate(v); err != nil {
				t.Fatal(err)
			}
		}
		json.Unmarshal(tc.bad, &v)
		if err := sch.Validate(v); err == nil {
			t.Fatal("accepted bad", tc.name)
		}
	}
}
