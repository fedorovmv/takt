package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"takt/internal/bootstrap"
)

func worktreeCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt worktree <list|remove|prune> ...")
	}
	switch args[0] {
	case "list":
		return worktreeListCmd(args[1:])
	case "remove":
		return worktreeRemoveCmd(args[1:])
	case "prune":
		return worktreePruneCmd(args[1:])
	default:
		return fmt.Errorf("usage: takt worktree <list|remove|prune> ...")
	}
}

func worktreeListCmd(args []string) error {
	fs := newFlagSet("worktree list")
	workspace := fs.String("workspace", ".", "control workspace")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt worktree list [--workspace dir]")
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	app, err := bootstrap.New(abs, "")
	if err != nil {
		return err
	}
	result, err := app.Services.WorktreeService.List(context.Background())
	if err != nil {
		return err
	}
	return printResult(*jsonOut, map[string]any{"worktrees": result})
}

func worktreeRemoveCmd(args []string) error {
	fs := newFlagSet("worktree remove")
	workspace := fs.String("workspace", ".", "control workspace")
	force := fs.Bool("force", false, "remove a dirty worktree")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--force": false, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt worktree remove <run-id> [--force]")
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	app, err := bootstrap.New(abs, "")
	if err != nil {
		return err
	}
	result, err := app.Services.WorktreeService.Remove(context.Background(), fs.Arg(0), *force)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
}

func worktreePruneCmd(args []string) error {
	fs := newFlagSet("worktree prune")
	workspace := fs.String("workspace", ".", "workspace inside the Git repository")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt worktree prune [--workspace dir]")
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	app, err := bootstrap.New(abs, "")
	if err != nil {
		return err
	}
	if err := app.Services.WorktreeService.Prune(context.Background()); err != nil {
		return err
	}
	return printResult(*jsonOut, map[string]any{"pruned": true})
}
