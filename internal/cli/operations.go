package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"takt/internal/application"
	"takt/internal/daemon"
)

func runDispatchCmd(args []string) error {
	if len(args) == 0 {
		return runCmd(args)
	}
	switch args[0] {
	case "list", "attention", "summary", "watch", "pause", "resume", "retry", "fork", "abandon", "recover":
		return runOperationsCmd(args[0], args[1:])
	default:
		return runCmd(args)
	}
}

func runOperationsCmd(operation string, args []string) error {
	switch operation {
	case "list":
		return runsCmd(args)
	case "attention":
		return attentionCmd(args)
	case "summary":
		return runSummaryCmd(args)
	case "watch":
		return runWatchCmd(args)
	case "pause":
		return runPauseCmd(args)
	case "resume":
		return runResumePausedCmd(args)
	case "retry":
		return runRetryCmd(args)
	case "fork":
		return runForkCmd(args)
	case "abandon":
		return runAbandonCmd(args)
	case "recover":
		return runRecoverCmd(args)
	default:
		return fmt.Errorf("unknown run operation %q", operation)
	}
}

func runsCmd(args []string) error {
	fs := newFlagSet("run list")
	workspace := fs.String("workspace", ".", "control workspace")
	status := fs.String("status", "", "status filter")
	active := fs.Bool("active", false, "only non-terminal Runs")
	attention := fs.Bool("attention", false, "only Runs requiring attention")
	rootOnly := fs.Bool("root-only", false, "exclude child Runs")
	limit := fs.Int("limit", 200, "maximum Runs")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--status": true, "--active": false, "--attention": false, "--root-only": false, "--limit": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt run list [--active] [--attention] [--status value]")
	}
	request := application.RunListRequest{Status: *status, ActiveOnly: *active, AttentionOnly: *attention, RootOnly: *rootOnly, Limit: *limit}
	var result []application.RunListEntry
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "run.list", request, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		result, err = service.RunService.ListRuns(request)
		if err != nil {
			return err
		}
	}
	return printResult(*jsonOut, map[string]any{"runs": result})
}

func attentionCmd(args []string) error {
	fs := newFlagSet("run attention")
	workspace := fs.String("workspace", ".", "control workspace")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt run attention")
	}
	var result []application.AttentionItem
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "run.attention", map[string]any{}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		result, err = service.Attention()
		if err != nil {
			return err
		}
	}
	return printResult(*jsonOut, map[string]any{"attention": result})
}

func runSummaryCmd(args []string) error {
	fs := newFlagSet("run summary")
	workspace := fs.String("workspace", ".", "control workspace")
	recursive := fs.Bool("recursive", true, "aggregate descendant Runs")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--recursive": false, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt run summary <run-id>")
	}
	var result application.RunSummary
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "run.summary", map[string]any{"run_id": fs.Arg(0), "recursive": *recursive}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.Summary(fs.Arg(0), *recursive)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runPauseCmd(args []string) error {
	fs := newFlagSet("run pause")
	workspace := fs.String("workspace", ".", "control workspace")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt run pause <run-id>")
	}
	var result application.PauseResult
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "run.pause", map[string]string{"run_id": fs.Arg(0)}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.Pause(fs.Arg(0))
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runResumePausedCmd(args []string) error {
	fs := newFlagSet("run resume")
	workspace := fs.String("workspace", ".", "control workspace")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt run resume <run-id>")
	}
	var result application.RunState
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "run.resume_paused", map[string]string{"run_id": fs.Arg(0)}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.RunService.ResumePaused(context.Background(), fs.Arg(0), false)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runRetryCmd(args []string) error {
	fs := newFlagSet("run retry")
	workspace := fs.String("workspace", ".", "control workspace")
	node := fs.String("node", "", "failed node ID")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--node": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt run retry <run-id> [--node id]")
	}
	request := application.RetryRequest{RunID: fs.Arg(0), NodeID: *node}
	var result application.RunState
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "run.retry", request, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.RunService.Retry(context.Background(), request)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runForkCmd(args []string) error {
	fs := newFlagSet("run fork")
	workspace := fs.String("workspace", ".", "control workspace")
	input := fs.String("input", "", "replacement input or Dynamic Plan goal")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--input": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt run fork <run-id> [--input value]")
	}
	request := application.ForkRequest{RunID: fs.Arg(0), Input: *input}
	var result application.ForkResult
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "run.fork", request, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.RunService.Fork(context.Background(), request)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runAbandonCmd(args []string) error {
	fs := newFlagSet("run abandon")
	workspace := fs.String("workspace", ".", "control workspace")
	reason := fs.String("reason", "", "operator reason")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--reason": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt run abandon <run-id> [--reason text]")
	}
	var result any
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		var value map[string]any
		if err := client.Call(context.Background(), "run.abandon", map[string]any{"run_id": fs.Arg(0), "reason": *reason}, &value); err != nil {
			return err
		}
		result = value
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		result, err = service.Abandon(fs.Arg(0), *reason)
		if err != nil {
			return err
		}
	}
	return printResult(*jsonOut, result)
}

func runRecoverCmd(args []string) error {
	fs := newFlagSet("run recover")
	workspace := fs.String("workspace", ".", "control workspace")
	useDaemon := fs.Bool("daemon", false, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt run recover")
	}
	var result application.RecoverResult
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		if err := client.Call(context.Background(), "run.recover", map[string]any{}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.RunService.RecoverInterruptedRunsForeground(context.Background())
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runWatchCmd(args []string) error {
	fs := newFlagSet("run watch")
	workspace := fs.String("workspace", ".", "control workspace")
	useDaemon := fs.Bool("daemon", true, "use local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", false, "emit NDJSON events")
	interval := fs.Duration("interval", time.Second, "status refresh interval")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--json": false, "--interval": true})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt run watch <run-id>")
	}
	runID := fs.Arg(0)
	var revision uint64
	for {
		var events application.EventsResult
		var state application.RunState
		if *useDaemon {
			client, err := daemon.NewClient(*workspace, *socket)
			if err != nil {
				return err
			}
			if err := client.Call(context.Background(), "run.events", map[string]any{"run_id": runID, "after_revision": revision, "limit": 200, "wait_ms": int(interval.Milliseconds())}, &events); err != nil {
				return err
			}
			if err := client.Call(context.Background(), "run.get", map[string]string{"run_id": runID}, &state); err != nil {
				return err
			}
		} else {
			service, err := localServices(*workspace, ".takt/config.yaml")
			if err != nil {
				return err
			}
			value, err := service.Events(context.Background(), runID, revision, 200, *interval)
			if err != nil {
				return err
			}
			events = *value
			current, err := service.RunService.GetRun(runID)
			if err != nil {
				return err
			}
			state = *current
		}
		for _, event := range events.Events {
			revision = event.Revision
			if *jsonOut {
				raw, _ := json.Marshal(event)
				fmt.Println(string(raw))
			} else {
				fmt.Fprintf(os.Stdout, "%s %-28s %s\n", event.Time.Format(time.RFC3339), event.Type, event.NodeID)
			}
		}
		if terminalRunState(state.Status) || state.Status == application.RunWaiting || state.Status == application.RunPaused {
			if !*jsonOut {
				fmt.Fprintf(os.Stdout, "status: %s\n", state.Status)
			}
			return nil
		}
	}
}

func terminalRunState(status string) bool {
	switch strings.ToLower(status) {
	case application.RunCompleted, application.RunFailed, application.RunCancelled, application.RunAbandoned:
		return true
	default:
		return false
	}
}
