package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type envelope struct {
	Result runResult `json:"result"`
}

type runResult struct {
	ID      string               `json:"id"`
	Status  string               `json:"status"`
	Waiting *waitingState        `json:"waiting"`
	Nodes   map[string]nodeState `json:"nodes"`
}

type waitingState struct {
	NodeID string `json:"node_id"`
}

type nodeState struct {
	Status    string `json:"status"`
	Attempts  int    `json:"attempts"`
	SessionID string `json:"session_id"`
	Feedback  string `json:"feedback"`
	Output    string `json:"output"`
}

func main() {
	if len(os.Args) != 3 {
		fail("usage: routee2eassert <run|answer> <json-file>")
	}
	data, err := os.ReadFile(os.Args[2])
	if err != nil {
		fail("read %s: %v", os.Args[2], err)
	}
	var value envelope
	if err := json.Unmarshal(data, &value); err != nil {
		fail("decode %s: %v", os.Args[2], err)
	}

	switch os.Args[1] {
	case "run":
		assertRun(value.Result)
		fmt.Println(value.Result.ID)
	case "answer":
		assertAnswer(value.Result)
	default:
		fail("unknown assertion mode %q", os.Args[1])
	}
}

func assertRun(state runResult) {
	if state.Status != "waiting" {
		fail("expected waiting run, got %q", state.Status)
	}
	if state.Waiting == nil || state.Waiting.NodeID != "approve-result" {
		fail("expected approval at approve-result, got %+v", state.Waiting)
	}
	implement, ok := state.Nodes["implement"]
	if !ok {
		fail("implement node is missing")
	}
	if implement.Status != "completed" || implement.Attempts != 2 {
		fail("unexpected implement node: %+v", implement)
	}
	if implement.SessionID != "fake-pi-session-1" {
		fail("unexpected implement session id %q", implement.SessionID)
	}
	if !contains(implement.Feedback, "ROUTE_INVALID") {
		fail("validator feedback was not preserved: %q", implement.Feedback)
	}
	validation, ok := state.Nodes["full-validation"]
	if !ok || validation.Status != "completed" {
		fail("unexpected full-validation node: %+v", validation)
	}
	if state.ID == "" {
		fail("run id is empty")
	}
}

func assertAnswer(state runResult) {
	if state.Status != "completed" {
		fail("expected completed run, got %q", state.Status)
	}
	approval, ok := state.Nodes["approve-result"]
	if !ok || approval.Status != "completed" || approval.Output != "approved" {
		fail("unexpected approval node: %+v", approval)
	}
}

func contains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "route e2e assertion failed: "+format+"\n", args...)
	os.Exit(1)
}
