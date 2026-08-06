package blockcatalog_test

import (
	"os"
	"path/filepath"
	"testing"

	"takt/internal/blockcatalog"
	"takt/internal/profile"
)

func TestBuiltinCodeCatalogLoadsTrustedBlocksAndGovernance(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	resolved, err := profile.Resolve("code", workspace)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := blockcatalog.Load(resolved.BlockPackagePaths)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Blocks) != 7 || catalog.Fingerprint == "" {
		t.Fatalf("unexpected catalog: %#v", catalog)
	}
	block, ok := catalog.Block("adversarial-verify")
	if !ok || filepath.Base(block.WorkflowPath) != "dynamic-adversarial-verify.yaml" {
		t.Fatalf("dedicated adversarial block missing: %#v", block)
	}
	if len(catalog.RequiredCapabilities([]string{"implement"})) == 0 {
		t.Fatal("capabilities were not exposed")
	}
}

func TestPackageRejectsWorkflowEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside.yaml")
	if err := os.WriteFile(outside, []byte("apiVersion: takt/v1alpha1\nkind: Workflow\nmetadata:\n  name: x\nnodes:\n  - id: x\n    bash: echo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(pkgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	pkg := `apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: escaped
  version: 1.0.0
  scope: project
blocks:
  escaped:
    workflow: ../outside.yaml
    output_paths: [items]
`
	path := filepath.Join(pkgDir, "package.yaml")
	if err := os.WriteFile(path, []byte(pkg), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := blockcatalog.LoadOne(path); err == nil {
		t.Fatal("expected package escape error")
	}
}

func TestCorporatePackageNarrowsCatalogAndAppliesGovernance(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	resolved, err := profile.Resolve("code", workspace)
	if err != nil {
		t.Fatal(err)
	}
	corporatePath, err := filepath.Abs(filepath.Join("..", "..", "examples", "corporate-block-package", "package.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := blockcatalog.Load(append(resolved.BlockPackagePaths, corporatePath))
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Blocks) != 10 {
		t.Fatalf("blocks=%d", len(catalog.Blocks))
	}
	if catalog.Governance.Limits.MaxChildRuns != 48 || catalog.Governance.BranchRules.Prefix != "feature/" {
		t.Fatalf("corporate governance not applied: %#v", catalog.Governance)
	}
	core, _ := catalog.Block("implement")
	foundDeny := false
	for _, tool := range core.Policy.DeniedTools {
		if tool == "network-unapproved" {
			foundDeny = true
		}
	}
	if !foundDeny {
		t.Fatalf("corporate security policy was not applied to core block: %#v", core.Policy)
	}
	if err := catalog.ValidateRequiredBlocks([]string{"discover"}); err == nil {
		t.Fatal("expected mandatory corporate validation block")
	}
}

func TestPackageRejectsBlockThatStartsGovernedChildRun(t *testing.T) {
	root := t.TempDir()
	child := `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: done
    bash: echo '{"summary":"ok"}'
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
`
	parent := `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
nodes:
  - id: child
    workflow:
      path: child.yaml
`
	pkg := `apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: non-atomic
  version: 1.0.0
  scope: project
blocks:
  nested:
    workflow: parent.yaml
`
	for name, content := range map[string]string{"child.yaml": child, "parent.yaml": parent, "package.yaml": pkg} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := blockcatalog.LoadOne(filepath.Join(root, "package.yaml")); err == nil {
		t.Fatal("expected governed child Run rejection")
	}
}
