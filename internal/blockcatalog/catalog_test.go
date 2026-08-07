package blockcatalog_test

import (
	"os"
	"path/filepath"
	"strings"
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
	if len(catalog.Blocks) != 11 || catalog.Fingerprint == "" {
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
	if len(catalog.Blocks) != 14 {
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
    prompt: return summary
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
  - id: result
    depends_on: [child]
    prompt: summarize
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
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
    output_paths: [summary]
`
	for name, content := range map[string]string{"child.yaml": child, "parent.yaml": parent, "package.yaml": pkg} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := blockcatalog.LoadOne(filepath.Join(root, "package.yaml")); err == nil || !strings.Contains(err.Error(), "starts governed child Runs") {
		t.Fatalf("expected governed child Run rejection, got %v", err)
	}
}

func TestPackageFingerprintIncludesResolvedCommandContent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "commands"), 0o700); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(root, "block.yaml")
	packagePath := filepath.Join(root, "package.yaml")
	commandPath := filepath.Join(root, "commands", "dynamic-implement.md")
	mustWriteCatalog(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: command-block
nodes:
  - id: result
    command: dynamic-implement
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
`)
	mustWriteCatalog(t, packagePath, `apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: command-package
  version: 1.0.0
  scope: project
blocks:
  implement:
    workflow: block.yaml
    output_paths: [summary]
`)
	mustWriteCatalog(t, commandPath, "first implementation command")
	before, err := blockcatalog.LoadOne(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCatalog(t, commandPath, "second implementation command")
	after, err := blockcatalog.LoadOne(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	if before.Fingerprint == after.Fingerprint {
		t.Fatal("command content did not change package fingerprint")
	}
}

func TestPackageFingerprintIncludesNestedWorkflowScriptDependency(t *testing.T) {
	root := t.TempDir()
	mustWriteCatalog(t, filepath.Join(root, "tool.sh"), "#!/bin/sh\necho ok\n")
	mustWriteCatalog(t, filepath.Join(root, "dependency.txt"), "first")
	mustWriteCatalog(t, filepath.Join(root, "child.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: result
    script:
      runtime: command
      path: tool.sh
      dependencies: [dependency.txt]
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
`)
	mustWriteCatalog(t, filepath.Join(root, "block.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
nodes:
  - id: child
    subworkflow:
      path: child.yaml
      output_node: result
  - id: result
    depends_on: [child]
    prompt: summarize nested result
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
`)
	packagePath := filepath.Join(root, "package.yaml")
	mustWriteCatalog(t, packagePath, `apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: nested-package
  version: 1.0.0
  scope: project
blocks:
  inspect:
    workflow: block.yaml
    output_paths: [summary]
`)
	before, err := blockcatalog.LoadOne(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCatalog(t, filepath.Join(root, "dependency.txt"), "second")
	after, err := blockcatalog.LoadOne(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	if before.Fingerprint == after.Fingerprint {
		t.Fatal("nested script dependency did not change package fingerprint")
	}
}

func TestExplicitEmptyAllowedIntegrationsDeniesAll(t *testing.T) {
	root := t.TempDir()
	mustWriteCatalog(t, filepath.Join(root, "block.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: block
nodes:
  - id: result
    prompt: return ok
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
`)
	path := filepath.Join(root, "package.yaml")
	mustWriteCatalog(t, path, `apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: deny-integrations
  version: 1.0.0
  scope: project
blocks:
  inspect:
    workflow: block.yaml
    integrations: [filesystem]
    output_paths: [summary]
governance:
  allowed_integrations: []
`)
	if _, err := blockcatalog.LoadOne(path); err == nil || !strings.Contains(err.Error(), "outside governance.allowed_integrations") {
		t.Fatalf("expected explicit empty integration denial, got %v", err)
	}
}

func mustWriteCatalog(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPackageScopePrecedenceAndGovernanceRemainFailClosed(t *testing.T) {
	root := t.TempDir()
	makePackage := func(scope, marker, denied string) string {
		t.Helper()
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		mustWriteCatalog(t, filepath.Join(dir, "block.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: shared
nodes:
  - id: result
    prompt: return `+marker+`
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
`)
		pkg := `apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: ` + scope + `
  version: 1.0.0
  scope: ` + scope + `
blocks:
  shared:
    workflow: block.yaml
    output_paths: [summary]
governance:
  policy:
    denied_tools: [` + denied + `]
`
		path := filepath.Join(dir, "package.yaml")
		mustWriteCatalog(t, path, pkg)
		return path
	}
	global := makePackage("global", "global", "global-deny")
	corporate := makePackage("corporate", "corporate", "corporate-deny")
	project := makePackage("project", "project", "project-deny")
	catalog, err := blockcatalog.Load([]string{project, global, corporate})
	if err != nil {
		t.Fatal(err)
	}
	block, ok := catalog.Block("shared")
	if !ok || !strings.Contains(block.WorkflowPath, string(filepath.Separator)+"project"+string(filepath.Separator)) {
		t.Fatalf("project block did not win precedence: %#v", block)
	}
	for _, want := range []string{"global-deny", "corporate-deny", "project-deny"} {
		found := false
		for _, got := range catalog.Governance.Policy.DeniedTools {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("governance lost %q: %#v", want, catalog.Governance)
		}
	}
}

func TestPackageAdapterRequirementsAreValidated(t *testing.T) {
	root := t.TempDir()
	mustWriteCatalog(t, filepath.Join(root, "block.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: block
nodes:
  - id: result
    prompt: return ok
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
`)
	path := filepath.Join(root, "package.yaml")
	mustWriteCatalog(t, path, `apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: requirements
  version: 1.0.0
  scope: project
blocks:
  inspect:
    workflow: block.yaml
    output_paths: [summary]
requirements:
  takt: ">=0.1.40"
  adapters:
    - name: scm
      domain: scm
      operations: [change.create]
      reconcile: [change.create]
      level: required
`)
	pkg, err := blockcatalog.LoadOne(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Requirements.Adapters) != 1 {
		t.Fatalf("requirements=%#v", pkg.Requirements)
	}
	if got := pkg.Requirements.Adapters[0]; got.Name != "scm" || got.Domain != "scm" || len(got.Operations) != 1 || got.Level != "required" {
		t.Fatalf("requirements=%#v", pkg.Requirements)
	}
	mustWriteCatalog(t, path, strings.ReplaceAll(string(mustReadCatalogForTest(t, path)), "change.create", "Change.Create"))
	if _, err := blockcatalog.LoadOne(path); err == nil {
		t.Fatal("expected invalid adapter operation")
	}
}

func mustReadCatalogForTest(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
