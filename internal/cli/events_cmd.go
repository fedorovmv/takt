package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"takt/internal/daemon"
	"time"

	"takt/internal/application"
)

func eventsCmd(args []string) error {
	fs := newFlagSet("events")
	workspace := fs.String("workspace", ".", "workspace")
	useDaemon := fs.Bool("daemon", false, "subscribe through the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	after := fs.Uint64("after", 0, "read events after this revision")
	limit := fs.Int("limit", 200, "maximum events per batch")
	follow := fs.Bool("follow", false, "follow until the Run becomes terminal")
	jsonOut := fs.Bool("json", true, "JSON lines output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--after": true, "--limit": true, "--follow": false, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt events <run-id> [--follow] [--daemon]")
	}
	runID := fs.Arg(0)
	printEvent := func(event application.Event) error {
		if *jsonOut {
			raw, err := json.Marshal(event)
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		}
		fmt.Printf("%d\t%s\t%s\n", event.Revision, event.Type, event.NodeID)
		return nil
	}
	if *useDaemon && *follow {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		return client.Subscribe(context.Background(), runID, *after, *limit, printEvent)
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	cursor := *after
	for {
		wait := time.Duration(0)
		if *follow {
			wait = 30 * time.Second
		}
		result, err := service.Events(context.Background(), runID, cursor, *limit, wait)
		if err != nil {
			return err
		}
		for _, event := range result.Events {
			if err := printEvent(event); err != nil {
				return err
			}
			cursor = event.Revision
		}
		if !*follow {
			return nil
		}
		state, err := service.RunService.GetRun(runID)
		if err != nil {
			return err
		}
		if (state.Status == application.RunCompleted || state.Status == application.RunFailed || state.Status == application.RunCancelled || state.Status == application.RunAbandoned) && len(result.Events) == 0 {
			return nil
		}
	}
}
