package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"takt/internal/application"
	"takt/internal/bootstrap"
)

func commandCmd(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "run" {
		return fmt.Errorf("usage: takt command run <name> [flags]")
	}
	fs := newFlagSet("command run")
	configPath := fs.String("config", ".takt/config.yaml", "config path")
	workspace := fs.String("workspace", ".", "workspace")
	input := fs.String("input", "", "input text or file")
	assistantName := fs.String("assistant", "", "override assistant")
	modelName := fs.String("model", "", "override model")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args[1:], map[string]bool{"--config": true, "--workspace": true, "--input": true, "--assistant": true, "--model": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt command run <name>")
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	app, err := bootstrap.New(abs, *configPath)
	if err != nil {
		return err
	}
	state, err := app.Core.CommandService.Run(ctx, application.CommandRunRequest{
		Name: fs.Arg(0), Input: *input, Assistant: *assistantName, Model: *modelName, ConfigPath: *configPath,
	})
	if err != nil {
		return err
	}
	return printResult(*jsonOut, state)
}
