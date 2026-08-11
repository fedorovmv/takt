package runtime

import "testing"

func TestMatchSignalContract(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		want       string
		diagnostic SignalDiagnostic
	}{
		{name: "promise", output: "done\n<promise>BUILD-CLEAN</promise>", want: "BUILD-CLEAN"},
		{name: "final line", output: "review\nBUILD-CLEAN\n", want: "BUILD-CLEAN"},
		{name: "prose missing", output: "BUILD-CLEAN, but more work", diagnostic: SignalMissing},
		{name: "fenced", output: "```\n<promise>BUILD-CLEAN</promise>\n```", diagnostic: SignalMissing},
		{name: "ambiguous", output: "<promise>BUILD-CLEAN</promise>\nBUILD-CLEAN", diagnostic: SignalAmbiguous},
		{name: "other promise", output: "<promise>OTHER</promise>\n", diagnostic: SignalAmbiguous},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MatchSignal(test.output, "BUILD-CLEAN")
			if test.want != "" {
				if got.MatchedSignal != test.want || got.Diagnostic != nil {
					t.Fatalf("MatchSignal() = %#v", got)
				}
				return
			}
			if got.Diagnostic == nil || *got.Diagnostic != test.diagnostic {
				t.Fatalf("MatchSignal() = %#v, want %s", got, test.diagnostic)
			}
		})
	}
}
