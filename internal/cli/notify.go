package cli

import (
	"context"
	"fmt"

	"takt/internal/application"
	"takt/internal/bootstrap"
	"takt/internal/daemon"
)

func notifyCmd(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt notify <list|ack|test|dispatch>")
	}
	sub := args[0]
	args = args[1:]
	switch sub {
	case "list":
		fs := newFlagSet("notify list")
		workspace := fs.String("workspace", ".", "control workspace")
		unread := fs.Bool("unread", false, "only unacknowledged notifications")
		limit := fs.Int("limit", 100, "maximum notifications")
		useDaemon := fs.Bool("daemon", false, "use local daemon")
		socket := fs.String("socket", "", "daemon Unix socket path")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--unread": false, "--limit": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: takt notify list [--unread]")
		}
		var result []application.NotificationItem
		if *useDaemon {
			client, err := daemon.NewClient(*workspace, *socket)
			if err != nil {
				return err
			}
			if err := client.Call(ctx, "notify.list", map[string]any{"unread_only": *unread, "limit": *limit}, &result); err != nil {
				return err
			}
		} else {
			app, err := bootstrap.New(*workspace, "")
			if err != nil {
				return err
			}
			notifyService := app.Services.Notifications
			result, err = notifyService.List(*unread, *limit)
			if err != nil {
				return err
			}
		}
		return printResult(*jsonOut, map[string]any{"notifications": result})
	case "ack":
		fs := newFlagSet("notify ack")
		workspace := fs.String("workspace", ".", "control workspace")
		useDaemon := fs.Bool("daemon", false, "use local daemon")
		socket := fs.String("socket", "", "daemon Unix socket path")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt notify ack <notification-id>")
		}
		var result application.NotificationItem
		if *useDaemon {
			client, err := daemon.NewClient(*workspace, *socket)
			if err != nil {
				return err
			}
			if err := client.Call(ctx, "notify.ack", map[string]string{"id": fs.Arg(0)}, &result); err != nil {
				return err
			}
		} else {
			app, err := bootstrap.New(*workspace, "")
			if err != nil {
				return err
			}
			notifyService := app.Services.Notifications
			value, err := notifyService.Ack(fs.Arg(0))
			if err != nil {
				return err
			}
			result = *value
		}
		return printResult(*jsonOut, &result)
	case "test":
		fs := newFlagSet("notify test")
		workspace := fs.String("workspace", ".", "control workspace")
		message := fs.String("message", "", "test message")
		useDaemon := fs.Bool("daemon", false, "use local daemon")
		socket := fs.String("socket", "", "daemon Unix socket path")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--message": true, "--daemon": false, "--socket": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: takt notify test [--message text]")
		}
		var result application.NotificationItem
		if *useDaemon {
			client, err := daemon.NewClient(*workspace, *socket)
			if err != nil {
				return err
			}
			if err := client.Call(ctx, "notify.test", map[string]string{"message": *message}, &result); err != nil {
				return err
			}
		} else {
			app, err := bootstrap.New(*workspace, "")
			if err != nil {
				return err
			}
			notifyService := app.Services.Notifications
			value, err := notifyService.Test(*message)
			if err != nil {
				return err
			}
			result = *value
		}
		return printResult(*jsonOut, &result)
	case "dispatch":
		fs := newFlagSet("notify dispatch")
		workspace := fs.String("workspace", ".", "control workspace")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
			return err
		}
		app, err := bootstrap.New(*workspace, "")
		if err != nil {
			return err
		}
		notifyService := app.Services.Notifications
		result, err := notifyService.Dispatch()
		if err != nil {
			return err
		}
		return printResult(*jsonOut, map[string]any{"emitted": result})
	default:
		return fmt.Errorf("unknown notify subcommand %q", sub)
	}
}
