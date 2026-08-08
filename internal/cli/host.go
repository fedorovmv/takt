package cli

import (
	"context"
	"fmt"
	"strings"

	"takt/internal/application"
	"takt/internal/daemon"
)

func hostCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt host <begin|confirm|status|find|guard-tool|guard-completion|release> ...")
	}
	switch args[0] {
	case "begin":
		return hostBeginCmd(args[1:])
	case "confirm":
		return hostConfirmCmd(args[1:])
	case "status":
		return hostStatusCmd(args[1:])
	case "find":
		return hostFindCmd(args[1:])
	case "guard-tool":
		return hostGuardToolCmd(args[1:])
	case "guard-completion":
		return hostGuardCompletionCmd(args[1:])
	case "release":
		return hostReleaseCmd(args[1:])
	default:
		return fmt.Errorf("unknown host subcommand %q", args[0])
	}
}

func hostBeginCmd(args []string) error {
	fs := newFlagSet("host begin")
	workspace := fs.String("workspace", ".", "control workspace")
	host := fs.String("host", "", "coding-agent host name")
	hostSession := fs.String("host-session", "", "stable coding-agent session ID")
	profileName := fs.String("profile", "code", "planning profile")
	enforcement := fs.String("enforcement", application.HostEnforcementAdvisory, "advisory|guarded|strict")
	commandInterception := fs.Bool("command-interception", false, "host intercepts /takt before the LLM")
	inputInterception := fs.Bool("input-interception", false, "host intercepts later user input while managed")
	toolCallBlocking := fs.Bool("tool-call-blocking", false, "host can block tool calls before execution")
	completionBlocking := fs.Bool("completion-blocking", false, "host can block or bypass premature final responses")
	sessionRecovery := fs.Bool("session-recovery", false, "host restores managed mode after restart")
	useDaemon := fs.Bool("daemon", false, "use the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--host": true, "--host-session": true, "--profile": true, "--enforcement": true, "--command-interception": false, "--input-interception": false, "--tool-call-blocking": false, "--completion-blocking": false, "--session-recovery": false, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	goal := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if *host == "" || *hostSession == "" || goal == "" {
		return fmt.Errorf("usage: takt host begin <goal> --host pi|opencode --host-session <id> [--daemon]")
	}
	request := application.HostBeginRequest{Host: *host, HostSessionID: *hostSession, Goal: goal, Profile: *profileName, Enforcement: *enforcement, Capabilities: application.HostCapabilities{CommandInterception: *commandInterception, InputInterception: *inputInterception, ToolCallBlocking: *toolCallBlocking, CompletionBlocking: *completionBlocking, SessionRecovery: *sessionRecovery}}
	var result application.HostBeginResult
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "host.begin", request, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.HostService.BeginHostSession(context.Background(), request)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func hostConfirmCmd(args []string) error {
	fs := newFlagSet("host confirm")
	workspace := fs.String("workspace", ".", "control workspace")
	confirm := fs.Bool("confirm", false, "confirm preview and budgets")
	useDaemon := fs.Bool("daemon", false, "use local daemon for detached execution")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--confirm": false, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt host confirm <session-id> --confirm [--daemon]")
	}
	request := application.HostConfirmRequest{SessionID: fs.Arg(0), Confirm: *confirm}
	var result application.HostSessionView
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "host.confirm", request, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.HostService.ConfirmHostSession(context.Background(), request)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func hostStatusCmd(args []string) error {
	fs := newFlagSet("host status")
	workspace := fs.String("workspace", ".", "control workspace")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt host status <session-id>")
	}
	var result application.HostSessionView
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "host.get", map[string]string{"session_id": fs.Arg(0)}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.HostService.GetHostSession(fs.Arg(0))
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func hostFindCmd(args []string) error {
	fs := newFlagSet("host find")
	workspace := fs.String("workspace", ".", "control workspace")
	host := fs.String("host", "", "coding-agent host")
	hostSession := fs.String("host-session", "", "coding-agent session ID")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--host": true, "--host-session": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if *host == "" || *hostSession == "" {
		return fmt.Errorf("usage: takt host find --host pi|opencode --host-session <id>")
	}
	var result application.HostSessionView
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "host.find", map[string]string{"host": *host, "host_session_id": *hostSession}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.HostService.FindHostSession(*host, *hostSession)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func hostGuardToolCmd(args []string) error {
	fs := newFlagSet("host guard-tool")
	workspace := fs.String("workspace", ".", "control workspace")
	tool := fs.String("tool", "", "host tool name")
	readOnly := fs.Bool("read-only", false, "tool is read-only")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--tool": true, "--read-only": false, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 || *tool == "" {
		return fmt.Errorf("usage: takt host guard-tool <session-id> --tool <name>")
	}
	request := application.HostToolGuardRequest{SessionID: fs.Arg(0), Tool: *tool, ReadOnly: *readOnly}
	var result application.HostGuardDecision
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "host.guard_tool", request, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.HostService.GuardHostTool(request)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func hostGuardCompletionCmd(args []string) error {
	fs := newFlagSet("host guard-completion")
	workspace := fs.String("workspace", ".", "control workspace")
	kind := fs.String("kind", "final", "final|question|status")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--kind": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt host guard-completion <session-id> --kind final|question|status")
	}
	request := application.HostCompletionGuardRequest{SessionID: fs.Arg(0), Kind: *kind}
	var result application.HostGuardDecision
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "host.guard_completion", request, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.HostService.GuardHostCompletion(request)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func hostReleaseCmd(args []string) error {
	fs := newFlagSet("host release")
	workspace := fs.String("workspace", ".", "control workspace")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt host release <session-id>")
	}
	var result application.HostSession
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "host.release", map[string]string{"session_id": fs.Arg(0)}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.HostService.ReleaseHostSession(fs.Arg(0))
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}
