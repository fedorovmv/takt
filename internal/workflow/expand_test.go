package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/spec"
)

func TestLoadExpandsSubworkflowAndPreservesPublicNode(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "flows")
	if err := os.MkdirAll(filepath.Join(childDir, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(childDir, "task.yaml"), `name: task
provider: demo
model: model
nodes:
  - id: write
    command: write-item
  - id: result
    depends_on: [write]
    bash: |
      cat order.txt
`)
	writeTestFile(t, filepath.Join(childDir, "commands", "write-item.md"), `---
provider: child-assistant
model: child-model
---
printf '%s\n' '$INPUTS.value' >> order.txt
`)
	writeTestFile(t, filepath.Join(dir, "workflow.yaml"), `name: parent
nodes:
  - id: task
    subworkflow:
      path: flows/task.yaml
      inputs:
        value: $ARGUMENTS
  - id: verify
    depends_on: [task]
    bash: |
      test $task.output = "hello"
`)

	wf, err := Load(filepath.Join(dir, "workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	ids := nodeIDs(wf.Nodes)
	for _, expected := range []string{"task__start", "task__write", "task__result", "task", "verify"} {
		if !contains(ids, expected) {
			t.Fatalf("expanded ids %v do not contain %s", ids, expected)
		}
	}
	var writeNodeFound bool
	for _, node := range wf.Nodes {
		if node.ID == "task__write" {
			writeNodeFound = true
			if node.Command != "" || !strings.Contains(node.Prompt, "$ARGUMENTS") {
				t.Fatalf("local command was not inlined correctly: %+v", node)
			}
			if node.Provider != "child-assistant" || node.Model != "child-model" {
				t.Fatalf("command frontmatter was not preserved: %+v", node)
			}
		}
		if node.ID == "verify" && !strings.Contains(node.Bash, "$task.output") {
			t.Fatalf("public container reference changed: %q", node.Bash)
		}
	}
	if !writeNodeFound {
		t.Fatal("expanded command node not found")
	}
}

func TestLoadExpandsForeachSequentially(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "item.yaml"), `name: item
nodes:
  - id: append
    bash: |
      printf '%s\n' '$INPUTS.name' >> order.txt
  - id: result
    depends_on: [append]
    bash: cat order.txt
`)
	writeTestFile(t, filepath.Join(dir, "workflow.yaml"), `name: batch
nodes:
  - id: batch
    foreach:
      as: service
      items:
        - alpha
        - beta
        - gamma
      subworkflow:
        path: item.yaml
        inputs:
          name: $INPUTS.service
  - id: verify
    depends_on: [batch]
    bash: |
      test "$(tr '\\n' ',' < order.txt)" = "alpha,beta,gamma,"
`)
	wf, err := Load(filepath.Join(dir, "workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string][]string{}
	for _, node := range wf.Nodes {
		byID[node.ID] = node.DependsOn
	}
	if !contains(byID["batch__002__append"], "batch__001") {
		t.Fatalf("second iteration is not sequential: %+v", byID["batch__002__append"])
	}
	if !contains(byID["batch__003__append"], "batch__002") {
		t.Fatalf("third iteration is not sequential: %+v", byID["batch__003__append"])
	}
	if !contains(byID["batch"], "batch__003") {
		t.Fatalf("public foreach node does not wait for final iteration: %+v", byID["batch"])
	}
}

func TestLoadRejectsRecursiveSubworkflow(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.yaml"), `name: a
nodes:
  - id: b
    subworkflow:
      path: b.yaml
`)
	writeTestFile(t, filepath.Join(dir, "b.yaml"), `name: b
nodes:
  - id: a
    subworkflow:
      path: a.yaml
`)
	_, err := Load(filepath.Join(dir, "a.yaml"))
	if err == nil || !strings.Contains(err.Error(), "recursive subworkflow") {
		t.Fatalf("expected recursion error, got %v", err)
	}
}

func TestLoadRequiresOutputNodeForMultipleTerminals(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "child.yaml"), `name: child
nodes:
  - id: left
    bash: echo left
  - id: right
    bash: echo right
`)
	writeTestFile(t, filepath.Join(dir, "workflow.yaml"), `name: parent
nodes:
  - id: child
    subworkflow:
      path: child.yaml
`)
	_, err := Load(filepath.Join(dir, "workflow.yaml"))
	if err == nil || !strings.Contains(err.Error(), "multiple terminal nodes") {
		t.Fatalf("expected output_node error, got %v", err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func nodeIDs(nodes []spec.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.ID)
	}
	return out
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestLoadExpandsForeachItemsFromFile(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "items.yaml"), `
- alpha
- beta
`)
	writeTestFile(t, filepath.Join(dir, "item.yaml"), `name: item
nodes:
  - id: result
    bash: printf '%s' '$INPUTS.name'
`)
	writeTestFile(t, filepath.Join(dir, "workflow.yaml"), `name: batch
nodes:
  - id: batch
    foreach:
      items_from:
        path: items.yaml
      subworkflow:
        path: item.yaml
        inputs:
          name: $INPUTS.item
`)
	wf, err := Load(filepath.Join(dir, "workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	ids := nodeIDs(wf.Nodes)
	for _, expected := range []string{"batch__001__result", "batch__002__result", "batch"} {
		if !contains(ids, expected) {
			t.Fatalf("expanded ids %v do not contain %s", ids, expected)
		}
	}
}

func TestLoadExpandsCompositionInsideLoopGroup(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "step.yaml"), `name: step
nodes:
  - id: result
    bash: echo done
`)
	writeTestFile(t, filepath.Join(dir, "workflow.yaml"), `name: parent
nodes:
  - id: retry
    loop_group:
      max_iterations: 2
      nodes:
        - id: child
          subworkflow:
            path: step.yaml
      until:
        node: child
        output_contains: done
`)
	wf, err := Load(filepath.Join(dir, "workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	loop := wf.Nodes[0].LoopGroup
	if loop == nil || loop.Until.Node != "retry__child" {
		t.Fatalf("unexpected expanded loop: %+v", loop)
	}
	ids := nodeIDs(loop.Nodes)
	for _, expected := range []string{"retry__child__start", "retry__child__result", "retry__child"} {
		if !contains(ids, expected) {
			t.Fatalf("loop ids %v do not contain %s", ids, expected)
		}
	}
}

func TestLoadRejectsExpansionBeyondDepthLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i <= maxExpansionDepth; i++ {
		next := ""
		if i < maxExpansionDepth {
			next = fmt.Sprintf("nodes:\n  - id: next\n    subworkflow:\n      path: %d.yaml\n", i+1)
		} else {
			next = "nodes:\n  - id: done\n    bash: echo done\n"
		}
		writeTestFile(t, filepath.Join(dir, fmt.Sprintf("%d.yaml", i)), fmt.Sprintf("name: depth-%d\n%s", i, next))
	}
	_, err := Load(filepath.Join(dir, "0.yaml"))
	if err == nil || !strings.Contains(err.Error(), "exceeds depth 16") {
		t.Fatalf("expected depth error, got %v", err)
	}
}

func TestLoadRejectsUnresolvedSubworkflowInput(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "child.yaml"), `name: child
nodes:
  - id: result
    bash: echo '$INPUTS.missing'
`)
	writeTestFile(t, filepath.Join(dir, "workflow.yaml"), `name: parent
nodes:
  - id: child
    subworkflow:
      path: child.yaml
`)
	_, err := Load(filepath.Join(dir, "workflow.yaml"))
	if err == nil || !strings.Contains(err.Error(), "unresolved subworkflow input $INPUTS.missing") {
		t.Fatalf("expected unresolved input error, got %v", err)
	}
}

func TestLoadFindsCommandsAtCompositionRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "commands", "shared.md"), "echo from-root\n")
	writeTestFile(t, filepath.Join(dir, "workflows", "child.yaml"), `name: child
nodes:
  - id: result
    command: shared
`)
	writeTestFile(t, filepath.Join(dir, "workflow.yaml"), `name: parent
nodes:
  - id: child
    subworkflow:
      path: workflows/child.yaml
`)
	wf, err := Load(filepath.Join(dir, "workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range wf.Nodes {
		if node.ID == "child__result" {
			if node.Command != "" || !strings.Contains(node.Prompt, "from-root") {
				t.Fatalf("root command was not inlined: %+v", node)
			}
			return
		}
	}
	t.Fatal("expanded result node not found")
}

func TestRewriteNodeRefsDoesNotChangeProse(t *testing.T) {
	value := "Explain nodes.build.output in prose and use $build.output as a template."
	rewritten := rewriteTemplateNodeRefs(value, "child__", map[string]spec.Node{"build": {ID: "build"}})
	if !strings.Contains(rewritten, "Explain nodes.build.output in prose") {
		t.Fatalf("prose reference was rewritten: %q", rewritten)
	}
	if !strings.Contains(rewritten, "$child__build.output") {
		t.Fatalf("template reference was not rewritten: %q", rewritten)
	}
}

func TestCompositionContainerProvidesChildExecutionDefaults(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "child.yaml"), `name: child
nodes:
  - id: implement
    prompt: do work
`)
	writeTestFile(t, filepath.Join(dir, "workflow.yaml"), `name: parent
provider: parent-assistant
model: parent-model
nodes:
  - id: child
    provider: invocation-assistant
    context: fresh
    subworkflow:
      path: child.yaml
`)
	wf, err := Load(filepath.Join(dir, "workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range wf.Nodes {
		if node.ID != "child__implement" {
			continue
		}
		if node.Provider != "invocation-assistant" || node.Model != "parent-model" || node.Context != "fresh" {
			t.Fatalf("container defaults were not inherited: %+v", node)
		}
		return
	}
	t.Fatal("expanded child node not found")
}

func TestValidateContainerFieldsMatchesValueSemantics(t *testing.T) {
	if err := validateContainerFields(spec.Node{
		ID:           "container",
		Attempts:     spec.AttemptsSpec{Max: 0},
		AllowFailure: false,
		Timeout:      "",
		Hooks:        spec.HookSet{},
	}); err != nil {
		t.Fatalf("zero-value container fields must be accepted: %v", err)
	}

	cases := []struct {
		name string
		node spec.Node
	}{
		{name: "attempts", node: spec.Node{Attempts: spec.AttemptsSpec{Max: 1}}},
		{name: "allow failure", node: spec.Node{AllowFailure: true}},
		{name: "timeout", node: spec.Node{Timeout: "1s"}},
		{name: "hooks", node: spec.Node{Hooks: spec.HookSet{BeforeNode: []spec.HookSpec{{Bash: "true"}}}}},
		{name: "native hooks", node: spec.Node{NativeHooks: []byte(`{}`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.node.ID = "container"
			if err := validateContainerFields(tc.node); err == nil {
				t.Fatalf("expected %s to be rejected", tc.name)
			}
		})
	}
}
