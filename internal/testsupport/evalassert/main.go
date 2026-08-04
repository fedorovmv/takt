package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
)

type envelope struct {
	Result report `json:"result"`
}

type report struct {
	ReportVersion string      `json:"report_version"`
	TaktVersion   string      `json:"takt_version"`
	Strategy      strategy    `json:"strategy"`
	Benchmark     benchmark   `json:"benchmark"`
	Runs          []runRecord `json:"runs"`
	Summary       summary     `json:"summary"`
}

type strategy struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
	Workflow    string `json:"workflow_fingerprint"`
	Config      string `json:"config_fingerprint"`
	Commands    string `json:"commands_fingerprint"`
}

type benchmark struct {
	ID          string    `json:"id"`
	Fingerprint string    `json:"fingerprint"`
	Dataset     string    `json:"dataset_fingerprint"`
	Workspace   string    `json:"workspace_fingerprint"`
	CaseCount   int       `json:"case_count"`
	QualityNode string    `json:"quality_node"`
	Validator   validator `json:"validator"`
}

type validator struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

type summary struct {
	Total                  int            `json:"total"`
	ByStatus               map[string]int `json:"by_status"`
	InputTokens            int            `json:"input_tokens"`
	OutputTokens           int            `json:"output_tokens"`
	Cost                   float64        `json:"cost"`
	Answers                int            `json:"answers"`
	QualityRuns            int            `json:"quality_runs"`
	Valid                  int            `json:"valid"`
	ValidAtFirstAttempt    int            `json:"valid_at_first_attempt"`
	SuccessAt1             float64        `json:"success_at_1"`
	FinalSuccessRate       float64        `json:"final_success_rate"`
	AverageAttemptsToValid float64        `json:"average_attempts_to_valid"`
	AverageScore           float64        `json:"average_score"`
	ByAssistant            map[string]int `json:"by_assistant"`
	ByAssistantVersion     map[string]int `json:"by_assistant_version"`
	ByRequestedModel       map[string]int `json:"by_requested_model"`
	ByResolvedModel        map[string]int `json:"by_resolved_model"`
}

type runRecord struct {
	Status              string                `json:"status"`
	InputTokens         int                   `json:"input_tokens"`
	OutputTokens        int                   `json:"output_tokens"`
	Cost                float64               `json:"cost"`
	Answers             int                   `json:"answers"`
	Resumed             int                   `json:"resumed_nodes"`
	AttemptsToValid     int                   `json:"attempts_to_valid"`
	ValidAtFirstAttempt bool                  `json:"valid_at_first_attempt"`
	Quality             *quality              `json:"quality"`
	Nodes               map[string]nodeRecord `json:"nodes"`
}

type quality struct {
	ProtocolVersion string  `json:"protocol_version"`
	Valid           bool    `json:"valid"`
	Score           float64 `json:"score"`
}

type nodeRecord struct {
	Status           string    `json:"status"`
	Attempts         int       `json:"attempts"`
	Assistant        string    `json:"assistant"`
	AssistantVersion string    `json:"assistant_version"`
	RequestedModel   *modelRef `json:"requested_model"`
	ResolvedModel    *modelRef `json:"resolved_model"`
	Resumed          bool      `json:"resumed"`
	Feedback         string    `json:"feedback"`
	Error            string    `json:"error"`
	DiagnosticOutput string    `json:"diagnostic_output"`
	Usage            *usage    `json:"usage"`
}

type modelRef struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

type usage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

func main() {
	if len(os.Args) != 2 {
		fail("usage: evalassert <json-file>")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail("read report: %v", err)
	}
	var value envelope
	if err := json.Unmarshal(data, &value); err != nil {
		fail("decode report: %v", err)
	}
	r := value.Result
	if r.ReportVersion != "takt-evaluation/v1alpha1" || r.TaktVersion == "" {
		fail("report identity missing: %+v", r)
	}
	if r.Strategy.ID != "fake-pi-route-feedback-v1" || len(r.Strategy.Fingerprint) != 64 || len(r.Strategy.Workflow) != 64 || len(r.Strategy.Config) != 64 || len(r.Strategy.Commands) != 64 {
		fail("strategy identity missing: %+v", r.Strategy)
	}
	if r.Benchmark.ID != "route-dsl-infrastructure" || r.Benchmark.CaseCount != 2 || r.Benchmark.QualityNode != "full-validation" || len(r.Benchmark.Dataset) != 64 || len(r.Benchmark.Workspace) != 64 || len(r.Benchmark.Validator.Fingerprint) != 64 {
		fail("benchmark identity missing: %+v", r.Benchmark)
	}
	if len(r.Runs) != 2 || r.Summary.Total != 2 || r.Summary.ByStatus["completed"] != 2 {
		fail("unexpected run summary: %+v", r.Summary)
	}
	for _, run := range r.Runs {
		if run.Status != "completed" || run.Answers != 1 || run.Resumed != 1 {
			fail("unexpected run: %+v", run)
		}
		if run.Quality == nil || run.Quality.ProtocolVersion != "takt-validation/v1alpha1" || !run.Quality.Valid || run.Quality.Score != 100 || run.AttemptsToValid != 2 || run.ValidAtFirstAttempt {
			fail("unexpected quality result: %+v", run)
		}
		node := run.Nodes["implement"]
		if node.Status != "completed" || node.Attempts != 2 || !node.Resumed || node.Usage == nil {
			fail("unexpected implement node: %+v", node)
		}
		if node.Assistant != "pi" || !strings.Contains(node.AssistantVersion, "0.83.0") || node.RequestedModel == nil || node.RequestedModel.Name != "route-model" || node.RequestedModel.Provider != "openai" || node.RequestedModel.ID != "fake-route-model" {
			fail("requested model identity missing: %+v", node)
		}
		if node.ResolvedModel == nil || node.ResolvedModel.Provider != "openai" || node.ResolvedModel.ID != "fake-route-model" {
			fail("resolved model identity missing: %+v", node.ResolvedModel)
		}
		if !strings.Contains(node.Feedback, "ROUTE_INVALID") || node.DiagnosticOutput == "" {
			fail("resume diagnostics were not preserved: %+v", node)
		}
		validation := run.Nodes["full-validation"]
		if !strings.Contains(validation.DiagnosticOutput, `"valid":true`) {
			fail("validator diagnostic output was not preserved: %+v", validation)
		}
		if node.Usage.InputTokens != 222 || node.Usage.OutputTokens != 44 || math.Abs(node.Usage.Cost-0.025) > 1e-9 {
			fail("unexpected usage: %+v", node.Usage)
		}
	}
	if r.Summary.InputTokens != 444 || r.Summary.OutputTokens != 88 || r.Summary.Answers != 2 || math.Abs(r.Summary.Cost-0.05) > 1e-9 {
		fail("unexpected aggregate metrics: %+v", r.Summary)
	}
	if r.Summary.QualityRuns != 2 || r.Summary.Valid != 2 || r.Summary.ValidAtFirstAttempt != 0 || r.Summary.SuccessAt1 != 0 || r.Summary.FinalSuccessRate != 1 || r.Summary.AverageAttemptsToValid != 2 || r.Summary.AverageScore != 100 {
		fail("unexpected quality metrics: %+v", r.Summary)
	}
	if r.Summary.ByAssistant["pi"] != 2 || r.Summary.ByAssistantVersion["takt-fake-pi 0.83.0"] != 2 || r.Summary.ByRequestedModel["route-model=openai/fake-route-model"] != 2 || r.Summary.ByResolvedModel["route-model=openai/fake-route-model"] != 2 {
		fail("unexpected model summary: %+v", r.Summary)
	}
	fmt.Println("Route DSL evaluation: PASS")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "route evaluation assertion failed: "+format+"\n", args...)
	os.Exit(1)
}
