package architecture

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func packageImports(t *testing.T, root, rel string) map[string]bool {
	t.Helper()
	imports := map[string]bool{}
	dir := filepath.Join(root, rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s/%s: %v", rel, entry.Name(), err)
		}
		for _, spec := range file.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			imports[value] = true
		}
	}
	return imports
}

func forbidImports(t *testing.T, root, rel string, forbidden ...string) {
	t.Helper()
	imports := packageImports(t, root, rel)
	for _, prefix := range forbidden {
		for imported := range imports {
			if imported == prefix || strings.HasPrefix(imported, prefix+"/") {
				t.Errorf("%s must not import %s", rel, imported)
			}
		}
	}
}

func productionLines(t *testing.T, root, rel string) int {
	t.Helper()
	total := 0
	entries, err := os.ReadDir(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		f, err := os.Open(filepath.Join(root, rel, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			total++
		}
		_ = f.Close()
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return total
}

func productionSource(t *testing.T, root, rel string) string {
	t.Helper()
	var out strings.Builder
	entries, err := os.ReadDir(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, rel, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out.Write(data)
		out.WriteByte('\n')
	}
	return out.String()
}

func productionSourceRecursive(t *testing.T, root, rel string, skipPrefixes ...string) string {
	t.Helper()
	base := filepath.Join(root, rel)
	var out strings.Builder
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		for _, prefix := range skipPrefixes {
			if relPath == prefix || strings.HasPrefix(relPath, strings.TrimSuffix(prefix, "/")+"/") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out.WriteString(relPath)
		out.WriteByte('\n')
		out.Write(data)
		out.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func requireOnlyInternalImports(t *testing.T, root, rel string, allowed ...string) {
	t.Helper()
	allow := map[string]bool{}
	for _, item := range allowed {
		allow[item] = true
	}
	for imported := range packageImports(t, root, rel) {
		if !strings.HasPrefix(imported, "takt/internal/") {
			continue
		}
		if !allow[imported] {
			t.Errorf("%s crosses the application boundary through %s", rel, imported)
		}
	}
}

func requireRunnerFieldsPrivate(t *testing.T, root string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, "internal", "runtime", "runner.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, item := range gen.Specs {
			typeSpec, ok := item.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "Runner" {
				continue
			}
			st, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				t.Fatal("Runner must remain a struct")
			}
			for _, field := range st.Fields.List {
				for _, name := range field.Names {
					if ast.IsExported(name.Name) {
						t.Errorf("runtime.Runner field %s must stay private; dependencies are constructor-injected", name.Name)
					}
				}
			}
			return
		}
	}
	t.Fatal("runtime.Runner declaration not found")
}

func requireServiceFieldsPrivate(t *testing.T, root, rel string) {
	t.Helper()
	fset := token.NewFileSet()
	dir := filepath.Join(root, rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, item := range gen.Specs {
				typeSpec, ok := item.(*ast.TypeSpec)
				if !ok || !strings.HasSuffix(typeSpec.Name.Name, "Service") || typeSpec.Name.Name == "Services" {
					continue
				}
				st, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						if ast.IsExported(name.Name) {
							t.Errorf("%s.%s must keep injected dependencies private", typeSpec.Name.Name, name.Name)
						}
					}
				}
			}
		}
	}
}

func requireAcyclicApplicationServices(t *testing.T, root string) {
	t.Helper()
	fset := token.NewFileSet()
	dir := filepath.Join(root, "internal", "application")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	graph := map[string][]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, item := range gen.Specs {
				typeSpec, ok := item.(*ast.TypeSpec)
				if !ok || !strings.HasSuffix(typeSpec.Name.Name, "Service") || typeSpec.Name.Name == "Services" {
					continue
				}
				st, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range st.Fields.List {
					star, ok := field.Type.(*ast.StarExpr)
					if !ok {
						continue
					}
					ident, ok := star.X.(*ast.Ident)
					if ok && strings.HasSuffix(ident.Name, "Service") {
						graph[typeSpec.Name.Name] = append(graph[typeSpec.Name.Name], ident.Name)
					}
				}
			}
		}
	}
	state := map[string]int{}
	stack := []string{}
	var visit func(string)
	visit = func(node string) {
		if state[node] == 2 {
			return
		}
		if state[node] == 1 {
			cycle := append(append([]string(nil), stack...), node)
			t.Errorf("application service dependency cycle: %s", strings.Join(cycle, " -> "))
			return
		}
		state[node] = 1
		stack = append(stack, node)
		for _, next := range graph[node] {
			visit(next)
		}
		stack = stack[:len(stack)-1]
		state[node] = 2
	}
	for node := range graph {
		visit(node)
	}
}

func requireExplicitApplicationBackground(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "internal", "application")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		n := strings.Count(string(data), "context.Background()")
		if n == 0 {
			continue
		}
		count += n
		if entry.Name() != "contexts.go" {
			t.Errorf("application foreground code must propagate caller context; %s creates context.Background directly", entry.Name())
		}
	}
	if count != 1 {
		t.Errorf("application must centralize request-independent durable context in contexts.go (Background count=%d)", count)
	}
}

func TestArchitectureBoundaries(t *testing.T) {
	root := repoRoot(t)

	if _, err := os.Stat(filepath.Join(root, "internal", "control")); !os.IsNotExist(err) {
		t.Fatalf("legacy internal/control must stay removed")
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "yamlmini")); !os.IsNotExist(err) {
		t.Fatalf("hand-written YAML parser must stay removed; YAML syntax belongs to the upstream module")
	}
	if matches, err := filepath.Glob(filepath.Join(root, "cmd", "takt-fake-*")); err != nil {
		t.Fatal(err)
	} else if len(matches) != 0 {
		t.Fatalf("test fixtures must not be product commands; move them under internal/testsupport/cmd: %v", matches)
	}
	for _, providerFile := range []string{"pi.go", "opencode.go"} {
		if _, err := os.Stat(filepath.Join(root, "internal", "assistant", providerFile)); !os.IsNotExist(err) {
			t.Fatalf("provider-specific assistant %s must stay outside stable assistant core", providerFile)
		}
	}

	cmdImports := packageImports(t, root, "cmd/takt")
	allowed := map[string]bool{"context": true, "fmt": true, "os": true, "os/signal": true, "syscall": true, "takt/internal/cli": true}
	for imported := range cmdImports {
		if !allowed[imported] {
			t.Errorf("cmd/takt is a launcher only; unexpected import %s", imported)
		}
	}
	if lines := productionLines(t, root, "cmd/takt"); lines > 80 {
		t.Errorf("cmd/takt production code grew to %d lines; application logic belongs outside cmd", lines)
	}

	requireOnlyInternalImports(t, root, "internal/cli",
		"takt/internal/apperror", "takt/internal/application", "takt/internal/bootstrap",
		"takt/internal/daemon", "takt/internal/mcp", "takt/internal/version",
		"takt/internal/experimental/dynamicflow", "takt/internal/experimental/learning",
		"takt/internal/extensions", "takt/internal/externalworker", "takt/internal/maintenance", "takt/internal/tooling")
	applicationSource := productionSource(t, root, "internal/application")
	if strings.Contains(applicationSource, "type ExternalService") {
		t.Error("external worker/tool lifecycle must stay in internal/externalworker, not regrow inside application")
	}
	for _, forbidden := range []string{"store.FS{", "dynamicplan.Store{", "hostcontrol.Store{", "notification.Dispatcher{", "learning.Manager{", "packagedist.New(", "type Context struct", "dynamicMu", "hostMu"} {
		if strings.Contains(applicationSource, forbidden) {
			t.Errorf("application must receive infrastructure through narrow ports; found %q", forbidden)
		}
	}
	evaluationSource := productionSource(t, root, "internal/tooling/evaluation")
	for _, forbidden := range []string{"runtime.New(", "store.FS{"} {
		if strings.Contains(evaluationSource, forbidden) {
			t.Errorf("evaluation must receive execution infrastructure from bootstrap; found %q", forbidden)
		}
	}
	runtimeSource := productionSource(t, root, "internal/runtime")
	for _, forbidden := range []string{"func New(wf *spec.Workflow", "func DefaultDependencies("} {
		if strings.Contains(runtimeSource, forbidden) {
			t.Errorf("runtime must not expose a hidden default composition path; found %q", forbidden)
		}
	}
	cliSource := productionSource(t, root, "internal/cli")
	if count := strings.Count(cliSource, "context.Background()"); count != 1 || !strings.Contains(cliSource, "func Run(args []string) error { return RunContext(context.Background(), args) }") {
		t.Errorf("CLI must propagate caller context; only the compatibility Run wrapper may create Background (count=%d)", count)
	}
	requireRunnerFieldsPrivate(t, root)
	requireServiceFieldsPrivate(t, root, "internal/application")
	requireServiceFieldsPrivate(t, root, "internal/externalworker")
	requireAcyclicApplicationServices(t, root)
	requireExplicitApplicationBackground(t, root)

	forbidImports(t, root, "internal/application",
		"takt/internal/cli", "takt/internal/mcp", "takt/internal/daemon", "takt/internal/appapi", "takt/internal/bootstrap",
		"takt/internal/experimental", "takt/internal/tooling", "takt/internal/extensions")
	for _, stable := range []string{"internal/application", "internal/assistant", "internal/externalworker", "internal/runcontrol", "internal/profile", "internal/runtime", "internal/workflow", "internal/store", "internal/config"} {
		forbidImports(t, root, stable, "takt/internal/experimental", "takt/internal/tooling", "takt/internal/extensions")
	}
	forbidImports(t, root, "internal/appapi",
		"takt/internal/cli", "takt/internal/mcp", "takt/internal/daemon", "takt/internal/bootstrap", "takt/internal/runtime")
	forbidImports(t, root, "internal/mcp",
		"takt/internal/cli", "takt/internal/daemon", "takt/internal/runtime", "takt/internal/tooling/evaluation", "takt/internal/extensions/notification")
	forbidImports(t, root, "internal/daemon",
		"takt/internal/cli", "takt/internal/runtime", "takt/internal/tooling/evaluation", "takt/internal/extensions/notification")

	// Architecture Contracts (ADR-090): provider extensions declare immutable
	// registrations, and only bootstrap assembles the production registry.
	productionInternal := productionSourceRecursive(t, root, "internal", "internal/bootstrap", "internal/testsupport")
	for _, forbidden := range []string{"assistant.MustRegistry(", "assistant.NewRegistry("} {
		if strings.Contains(productionInternal, forbidden) {
			t.Errorf("production assistant registry assembly belongs only in bootstrap; found %q outside it", forbidden)
		}
	}
	bootstrapSource := productionSource(t, root, "internal/bootstrap")
	if strings.Count(bootstrapSource, "assistant.MustRegistry(assistantproviders.Registrations()...)") != 1 {
		t.Error("bootstrap must assemble exactly one immutable assistant registry from extension registrations")
	}
	extensionAssistantSource := productionSource(t, root, "internal/extensions/assistants")
	if !strings.Contains(extensionAssistantSource, "func Registrations() []core.ProviderRegistration") {
		t.Error("assistant extensions must declare ProviderRegistration descriptors")
	}
	for _, forbidden := range []string{"func init()", "var registry", "registerProvider("} {
		if strings.Contains(extensionAssistantSource, forbidden) {
			t.Errorf("assistant extensions must not mutate global registration state; found %q", forbidden)
		}
	}

	// Workflow language constitution: when is one deliberately small governance
	// language owned by whenexpr. Runtime evaluates it; loader validates it.
	runtimeImports := packageImports(t, root, "internal/runtime")
	workflowImports := packageImports(t, root, "internal/workflow")
	if !runtimeImports["takt/internal/whenexpr"] || !workflowImports["takt/internal/whenexpr"] {
		t.Error("runtime and workflow validation must share the constitutional whenexpr implementation")
	}
	for _, forbidden := range []string{"splitLogicalExpression", "contains(", "regexp.MatchString(expr"} {
		if strings.Contains(runtimeSource, forbidden) {
			t.Errorf("runtime must not regrow a second or richer when-expression parser; found %q", forbidden)
		}
	}

	// Schema-first operations: appapi owns metadata/schema, validates it before
	// typed decode, and MCP projects canonical tools from those descriptors.
	appapiSource := productionSource(t, root, "internal/appapi")
	for _, required := range []string{"type OperationDescriptor struct", "InputSchema map[string]any", "func registerOperation[T any]", "validateOperationInput(schema, raw)", "func RenderOperationDocs() string"} {
		if !strings.Contains(appapiSource, required) {
			t.Errorf("schema-first application operation contract is incomplete; missing %q", required)
		}
	}
	mcpSource := productionSource(t, root, "internal/mcp")
	if !strings.Contains(mcpSource, "appapi.CanonicalOperations()") {
		t.Error("MCP canonical tools must be projected from appapi operation descriptors")
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "71-canonical-operation-contracts.generated.md")); err != nil {
		t.Errorf("generated canonical operation documentation is required: %v", err)
	}

	schemaImports := packageImports(t, root, "internal/schemasubset")
	if !schemaImports["github.com/santhosh-tekuri/jsonschema/v6"] {
		t.Error("schemasubset must delegate JSON Schema execution to the upstream Draft 2020-12 validator")
	}
	schemaSource := productionSource(t, root, "internal/schemasubset")
	for _, forbidden := range []string{"func validateValue(", "func uniquenessKey(", "func jsonNumberIsInteger("} {
		if strings.Contains(schemaSource, forbidden) {
			t.Errorf("schemasubset must own subset policy, not a second JSON Schema runtime; found %q", forbidden)
		}
	}
}

func TestShellSmokeBudget(t *testing.T) {
	root := repoRoot(t)
	matches, err := filepath.Glob(filepath.Join(root, "scripts", "test-*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || filepath.Base(matches[0]) != "test-host-integrations-typescript.sh" {
		t.Fatalf("shell is reserved for the cross-language TypeScript compiler smoke; got %v", matches)
	}
	source, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "python") || strings.Contains(string(source), "grep ") {
		t.Fatal("shell smoke must not grow its own assertion framework; product assertions belong in Go tests")
	}
}
