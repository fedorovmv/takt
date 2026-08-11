package flowref_test

import (
	"testing"

	"takt/internal/flowref"
)

func TestReferenceLexerAcceptsArchonForms(t *testing.T) {
	cases := []string{
		"$ARGUMENTS",
		"$INPUTS.item.name",
		"$build-node.output.status",
		"$validate.artifacts.report.json.path",
		"$LOOP_PREV.review.output:-empty",
		"$confirm.output",
		"$build.output.items.0.name?",
	}
	for _, source := range cases {
		t.Run(source, func(t *testing.T) {
			if _, err := flowref.Parse(source, flowref.NonShell); err != nil {
				t.Fatalf("Parse(%q): %v", source, err)
			}
		})
	}
}

func TestReferenceLexerRejectsLegacyAndReservedForms(t *testing.T) {
	cases := []string{
		"${nodes.build.output}",
		"${input}",
		"$USER_MESSAGE",
		"$ARTIFACTS_DIRX",
		"$INPUTS",
		"$build.artifacts.123.path",
		"$ARGUMENTS.output",
	}
	for _, source := range cases {
		t.Run(source, func(t *testing.T) {
			if _, err := flowref.Parse(source, flowref.NonShell); err == nil {
				t.Fatalf("Parse(%q) succeeded; target language must reject it", source)
			}
		})
	}
}

func TestReferenceLexerTreatsDoubleDollarAsNonShellLiteral(t *testing.T) {
	got, err := flowref.Render("cost: $$5", flowref.NonShell, func(flowref.Reference) (string, bool) {
		t.Fatal("literal escape invoked resolver")
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "cost: $5" {
		t.Fatalf("Render() = %q, want %q", got, "cost: $5")
	}
}

func TestReferenceLexerStopsAtPunctuationAfterReference(t *testing.T) {
	refs, err := flowref.Scan("$FANOUT.item.name:$FANOUT.index/$FANOUT.total", flowref.NonShell)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 || refs[0].Name != "item" || len(refs[0].Path) != 1 || refs[0].Path[0] != "name" {
		t.Fatalf("references = %#v", refs)
	}
}

func TestShellSurfacePreservesPositionalParameters(t *testing.T) {
	got, err := flowref.Render(`printf '%s' "$1"`, flowref.Shell, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != `printf '%s' "$1"` {
		t.Fatalf("shell render = %q", got)
	}
}
