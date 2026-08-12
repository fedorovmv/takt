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
	if err := os.Symlink("x", filepath.Join(root, "cases", "ok", "workspace", "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverFlowCases(filepath.Join(root, "suite.yaml"), &FlowSuite{Cases: FlowCasesSpec{Directory: "cases"}}, "ok"); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestFlowCaseSCMAndFingerprintMode(t *testing.T) {
	root := makeFlowCaseTree(t, []string{"x"})
	cdir := filepath.Join(root, "cases", "x")
	os.Mkdir(filepath.Join(cdir, "scm"), 0755)
	os.WriteFile(filepath.Join(cdir, "scm", "a"), []byte("a"), 0644)
	cases, err := DiscoverFlowCases(filepath.Join(root, "suite.yaml"), &FlowSuite{Cases: FlowCasesSpec{Directory: "cases"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cases[0].SCMPath == "" {
		t.Fatal("missing scm")
	}
	before := cases[0].Fingerprint
	os.Chmod(filepath.Join(cdir, "workspace", "x"), 0755)
	after, err := FingerprintFlowCase(cases[0])
	if err != nil || before == after {
		t.Fatal("mode change not fingerprinted")
	}
}
func TestCopyFlowTreePreservesExistingMode(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	f := filepath.Join(src, "x")
	os.WriteFile(f, []byte("x"), 0644)
	os.WriteFile(filepath.Join(dst, "x"), []byte("old"), 0600)
	if err := CopyFlowTree(src, dst); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dst, "x"))
	if st.Mode().Perm() != 0644 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
}
