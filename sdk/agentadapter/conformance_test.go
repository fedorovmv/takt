package agentadapter

import (
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
