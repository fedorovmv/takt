package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
)

type envelope struct {
	Result report `json:"result"`
}

type report struct {
	Runs    []runRecord `json:"runs"`
	Summary summary     `json:"summary"`
}

type summary struct {
	Total        int            `json:"total"`
	ByStatus     map[string]int `json:"by_status"`
	InputTokens  int            `json:"input_tokens"`
	OutputTokens int            `json:"output_tokens"`
	Cost         float64        `json:"cost"`
	Answers      int            `json:"answers"`
}

type runRecord struct {
	Status       string                `json:"status"`
	InputTokens  int                   `json:"input_tokens"`
	OutputTokens int                   `json:"output_tokens"`
	Cost         float64               `json:"cost"`
	Answers      int                   `json:"answers"`
	Nodes        map[string]nodeRecord `json:"nodes"`
}

type nodeRecord struct {
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
	Usage    *usage `json:"usage"`
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
	if len(value.Result.Runs) != 2 || value.Result.Summary.Total != 2 || value.Result.Summary.ByStatus["completed"] != 2 {
		fail("unexpected run summary: %+v", value.Result.Summary)
	}
	for _, run := range value.Result.Runs {
		if run.Status != "completed" || run.Answers != 1 {
			fail("unexpected run: %+v", run)
		}
		node := run.Nodes["implement"]
		if node.Status != "completed" || node.Attempts != 2 || node.Usage == nil {
			fail("unexpected implement node: %+v", node)
		}
		if node.Usage.InputTokens != 222 || node.Usage.OutputTokens != 44 || math.Abs(node.Usage.Cost-0.025) > 1e-9 {
			fail("unexpected usage: %+v", node.Usage)
		}
	}
	if value.Result.Summary.InputTokens != 444 || value.Result.Summary.OutputTokens != 88 || value.Result.Summary.Answers != 2 || math.Abs(value.Result.Summary.Cost-0.05) > 1e-9 {
		fail("unexpected aggregate metrics: %+v", value.Result.Summary)
	}
	fmt.Println("Route DSL evaluation: PASS")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "route evaluation assertion failed: "+format+"\n", args...)
	os.Exit(1)
}
