package cli

import (
	"context"
	"fmt"
	"strings"

	"takt/internal/bootstrap"
	experimentallearning "takt/internal/experimental/learning"
)

func learnCmd(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt learn <scan|list|get|propose|review|evaluate|stage>")
	}
	switch args[0] {
	case "scan":
		fs := newFlagSet("learn scan")
		workspace := fs.String("workspace", ".", "workspace containing .takt run history")
		minRuns := fs.Int("min-runs", 2, "minimum distinct runs sharing a stable pattern")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--min-runs": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: takt learn scan [--workspace dir] [--min-runs 2]")
		}
		service, err := learningService(*workspace)
		if err != nil {
			return err
		}
		patterns, err := service.Scan(ctx, *minRuns)
		if err != nil {
			return err
		}
		return printResult(*jsonOut, map[string]any{"patterns": patterns})
	case "list":
		fs := newFlagSet("learn list")
		workspace := fs.String("workspace", ".", "workspace containing learning proposals")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: takt learn list [--workspace dir]")
		}
		service, err := learningService(*workspace)
		if err != nil {
			return err
		}
		proposals, err := service.List()
		if err != nil {
			return err
		}
		return printResult(*jsonOut, map[string]any{"proposals": proposals})
	case "get":
		fs := newFlagSet("learn get")
		workspace := fs.String("workspace", ".", "workspace containing learning proposals")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt learn get <proposal-id> [--workspace dir]")
		}
		service, err := learningService(*workspace)
		if err != nil {
			return err
		}
		proposal, err := service.Get(fs.Arg(0))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, proposal)
	case "propose":
		fs := newFlagSet("learn propose")
		workspace := fs.String("workspace", ".", "workspace containing run history")
		pattern := fs.String("pattern", "", "fingerprint returned by learn scan")
		kind := fs.String("kind", "", "candidate kind: skill or block")
		name := fs.String("name", "", "candidate name")
		candidate := fs.String("candidate", "", "candidate SKILL.md or block package.yaml")
		benefit := fs.String("benefit", "", "expected reusable benefit")
		minRuns := fs.Int("min-runs", 2, "minimum distinct supporting runs")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--pattern": true, "--kind": true, "--name": true, "--candidate": true, "--benefit": true, "--min-runs": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 0 || strings.TrimSpace(*pattern) == "" || strings.TrimSpace(*kind) == "" || strings.TrimSpace(*name) == "" || strings.TrimSpace(*benefit) == "" {
			return fmt.Errorf("usage: takt learn propose --pattern fingerprint --kind skill|block --name name --benefit text [--candidate path] [--workspace dir]")
		}
		service, err := learningService(*workspace)
		if err != nil {
			return err
		}
		proposal, err := service.Propose(ctx, experimentallearning.ProposeRequest{
			PatternFingerprint: *pattern,
			CandidateKind:      *kind,
			Name:               *name,
			CandidatePath:      *candidate,
			ExpectedBenefit:    *benefit,
			MinRuns:            *minRuns,
		})
		if err != nil {
			return err
		}
		return printResult(*jsonOut, proposal)
	case "review":
		fs := newFlagSet("learn review")
		workspace := fs.String("workspace", ".", "workspace containing learning proposals")
		decision := fs.String("decision", "", "review decision: accept or reject")
		reason := fs.String("reason", "", "human review rationale")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--decision": true, "--reason": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 || strings.TrimSpace(*decision) == "" || strings.TrimSpace(*reason) == "" {
			return fmt.Errorf("usage: takt learn review <proposal-id> --decision accept|reject --reason text [--workspace dir]")
		}
		service, err := learningService(*workspace)
		if err != nil {
			return err
		}
		proposal, err := service.Review(fs.Arg(0), *decision, *reason)
		if err != nil {
			return err
		}
		return printResult(*jsonOut, proposal)
	case "evaluate":
		fs := newFlagSet("learn evaluate")
		workspace := fs.String("workspace", ".", "workspace containing learning proposals")
		report := fs.String("report", "", "evaluation matrix report JSON")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--report": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 || strings.TrimSpace(*report) == "" {
			return fmt.Errorf("usage: takt learn evaluate <proposal-id> --report evaluation.json [--workspace dir]")
		}
		service, err := learningService(*workspace)
		if err != nil {
			return err
		}
		proposal, err := service.Evaluate(fs.Arg(0), *report)
		if err != nil {
			return err
		}
		return printResult(*jsonOut, proposal)
	case "stage":
		fs := newFlagSet("learn stage")
		workspace := fs.String("workspace", ".", "workspace containing learning proposals")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt learn stage <proposal-id> [--workspace dir]")
		}
		service, err := learningService(*workspace)
		if err != nil {
			return err
		}
		proposal, err := service.Stage(fs.Arg(0))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, proposal)
	default:
		return fmt.Errorf("usage: takt learn <scan|list|get|propose|review|evaluate|stage>")
	}
}

func learningService(workspace string) (*experimentallearning.Service, error) {
	app, err := bootstrap.New(workspace, ".takt/config.yaml")
	if err != nil {
		return nil, err
	}
	return app.Learning, nil
}
