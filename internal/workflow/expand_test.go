package workflow

import (
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
	writeTestFile(t, filepath.Join(childDir, "task.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: task
defaults:
  assistant: demo
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
assistant: child-assistant
model: child-model
---
printf '%s\n' '${inputs.value}' >> order.txt
`)
	writeTestFile(t, filepath.Join(dir, "workflow.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
nodes:
  - id: task
    subworkflow:
      path: flows/task.yaml
      inputs:
        value: ${input}
  - id: verify
    depends_on: [task]
    bash: |
      test "${nodes.task.output}" = "hello"
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
			if node.Command != "" || !strings.Contains(node.Prompt, "${input}") {
				t.Fatalf("local command was not inlined correctly: %+v", node)
			}
			if node.Assistant != "child-assistant" || node.Model != "child-model" {
				t.Fatalf("command frontmatter was not preserved: %+v", node)
			}
		}
		if node.ID == "verify" && !strings.Contains(node.Bash, "${nodes.task.output}") {
			t.Fatalf("public container reference changed: %q", node.Bash)
		}
	}
	if !writeNodeFound {
		t.Fatal("expanded command node not found")
	}
}

func TestLoadExpandsForeachSequentially(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "item.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: item
nodes:
  - id: append
    bash: |
      printf '%s\n' '${inputs.name}' >> order.txt
  - id: result
    depends_on: [append]
    bash: cat order.txt
`)
	writeTestFile(t, filepath.Join(dir, "workflow.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: batch
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
          name: ${service}
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
	writeTestFile(t, filepath.Join(dir, "a.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: a
nodes:
  - id: b
    subworkflow:
      path: b.yaml
`)
	writeTestFile(t, filepath.Join(dir, "b.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: b
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
	writeTestFile(t, filepath.Join(dir, "child.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: left
    bash: echo left
  - id: right
    bash: echo right
`)
	writeTestFile(t, filepath.Join(dir, "workflow.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
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
