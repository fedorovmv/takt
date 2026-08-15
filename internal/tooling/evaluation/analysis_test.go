package evaluation

import "testing"

func TestSelectAnalysisCasesDefaultsAndRepeatRule(t *testing.T) {
	r := &SuiteReport{Runs: []RunRecord{{CaseID: "ok", Repeat: 1, Outcome: "true_accept"}, {CaseID: "false-accept", Repeat: 1, Outcome: "false_accept"}, {CaseID: "outage", Repeat: 1, Outcome: "infrastructure_error"}}}
	got, err := SelectAnalysisCases(r, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].CaseID != "false-accept" || got[1].CaseID != "outage" {
		t.Fatalf("selected=%v", got)
	}
	if _, err := SelectAnalysisCases(r, "", 1); err == nil {
		t.Fatal("repeat without case should fail")
	}
	got, err = SelectAnalysisCases(r, "ok", 0)
	if err != nil || len(got) != 1 || got[0].CaseID != "ok" {
		t.Fatalf("explicit case=%v err=%v", got, err)
	}
}
