// Package tasksource defines the public protocol for resolving external task
// references into a provider-neutral Task before Takt routing begins.
package tasksource

import (
	"fmt"
	"sort"
	"strings"
)

const ProtocolV1Alpha1 = "takt-task-source/v1alpha1"

type ResolveRequest struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Reference  string `json:"reference"`
	Workspace  string `json:"workspace,omitempty"`
}

type Source struct {
	Adapter   string `json:"adapter"`
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	Revision  string `json:"revision"`
	URL       string `json:"url,omitempty"`
}

type Task struct {
	APIVersion  string   `json:"apiVersion"`
	Kind        string   `json:"kind"`
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Goal        string   `json:"goal"`
	Description string   `json:"description,omitempty"`
	Acceptance  []string `json:"acceptance,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	References  []string `json:"references,omitempty"`
	Source      Source   `json:"source"`
}

type ResolveResponse struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Task       *Task  `json:"task,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Error      string `json:"error,omitempty"`
}

func ValidateResolveRequest(v ResolveRequest) error {
	if v.APIVersion != ProtocolV1Alpha1 || v.Kind != "ResolveRequest" {
		return fmt.Errorf("task source request must use %s ResolveRequest", ProtocolV1Alpha1)
	}
	if strings.TrimSpace(v.Reference) == "" {
		return fmt.Errorf("task source reference is required")
	}
	if v.Workspace != "" && strings.TrimSpace(v.Workspace) == "" {
		return fmt.Errorf("task source workspace cannot be blank")
	}
	return nil
}

func NormalizeTask(v *Task) {
	if v.APIVersion == "" {
		v.APIVersion = ProtocolV1Alpha1
	}
	if v.Kind == "" {
		v.Kind = "Task"
	}
	v.ID = strings.TrimSpace(v.ID)
	v.Title = strings.TrimSpace(v.Title)
	v.Goal = strings.TrimSpace(v.Goal)
	v.Description = strings.TrimSpace(v.Description)
	v.Source.Adapter = strings.TrimSpace(v.Source.Adapter)
	v.Source.Kind = strings.TrimSpace(v.Source.Kind)
	v.Source.Reference = strings.TrimSpace(v.Source.Reference)
	v.Source.Revision = strings.TrimSpace(v.Source.Revision)
	v.Source.URL = strings.TrimSpace(v.Source.URL)
	v.Acceptance = clean(v.Acceptance)
	v.Labels = clean(v.Labels)
	v.References = clean(v.References)
	sort.Strings(v.Labels)
}

func ValidateTask(v Task) error {
	NormalizeTask(&v)
	if v.APIVersion != ProtocolV1Alpha1 || v.Kind != "Task" {
		return fmt.Errorf("normalized task must use %s Task", ProtocolV1Alpha1)
	}
	if v.ID == "" || v.Title == "" || v.Goal == "" {
		return fmt.Errorf("normalized task requires id, title, and goal")
	}
	if v.Source.Adapter == "" || v.Source.Kind == "" || v.Source.Reference == "" || v.Source.Revision == "" {
		return fmt.Errorf("normalized task source requires adapter, kind, reference, and revision")
	}
	return nil
}

func ValidateResolveResponse(v ResolveResponse) error {
	if v.APIVersion != ProtocolV1Alpha1 || v.Kind != "ResolveResponse" {
		return fmt.Errorf("task source response must use %s ResolveResponse", ProtocolV1Alpha1)
	}
	if v.Error != "" || v.ErrorCode != "" {
		if v.Task != nil {
			return fmt.Errorf("failed task source response cannot contain task")
		}
		if strings.TrimSpace(v.Error) == "" {
			return fmt.Errorf("task source error response requires error")
		}
		return nil
	}
	if v.Task == nil {
		return fmt.Errorf("successful task source response requires task")
	}
	return ValidateTask(*v.Task)
}

func GoalText(v Task) string {
	NormalizeTask(&v)
	var b strings.Builder
	b.WriteString(v.Goal)
	if v.Description != "" && v.Description != v.Goal {
		b.WriteString("\n\nDescription:\n")
		b.WriteString(v.Description)
	}
	if len(v.Acceptance) > 0 {
		b.WriteString("\n\nAcceptance criteria:\n")
		for _, x := range v.Acceptance {
			b.WriteString("- ")
			b.WriteString(x)
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n\nSource: ")
	b.WriteString(v.Source.Kind)
	b.WriteString(" ")
	b.WriteString(v.Source.Reference)
	b.WriteString(" @ ")
	b.WriteString(v.Source.Revision)
	return strings.TrimSpace(b.String())
}

func clean(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, x := range in {
		x = strings.TrimSpace(x)
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
