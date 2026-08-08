package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"takt/internal/bootstrap"
	"takt/internal/daemon"
	"takt/internal/mcp"
	"time"
)

func mcpCmd(args []string) error {
	fs := newFlagSet("mcp")
	workspace := fs.String("workspace", ".", "control workspace")
	configPath := fs.String("config", ".takt/config.yaml", "default config path for direct workflow files")
	useDaemon := fs.Bool("daemon", false, "proxy MCP through the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	surfaceValue := fs.String("surface", "agent", "MCP surface: agent, host, worker, operator, or all")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--config": true, "--daemon": false, "--socket": true, "--surface": true})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt mcp [--surface agent|host|worker|operator|all] [--daemon] [--workspace dir] [--config path]")
	}
	surface, err := mcp.ParseSurface(*surfaceValue)
	if err != nil {
		return err
	}
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		return proxyMCPThroughDaemon(context.Background(), client, string(surface), os.Stdin, os.Stdout, os.Stderr)
	}
	app, err := bootstrap.New(*workspace, *configPath)
	if err != nil {
		return err
	}
	services := app.Services
	deps := mcp.Dependencies{API: app.API, Plans: services.PlanService, External: services.ExternalService, Maintenance: services.Maintenance}
	return mcp.NewWithDependencies(deps, os.Stdin, os.Stdout, os.Stderr, surface).ServeStdio(context.Background())
}

func proxyMCPThroughDaemon(ctx context.Context, client *daemon.Client, surface string, in io.Reader, out, errOut io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var workers sync.WaitGroup
	var writeMu sync.Mutex
	sem := make(chan struct{}, 64)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		sem <- struct{}{}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-sem }()
			payload, respond, err := client.MCPForSurface(ctx, line, surface)
			if err != nil {
				fmt.Fprintln(errOut, "daemon MCP request failed:", err)
				return
			}
			if !respond {
				return
			}
			writeMu.Lock()
			defer writeMu.Unlock()
			_, _ = out.Write(append(payload, '\n'))
		}()
	}
	workers.Wait()
	return scanner.Err()
}

func daemonCmd(args []string) error {
	subcommand := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
		args = args[1:]
	}
	fs := newFlagSet("daemon " + subcommand)
	workspace := fs.String("workspace", ".", "control workspace")
	configPath := fs.String("config", ".takt/config.yaml", "default config path")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--config": true, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt daemon [serve|start|status|stop] [--workspace dir]")
	}
	switch subcommand {
	case "serve":
		server, err := daemon.New(daemon.Options{Workspace: *workspace, ConfigPath: *configPath, SocketPath: *socket, ErrOut: os.Stderr})
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return server.Serve(ctx)
	case "start":
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		if metadata, healthErr := client.Health(ctx); healthErr == nil {
			cancel()
			return printResult(*jsonOut, map[string]any{"started": false, "already_running": true, "daemon": metadata})
		}
		cancel()
		paths := client.Paths()
		if err := os.MkdirAll(filepath.Dir(paths.Log), 0o700); err != nil {
			return err
		}
		logFile, err := os.OpenFile(paths.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		executable, err := os.Executable()
		if err != nil {
			_ = logFile.Close()
			return err
		}
		childArgs := []string{"daemon", "serve", "--workspace", *workspace, "--config", *configPath}
		if *socket != "" {
			childArgs = append(childArgs, "--socket", *socket)
		}
		command := exec.Command(executable, childArgs...)
		command.Stdin = nil
		command.Stdout = logFile
		command.Stderr = logFile
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := command.Start(); err != nil {
			_ = logFile.Close()
			return err
		}
		_ = command.Process.Release()
		_ = logFile.Close()
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer waitCancel()
		metadata, err := daemon.WaitForHealth(waitCtx, client, 50*time.Millisecond)
		if err != nil {
			return fmt.Errorf("start daemon: %w; see %s", err, paths.Log)
		}
		return printResult(*jsonOut, map[string]any{"started": true, "daemon": metadata, "log": paths.Log})
	case "status":
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		metadata, err := client.Health(ctx)
		if err != nil {
			return fmt.Errorf("daemon is not running: %w", err)
		}
		return printResult(*jsonOut, map[string]any{"running": true, "daemon": metadata})
	case "stop":
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Shutdown(ctx); err != nil {
			return err
		}
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			probeCtx, probeCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			_, healthErr := client.Health(probeCtx)
			probeCancel()
			if healthErr != nil {
				return printResult(*jsonOut, map[string]any{"stopped": true})
			}
			select {
			case <-deadline.C:
				return fmt.Errorf("daemon did not stop")
			case <-ticker.C:
			}
		}
	default:
		return fmt.Errorf("usage: takt daemon [serve|start|status|stop] [--workspace dir]")
	}
}
