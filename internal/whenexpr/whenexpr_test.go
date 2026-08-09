package whenexpr

import "testing"

func TestConstitutionAllowsOnlySmallGateLanguage(t *testing.T) {
	valid := []string{
		`nodes.a.output == "ready"`,
		`nodes.a.status != completed && nodes.b.output == "ok"`,
		`nodes.a.output == "a || b" || inputs.message != "stop"`,
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
	}
	for _, value := range invalid {
		if err := Validate(value); err == nil {
			t.Errorf("Validate(%q) unexpectedly succeeded", value)
		}
	}
}

func TestEvaluateUsesAndBeforeOr(t *testing.T) {
	values := map[string]string{"nodes.a.output": "yes", "nodes.b.output": "no", "nodes.c.output": "yes"}
	got, err := Evaluate(`nodes.a.output == yes && nodes.b.output == yes || nodes.c.output == yes`, func(path string) (string, error) { return values[path], nil })
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("expected true")
	}
}
