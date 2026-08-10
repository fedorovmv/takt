package opencodeevents

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type event struct {
	Type  string `json:"type"`
	Part  part   `json:"part"`
	Error struct {
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	} `json:"error"`
}

type part struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Tokens struct {
		Input  int `json:"input"`
		Output int `json:"output"`
	} `json:"tokens"`
}

func Summarize(r io.Reader) (Usage, error) {
	var usage Usage
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var current event
		if err := json.Unmarshal(scanner.Bytes(), &current); err != nil {
			return Usage{}, fmt.Errorf("decode event: %w", err)
		}
		if current.Type == "step_finish" {
			usage.InputTokens += current.Part.Tokens.Input
			usage.OutputTokens += current.Part.Tokens.Output
		}
	}
	if err := scanner.Err(); err != nil {
		return Usage{}, err
	}
	return usage, nil
}
