package evaluation

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func makeFlowCaseTree(t *testing.T, ids []string) string {
	t.Helper()
	root := t.TempDir()
	for _, id := range ids {
		d := filepath.Join(root, "cases", id)
		if err := os.MkdirAll(filepath.Join(d, "workspace"), 0755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(d, "input.md"), []byte("in"), 0644)
		os.WriteFile(filepath.Join(d, "expected.yaml"), []byte("oracle: {}"), 0644)
		os.WriteFile(filepath.Join(d, "workspace", "x"), []byte("x"), 0644)
	}
	return root
}
func TestDiscoverFlowCasesIsOrderedAndSelfContained(t *testing.T) {
	root := makeFlowCaseTree(t, []string{"z-last", "a-first"})
	cases, err := DiscoverFlowCases(filepath.Join(root, "suite.yaml"), &FlowSuite{Cases: FlowCasesSpec{Directory: "cases"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{cases[0].ID, cases[1].ID}; !reflect.DeepEqual(got, []string{"a-first", "z-last"}) {
		t.Fatalf("order=%v", got)
	}
}
func TestDiscoverFlowCasesContainment(t *testing.T) {
	root := makeFlowCaseTree(t, []string{"ok"})
	os.WriteFile(filepath.Join(root, "cases", "ok", "workspace", "link"), []byte("x"), 0644)
	_, err := DiscoverFlowCases(filepath.Join(root, "suite.yaml"), &FlowSuite{Cases: FlowCasesSpec{Directory: "cases"}}, "ok")
	if err != nil {
		t.Fatal(err)
	}
}
