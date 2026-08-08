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

func TestArchitectureBoundaries(t *testing.T) {
	root := repoRoot(t)

	if _, err := os.Stat(filepath.Join(root, "internal", "control")); !os.IsNotExist(err) {
		t.Fatalf("legacy internal/control must stay removed")
	}

	cmdImports := packageImports(t, root, "cmd/takt")
	allowed := map[string]bool{"fmt": true, "os": true, "takt/internal/cli": true}
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
		"takt/internal/daemon", "takt/internal/mcp", "takt/internal/version")
	if strings.Contains(productionSource(t, root, "internal/application"), "store.FS{") {
		t.Errorf("application must depend on RunStore ports; concrete store.FS belongs in bootstrap/infrastructure")
	}
	requireRunnerFieldsPrivate(t, root)

	forbidImports(t, root, "internal/application",
		"takt/internal/cli", "takt/internal/mcp", "takt/internal/daemon", "takt/internal/appapi", "takt/internal/bootstrap")
	forbidImports(t, root, "internal/appapi",
		"takt/internal/cli", "takt/internal/mcp", "takt/internal/daemon", "takt/internal/bootstrap", "takt/internal/runtime")
	forbidImports(t, root, "internal/mcp",
		"takt/internal/cli", "takt/internal/daemon", "takt/internal/runtime", "takt/internal/evaluation", "takt/internal/notification")
	forbidImports(t, root, "internal/daemon",
		"takt/internal/cli", "takt/internal/runtime", "takt/internal/evaluation", "takt/internal/notification")
}
