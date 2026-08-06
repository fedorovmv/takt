package main

import (
	"context"
	"fmt"
	"strings"

	"takt/internal/control"
	"takt/internal/daemon"
)

func taskCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt task <start|status|respond|stop|explain> ...")
	}
	switch args[0] {
	case "start":
		return taskStartCmd(args[1:])
	case "status":
		return taskStatusCmd(args[1:])
	case "respond":
		return taskRespondCmd(args[1:])
	case "stop":
		return taskStopCmd(args[1:])
	case "explain":
		return taskExplainCmd(args[1:])
	default:
		return fmt.Errorf("usage: takt task <start|status|respond|stop|explain> ...")
	}
}

func taskStartCmd(args []string) error {
	fs := newFlagSet("task start")
	workspace := fs.String("workspace", ".", "control workspace")
	profileName := fs.String("profile", "code", "routing profile")
	goNow := fs.Bool("go", false, "confirm the preview and start immediately")
	useDaemon := fs.Bool("daemon", false, "run through the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--profile": true, "--go": false, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	goal := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if goal == "" {
		return fmt.Errorf("usage: takt task start <goal> [--go] [--profile code] [--daemon]")
	}
	if raw, err := readInput(goal); err == nil {
		goal = raw
	}
	request := control.TaskStartRequest{Goal: goal, Profile: *profileName, Go: *goNow}
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		var result control.TaskView
		if err := client.Call(context.Background(), "task.start", request, &result); err != nil {
			return err
		}
		return printResult(*jsonOut, &result)
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	result, err := service.StartTask(context.Background(), request)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
}

func taskStatusCmd(args []string) error {
	fs := newFlagSet("task status")
	workspace := fs.String("workspace", ".", "control workspace")
	useDaemon := fs.Bool("daemon", false, "query the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt task status <plan-id|run-id> [--daemon]")
	}
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		var result control.TaskView
		if err := client.Call(context.Background(), "task.status", map[string]string{"reference": fs.Arg(0)}, &result); err != nil {
			return err
		}
		return printResult(*jsonOut, &result)
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	result, err := service.TaskStatus(fs.Arg(0))
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
}

func taskRespondCmd(args []string) error {
	fs := newFlagSet("task respond")
	workspace := fs.String("workspace", ".", "control workspace")
	action := fs.String("action", "", "go, continue, answer, steer, pause, resume, or retry")
	message := fs.String("message", "", "answer or steering message")
	nodeID := fs.String("node", "", "optional waiting or failed node")
	useDaemon := fs.Bool("daemon", false, "submit through the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--action": true, "--message": true, "--node": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 || strings.TrimSpace(*action) == "" {
		return fmt.Errorf("usage: takt task respond <plan-id|run-id> --action <action> [--message text] [--daemon]")
	}
	request := control.TaskRespondRequest{Reference: fs.Arg(0), Action: *action, Message: *message, NodeID: *nodeID}
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		var result control.TaskView
		if err := client.Call(context.Background(), "task.respond", request, &result); err != nil {
			return err
		}
		return printResult(*jsonOut, &result)
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	result, err := service.RespondTask(context.Background(), request)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
}

func taskStopCmd(args []string) error {
	fs := newFlagSet("task stop")
	workspace := fs.String("workspace", ".", "control workspace")
	reason := fs.String("reason", "", "stop reason")
	useDaemon := fs.Bool("daemon", false, "submit through the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--reason": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt task stop <plan-id|run-id> [--reason text] [--daemon]")
	}
	request := control.TaskStopRequest{Reference: fs.Arg(0), Reason: *reason}
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		var result control.TaskView
		if err := client.Call(context.Background(), "task.stop", request, &result); err != nil {
			return err
		}
		return printResult(*jsonOut, &result)
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	result, err := service.StopTask(request)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
}

func taskExplainCmd(args []string) error {
	fs := newFlagSet("task explain")
	workspace := fs.String("workspace", ".", "control workspace")
	useDaemon := fs.Bool("daemon", false, "query the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt task explain <plan-id|run-id> [--daemon]")
	}
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		var result control.TaskView
		if err := client.Call(context.Background(), "task.explain", map[string]string{"reference": fs.Arg(0)}, &result); err != nil {
			return err
		}
		return printResult(*jsonOut, &result)
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	result, err := service.ExplainTask(fs.Arg(0))
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
}
