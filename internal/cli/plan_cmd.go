package cli

import (
	"context"
	"fmt"
	"strings"
	"takt/internal/application"
	"takt/internal/daemon"
)

func planCmd(args []string) error {
	if len(args) > 0 && args[0] == "get" {
		fs := newFlagSet("plan get")
		workspace := fs.String("workspace", ".", "control workspace")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt plan get <plan-id> [--workspace dir]")
		}
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		record, err := service.PlanService.GetPlan(fs.Arg(0))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, record)
	}
	if len(args) > 0 && args[0] == "promote" {
		fs := newFlagSet("plan promote")
		workspace := fs.String("workspace", ".", "control workspace")
		name := fs.String("name", "", "generated workflow name")
		force := fs.Bool("force", false, "replace an existing generated workflow")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--name": true, "--force": false, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 || strings.TrimSpace(*name) == "" {
			return fmt.Errorf("usage: takt plan promote <plan-id> --name workflow-name [--workspace dir]")
		}
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		record, err := service.PlanService.PromotePlanWithOptions(fs.Arg(0), *name, application.PromotePlanOptions{Force: *force})
		if err != nil {
			return err
		}
		return printResult(*jsonOut, record)
	}
	fs := newFlagSet("plan")
	workspace := fs.String("workspace", ".", "control workspace")
	profileName := fs.String("profile", "code", "planning profile")
	input := fs.String("input", "", "goal text or readable file")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--profile": true, "--input": true, "--json": false})); err != nil {
		return err
	}
	goal := strings.TrimSpace(*input)
	if goal == "" {
		goal = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}
	if goal == "" {
		return fmt.Errorf("usage: takt plan <goal> [--profile code] [--workspace dir]")
	}
	if raw, err := readInput(goal); err == nil {
		goal = raw
	}
	service, err := localServices(*workspace, ".takt/config.yaml")
	if err != nil {
		return err
	}
	result, err := service.PlanService.Plan(context.Background(), application.PlanRequest{Goal: goal, Profile: *profileName})
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
}

func executeCmd(args []string) error {
	fs := newFlagSet("execute")
	workspace := fs.String("workspace", ".", "control workspace")
	confirm := fs.Bool("confirm", false, "confirm the preview and hard limits")
	useDaemon := fs.Bool("daemon", false, "submit execution to the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--confirm": false, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt execute <plan-id> --confirm [--daemon] [--workspace dir]")
	}
	request := application.ExecutePlanRequest{PlanID: fs.Arg(0), Confirm: *confirm}
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		var record application.PlanRecord
		if err := client.Call(context.Background(), "plan.execute", request, &record); err != nil {
			return err
		}
		return printResult(*jsonOut, &record)
	}
	service, err := localServices(*workspace, ".takt/config.yaml")
	if err != nil {
		return err
	}
	record, err := service.PlanService.ExecutePlan(context.Background(), request)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, record)
}

func steerCmd(args []string) error {
	fs := newFlagSet("steer")
	workspace := fs.String("workspace", ".", "control workspace")
	runID := fs.String("run", "", "execution Run ID instead of plan ID")
	useDaemon := fs.Bool("daemon", false, "submit steering to the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--run": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() < 2 && *runID == "" {
		return fmt.Errorf("usage: takt steer <plan-id> <message> [--daemon] [--workspace dir]")
	}
	request := application.SteerRequest{RunID: *runID}
	if *runID != "" {
		request.Message = strings.TrimSpace(strings.Join(fs.Args(), " "))
	} else {
		request.PlanID = fs.Arg(0)
		request.Message = strings.TrimSpace(strings.Join(fs.Args()[1:], " "))
	}
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		var record application.PlanRecord
		if err := client.Call(context.Background(), "plan.steer", request, &record); err != nil {
			return err
		}
		return printResult(*jsonOut, &record)
	}
	service, err := localServices(*workspace, ".takt/config.yaml")
	if err != nil {
		return err
	}
	record, err := service.PlanService.Steer(context.Background(), request)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, record)
}
