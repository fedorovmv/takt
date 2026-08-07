package agentadapter

import (
	"os"
	"strings"
	"testing"
)

func TestValidateTranscript(t *testing.T) {
	good := `{"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2","capabilities":["skills","tool_control"],"tool_events":true,"tool_control":true}}
{"protocol_version":"takt-assistant/v1alpha2","type":"tool.request","tool_request":{"call_id":"call-1","tool":"read","input":{"path":"README.md"},"session_id":"session-1"}}
{"protocol_version":"takt-assistant/v1alpha2","type":"event","event":{"type":"tool.started","tool":"read","call_id":"call-1"}}
{"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"completed","output":"done","session":{"id":"session-1","resumed":true},"exit_code":0,"usage":{"input_tokens":1,"output_tokens":2,"cost":0.1}}}`
	report, err := ValidateTranscript(strings.NewReader(good), Options{RequireDeclaration: true, RequireToolControl: true, RequestedSessionID: "session-1", RequiredCapabilities: []string{"skills"}})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Terminal || report.Records != 4 || report.Events != 1 || report.ToolRequests != 1 {
		t.Fatalf("report=%#v", report)
	}

	bad := `{"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"completed","exit_code":0}}
{"protocol_version":"takt-assistant/v1alpha2","type":"event","event":{}}`
	if _, err := ValidateTranscript(strings.NewReader(bad), Options{}); err == nil {
		t.Fatal("expected trailing record rejection")
	}
}

func TestValidateTranscriptRequiresDeclarationBeforeToolRequest(t *testing.T) {
	stream := `{"protocol_version":"takt-assistant/v1alpha2","type":"tool.request","tool_request":{"call_id":"1","tool":"read"}}
{"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"completed","exit_code":0}}`
	if _, err := ValidateTranscript(strings.NewReader(stream), Options{}); err == nil || !strings.Contains(err.Error(), "tool_control") {
		t.Fatalf("expected undeclared tool control error, got %v", err)
	}
}

func TestPublicProtocolValidators(t *testing.T) {
	zero := 0
	goodReq := Request{ProtocolVersion: ProtocolV1Alpha2, Type: "request", RunID: "run-1", NodeID: "node-1", Attempt: 1, Model: Model{Provider: "test", ID: "model"}, Session: SessionRequest{Mode: "fresh"}}
	if err := ValidateRequest(goodReq); err != nil {
		t.Fatal(err)
	}
	bad := goodReq
	bad.Attempt = 0
	if err := ValidateRequest(bad); err == nil {
		t.Fatal("expected attempt validation")
	}
	bad = goodReq
	bad.RunID = ""
	if err := ValidateRequest(bad); err == nil {
		t.Fatal("expected run_id validation")
	}
	if err := ValidateDeclaration(Declaration{Protocol: EventProtocolV2, Capabilities: []string{"skills"}, EventTypes: []string{"message"}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDeclaration(Declaration{Protocol: EventProtocolV2, Capabilities: []string{""}}); err == nil {
		t.Fatal("expected empty capability rejection")
	}
	if err := ValidateResult(Result{ProtocolVersion: ProtocolV1Alpha2, Type: "result", Status: "completed", ExitCode: &zero}, ""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateToolRequest(ToolRequest{CallID: "", Tool: "read"}); err == nil {
		t.Fatal("expected call_id validation")
	}
}

func TestConformanceFixtures(t *testing.T) {
	cases := []struct {
		name, file string
		options    Options
		wantErr    bool
	}{
		{"success", "testdata/v1alpha2/success.ndjson", Options{RequireDeclaration: true}, false},
		{"resume", "testdata/v1alpha2/resume.ndjson", Options{RequireDeclaration: true, RequestedSessionID: "session-42"}, false},
		{"after-result", "testdata/v1alpha2/invalid-after-result.ndjson", Options{RequireDeclaration: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := os.Open(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			_, err = ValidateTranscript(f, tc.options)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestRequestValidationRejectsSchemaEdges(t *testing.T) {
	base := Request{ProtocolVersion: ProtocolV1Alpha2, Type: "request", RunID: "r", NodeID: "n", Attempt: 1, Model: Model{Provider: "p", ID: "m"}, Session: SessionRequest{Mode: "fresh"}}
	cases := []struct {
		name   string
		mutate func(*Request)
	}{
		{"protocol", func(v *Request) { v.ProtocolVersion = "bad" }},
		{"type", func(v *Request) { v.Type = "bad" }},
		{"node", func(v *Request) { v.NodeID = " " }},
		{"model", func(v *Request) { v.Model.ID = "" }},
		{"session-mode", func(v *Request) { v.Session.Mode = "other" }},
		{"resume-id", func(v *Request) { v.Session = SessionRequest{Mode: "resume"} }},
		{"timeout", func(v *Request) { v.Limits.TimeoutMS = -1 }},
		{"output-limit", func(v *Request) { v.Limits.MaxOutputBytes = -1 }},
		{"env-key", func(v *Request) { v.Environment = map[string]string{" ": "x"} }},
		{"metadata-key", func(v *Request) { v.Metadata = map[string]string{"": "x"} }},
		{"duplicate-policy", func(v *Request) { v.Policy = &Policy{AllowedTools: []string{"read", "read"}} }},
		{"filesystem", func(v *Request) { v.Policy = &Policy{Filesystem: "write"} }},
		{"network", func(v *Request) { v.Policy = &Policy{Network: "allow"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := base
			tc.mutate(&value)
			if err := ValidateRequest(value); err == nil {
				t.Fatalf("expected validation error: %#v", value)
			}
		})
	}
}

func TestResultAndDecisionValidationEdges(t *testing.T) {
	zero, one := 0, 1
	badResults := []Result{
		{ProtocolVersion: "bad", Type: "result", Status: "completed", ExitCode: &zero},
		{ProtocolVersion: ProtocolV1Alpha2, Type: "bad", Status: "completed", ExitCode: &zero},
		{ProtocolVersion: ProtocolV1Alpha2, Type: "result", Status: "other", ExitCode: &zero},
		{ProtocolVersion: ProtocolV1Alpha2, Type: "result", Status: "completed"},
		{ProtocolVersion: ProtocolV1Alpha2, Type: "result", Status: "completed", ExitCode: &one},
		{ProtocolVersion: ProtocolV1Alpha2, Type: "result", Status: "failed", ExitCode: &zero},
		{ProtocolVersion: ProtocolV1Alpha2, Type: "result", Status: "completed", ExitCode: &zero, Usage: &Usage{InputTokens: -1}},
	}
	for i, value := range badResults {
		if err := ValidateResult(value, ""); err == nil {
			t.Fatalf("case %d unexpectedly valid", i)
		}
	}
	resume := Result{ProtocolVersion: ProtocolV1Alpha2, Type: "result", Status: "completed", ExitCode: &zero, Session: &SessionResult{ID: "wrong", Resumed: true}}
	if err := ValidateResult(resume, "wanted"); err == nil {
		t.Fatal("expected resume identity rejection")
	}
	if err := ValidateToolDecision(ToolDecisionMessage{ProtocolVersion: ProtocolV1Alpha2, Type: "tool.decision", CallID: "c", Decision: ToolDecision{Decision: "allow"}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateToolDecision(ToolDecisionMessage{ProtocolVersion: ProtocolV1Alpha2, Type: "tool.decision", CallID: "c", Decision: ToolDecision{Decision: "maybe"}}); err == nil {
		t.Fatal("expected invalid decision")
	}
}

func TestTranscriptRejectsStructuralFailures(t *testing.T) {
	zero := 0
	_ = zero
	cases := []struct {
		name, stream string
		options      Options
	}{
		{"missing-terminal", `{"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2"}}`, Options{}},
		{"missing-declaration", `{"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"completed","exit_code":0}}`, Options{RequireDeclaration: true}},
		{"duplicate-declaration", `{"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2"}}\n{"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2"}}\n{"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"completed","exit_code":0}}`, Options{}},
		{"empty-event", `{"protocol_version":"takt-assistant/v1alpha2","type":"event","event":null}\n{"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"completed","exit_code":0}}`, Options{}},
		{"unknown-type", `{"protocol_version":"takt-assistant/v1alpha2","type":"mystery"}`, Options{}},
		{"wrong-protocol", `{"protocol_version":"bad","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"completed","exit_code":0}}`, Options{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateTranscript(strings.NewReader(tc.stream), tc.options); err == nil {
				t.Fatal("expected transcript rejection")
			}
		})
	}
}
