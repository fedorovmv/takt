package tasksource

import "testing"

func TestTaskContract(t *testing.T) {
	task := Task{ID: "1", Title: "Fix", Goal: "Fix bug", Source: Source{Adapter: "gh", Kind: "github.issue", Reference: "o/r#1", Revision: "sha256:x"}}
	NormalizeTask(&task)
	if err := ValidateTask(task); err != nil {
		t.Fatal(err)
	}
	if got := GoalText(task); got == "" {
		t.Fatal("empty goal")
	}
	if err := ValidateTask(Task{}); err == nil {
		t.Fatal("expected invalid task")
	}
}
