package yamlcodec

import "testing"

type blockDoc struct {
	Prompt string `json:"prompt"`
	Folded string `json:"folded"`
	Strip  string `json:"strip"`
	Keep   string `json:"keep"`
}

func TestUnmarshalBasic(t *testing.T) {
	var got struct {
		Name  string `json:"name"`
		Items []any  `json:"items"`
	}
	err := Unmarshal([]byte("name: demo\nitems: [one, 2, true]\n"), &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || len(got.Items) != 3 {
		t.Fatalf("unexpected value: %+v", got)
	}
}

func TestBlockScalarPreservesBlankLinesAndSpecialText(t *testing.T) {
	src := `prompt: |
  first

  second: value # literal comment
  ${feedback}
folded: >
  first
  second

  third
strip: |-
  a

keep: |+
  b

`
	var got blockDoc
	if err := Unmarshal([]byte(src), &got); err != nil {
		t.Fatal(err)
	}
	wantPrompt := "first\n\nsecond: value # literal comment\n${feedback}\n"
	if got.Prompt != wantPrompt {
		t.Fatalf("prompt mismatch\nwant: %q\n got: %q", wantPrompt, got.Prompt)
	}
	wantFolded := "first second\n\nthird\n"
	if got.Folded != wantFolded {
		t.Fatalf("folded mismatch\nwant: %q\n got: %q", wantFolded, got.Folded)
	}
	if got.Strip != "a" {
		t.Fatalf("strip mismatch: %q", got.Strip)
	}
	if got.Keep != "b\n\n\n" {
		t.Fatalf("keep mismatch: %q", got.Keep)
	}
}

func TestUnknownFieldsRemainStrict(t *testing.T) {
	var got struct {
		Known string `json:"known"`
	}
	if err := Unmarshal([]byte("known: yes\nunknown: no\n"), &got); err == nil {
		t.Fatal("expected unknown-field error")
	}
}

func TestUnknownFieldSuggestsNearestKnownField(t *testing.T) {
	var got struct {
		IdleTimeout string `json:"idle_timeout"`
	}
	err := Unmarshal([]byte("idle_timout: 10s\n"), &got)
	if err == nil || err.Error() != `unknown field "idle_timout" at $; did you mean "idle_timeout"?` {
		t.Fatalf("error = %v", err)
	}
}
