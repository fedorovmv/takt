package flowref_test

import (
	"reflect"
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

func TestReferenceLexerAcceptsWholeTypedArtifact(t *testing.T) {
	ref, err := flowref.Parse("$evidence.artifacts.evaluation-evidence", flowref.NonShell)
	if err != nil {
		t.Fatal(err)
	}
	if ref.NodeID != "evidence" || len(ref.Path) != 2 || ref.Path[0] != "artifacts" || ref.Path[1] != "evaluation-evidence" {
		t.Fatalf("reference = %#v", ref)
	}
}

func TestReferenceLexerKeepsDottedArtifactTypeCanonical(t *testing.T) {
	metadata, err := flowref.Parse("$evidence.artifacts.report.json.path", flowref.NonShell)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(metadata.Path, "/"); got != "artifacts/report.json/path" {
		t.Fatalf("metadata path = %q", got)
	}
	whole, err := flowref.Parse("$evidence.artifacts.report.json", flowref.NonShell)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(whole.Path, "/"); got != "artifacts/report.json" {
		t.Fatalf("whole artifact path = %q", got)
	}
}

func TestReferenceLexerAcceptsMatrixTemplateReferences(t *testing.T) {
	cases := []struct {
		source string
		name   string
		path   []string
	}{
		{source: "$MATRIX.index", name: "index"},
		{source: "$MATRIX.total", name: "total"},
		{source: "$MATRIX.item", name: "item"},
		{source: "$MATRIX.item.case_id", name: "item", path: []string{"case_id"}},
	}
	for _, tc := range cases {
		ref, err := flowref.Parse(tc.source, flowref.NonShell)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.source, err)
		}
		if ref.Kind != flowref.KindMatrix || ref.Name != tc.name || !reflect.DeepEqual(ref.Path, tc.path) {
			t.Fatalf("Parse(%q) = %#v", tc.source, ref)
		}
	}
}

func TestReferenceLexerRejectsInvalidMatrixReferences(t *testing.T) {
	for _, source := range []string{"$MATRIX", "$MATRIX.unknown", "$MATRIX.index.value", "$MATRIX.item.bad/path"} {
		if _, err := flowref.Parse(source, flowref.NonShell); err == nil {
			t.Fatalf("Parse(%q) succeeded", source)
		}
	}
	if _, err := flowref.Parse("$MATRIX.item", flowref.When); err == nil {
		t.Fatal("matrix reference was accepted directly in when")
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

func TestShellSurfaceDoesNotTreatSingleQuotedCommandSyntaxAsSubstitution(t *testing.T) {
	literal := "x='$('"
	if got, err := flowref.Render(literal, flowref.Shell, nil); err != nil || got != literal {
		t.Fatalf("single-quoted shell literal = %q, err=%v", got, err)
	}

	source := "x='$('\nprintf \"%s\" \"$build.output\""
	if _, err := flowref.Render(source, flowref.Shell, func(flowref.Reference) (string, bool) {
		return "$(touch pwned)", true
	}); err == nil {
		t.Fatal("reference after single-quoted $(: literal was accepted as safe")
	}
}

func TestShellSurfaceTracksCasePatternParentheses(t *testing.T) {
	literal := `echo "$(case x in
  x) printf ok ;;
esac)"`
	if got, err := flowref.Render(literal, flowref.Shell, nil); err != nil || got != literal {
		t.Fatalf("case shell syntax changed: got=%q err=%v", got, err)
	}

	source := `echo "$(case x in
  x) printf "%s" "$build.output" ;;
esac)"`
	if _, err := flowref.Render(source, flowref.Shell, func(flowref.Reference) (string, bool) {
		return "$(touch pwned)", true
	}); err == nil {
		t.Fatal("reference inside case command substitution was accepted as safe")
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
