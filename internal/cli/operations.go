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

func runDispatchCmd(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return runCmd(ctx, args)
	}
	switch args[0] {
	case "list", "attention", "summary", "watch", "pause", "resume", "retry", "fork", "abandon", "recover":
		return runOperationsCmd(ctx, args[0], args[1:])
	default:
		return runCmd(ctx, args)
	}
}

func runOperationsCmd(ctx context.Context, operation string, args []string) error {
	switch operation {
	case "list":
		return runsCmd(ctx, args)
	case "attention":
		return attentionCmd(ctx, args)
	case "summary":
		return runSummaryCmd(ctx, args)
	case "watch":
		return runWatchCmd(ctx, args)
	case "pause":
		return runPauseCmd(ctx, args)
	case "resume":
		return runResumePausedCmd(ctx, args)
	case "retry":
		return runRetryCmd(ctx, args)
	case "fork":
		return runForkCmd(ctx, args)
	case "abandon":
		return runAbandonCmd(ctx, args)
	case "recover":
		return runRecoverCmd(ctx, args)
	default:
		return fmt.Errorf("unknown run operation %q", operation)
	}
}

func runsCmd(ctx context.Context, args []string) error {
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
		if err := client.Call(ctx, "run.list", request, &result); err != nil {
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

func attentionCmd(ctx context.Context, args []string) error {
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
		if err := client.Call(ctx, "run.attention", map[string]any{}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		result, err = service.RunService.Attention()
		if err != nil {
			return err
		}
	}
	return printResult(*jsonOut, map[string]any{"attention": result})
}

func runSummaryCmd(ctx context.Context, args []string) error {
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
		if err := client.Call(ctx, "run.summary", map[string]any{"run_id": fs.Arg(0), "recursive": *recursive}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.RunService.Summary(fs.Arg(0), *recursive)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runPauseCmd(ctx context.Context, args []string) error {
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
		if err := client.Call(ctx, "run.pause", map[string]string{"run_id": fs.Arg(0)}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.RunService.Pause(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runResumePausedCmd(ctx context.Context, args []string) error {
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
		if err := client.Call(ctx, "run.resume_paused", map[string]string{"run_id": fs.Arg(0)}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.RunService.ResumePaused(ctx, fs.Arg(0), false)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runRetryCmd(ctx context.Context, args []string) error {
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
		if err := client.Call(ctx, "run.retry", request, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.RunService.Retry(ctx, request)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runForkCmd(ctx context.Context, args []string) error {
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
		if err := client.Call(ctx, "run.fork", request, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.ForkService.Fork(ctx, request)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runAbandonCmd(ctx context.Context, args []string) error {
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
		if err := client.Call(ctx, "run.abandon", map[string]any{"run_id": fs.Arg(0), "reason": *reason}, &value); err != nil {
			return err
		}
		result = value
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		result, err = service.RunService.Abandon(ctx, fs.Arg(0), *reason)
		if err != nil {
			return err
		}
	}
	return printResult(*jsonOut, result)
}

func runRecoverCmd(ctx context.Context, args []string) error {
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
		if err := client.Call(ctx, "run.recover", map[string]any{}, &result); err != nil {
			return err
		}
	} else {
		service, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		value, err := service.RunService.RecoverInterruptedRunsForeground(ctx)
		if err != nil {
			return err
		}
		result = *value
	}
	return printResult(*jsonOut, &result)
}

func runWatchCmd(ctx context.Context, args []string) error {
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
			if err := client.Call(ctx, "run.events", map[string]any{"run_id": runID, "after_revision": revision, "limit": 200, "wait_ms": int(interval.Milliseconds())}, &events); err != nil {
				return err
			}
			if err := client.Call(ctx, "run.get", map[string]string{"run_id": runID}, &state); err != nil {
				return err
			}
		} else {
			service, err := localServices(*workspace, ".takt/config.yaml")
			if err != nil {
				return err
			}
			value, err := service.RunService.Events(ctx, runID, revision, 200, *interval)
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
