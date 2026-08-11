package flowref_test

import (
	"strings"
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

func TestShellSurfaceRejectsTrailingDollarWithoutPanicking(t *testing.T) {
	if _, err := flowref.Render("$", flowref.Shell, nil); err == nil {
		t.Fatal("trailing dollar accepted")
	}
}

func TestShellSurfaceRejectsReferencesAnywhereInDoubleQuotes(t *testing.T) {
	for _, source := range []string{
		`echo "$build.output"`,
		`echo "prefix=$build.output"`,
		`echo "$build.output:suffix"`,
		`value="before $build.output after"`,
	} {
		t.Run(source, func(t *testing.T) {
			_, err := flowref.Render(source, flowref.Shell, func(flowref.Reference) (string, bool) { return "value", true })
			if err == nil {
				t.Fatalf("double-quoted Takt reference accepted: %s", source)
			}
		})
	}
}

func TestShellSurfaceTracksCommentsHeredocsAndNestedSubstitutions(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name:   "comment quote cannot desynchronize later command",
			source: "# \"\nprintf \"%s\" \"$build.output\"",
		},
		{
			name:   "nested command substitution keeps its own quotes",
			source: `printf '%s' "$(printf '%s' "$build.output")"`,
		},
		{
			name:   "backtick substitution keeps its own quotes",
			source: "printf '%s' \"`printf '%s' \"$build.output\"`\"",
		},
		{
			name:   "heredoc expansion is not a safe substitution surface",
			source: "cat <<EOF\n$build.output\nEOF",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := flowref.Render(tc.source, flowref.Shell, func(flowref.Reference) (string, bool) {
				return "$(touch pwned)", true
			})
			if err == nil {
				t.Fatalf("unsafe shell reference accepted: %s", tc.source)
			}
		})
	}
}

func TestShellSurfacePreservesNativeVariablesAndResolvesBareTaktVariables(t *testing.T) {
	seen := map[string]int{}
	got, err := flowref.Render(`printf '%s %s %s %s %s %s' "$PATH" "${PATH}" "$?" "$((1 + 1))" "$(true)" $BASE_BRANCH`, flowref.Shell, func(ref flowref.Reference) (string, bool) {
		seen[ref.Name]++
		return "main", true
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `printf '%s %s %s %s %s %s' "$PATH" "${PATH}" "$?" "$((1 + 1))" "$(true)" $BASE_BRANCH` {
		t.Fatalf("shell render = %q", got)
	}
	if seen["BASE_BRANCH"] != 1 {
		t.Fatalf("bare BASE_BRANCH did not resolve: %#v", seen)
	}
}

func TestShellSurfaceEscapesReferenceInsideSingleQuotes(t *testing.T) {
	got, err := flowref.Render(`printf '%s' '$build.output'`, flowref.Shell, func(flowref.Reference) (string, bool) {
		return "$(touch pwned)'", true
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "$(touch pwned)") || !strings.HasSuffix(got, "'\"'\"''") {
		t.Fatalf("single-quoted render = %q", got)
	}
}
