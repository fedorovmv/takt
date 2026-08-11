package whenexpr

import "testing"

func TestConstitutionAllowsOnlySmallGateLanguage(t *testing.T) {
	valid := []string{
		`$a.output == "ready"`,
		`$a.status != completed && $b.output == "ok"`,
		`$a.output == "a || b" || $INPUTS.message != "stop"`,
	}
	for _, value := range valid {
		if err := Validate(value); err != nil {
			t.Errorf("Validate(%q): %v", value, err)
		}
	}
	invalid := []string{
		`nodes.a.output > "1"`,
		`nodes.a.output == "x" && (nodes.b.output == "y")`,
		`contains(nodes.a.output, "x") == true`,
		`nodes.a.output + "x" == "yx"`,
		`nodes.a.output =~ "x"`,
		`inputs.input == "unterminated`,
		`inputs.input == 'unterminated`,
		`inputs.input == "mismatched'`,
	}
	for _, value := range invalid {
		if err := Validate(value); err == nil {
			t.Errorf("Validate(%q) unexpectedly succeeded", value)
		}
	}
}

func TestEvaluateUsesAndBeforeOr(t *testing.T) {
	values := map[string]string{"$a.output": "yes", "$b.output": "no", "$c.output": "yes"}
	got, err := Evaluate(`$a.output == yes && $b.output == yes || $c.output == yes`, func(path string) (string, error) { return values[path], nil })
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("expected true")
	}
}
