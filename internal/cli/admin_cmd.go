package cli

import (
	"context"
	"fmt"
	"strings"

	"takt/internal/application"
)

func blockCmd(args []string) error {
	subcommand := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
		args = args[1:]
	}
	switch subcommand {
	case "list", "describe":
		fs := newFlagSet("block " + subcommand)
		workspace := fs.String("workspace", ".", "control workspace")
		profileName := fs.String("profile", "code", "profile with trusted block packages")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--profile": true, "--json": false})); err != nil {
			return err
		}
		expected := 0
		if subcommand == "describe" {
			expected = 1
		}
		if fs.NArg() != expected {
			if subcommand == "describe" {
				return fmt.Errorf("usage: takt block describe <name> [--profile code] [--workspace dir]")
			}
			return fmt.Errorf("usage: takt block list [--profile code] [--workspace dir]")
		}
		services, err := localServices(*workspace, ".takt/config.yaml")
		if err != nil {
			return err
		}
		if subcommand == "list" {
			value, err := services.CatalogService.ListBlocks(*profileName)
			if err != nil {
				return err
			}
			return printResult(*jsonOut, value)
		}
		value, err := services.CatalogService.DescribeBlock(*profileName, fs.Arg(0))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, value)
	case "validate":
		fs := newFlagSet("block validate")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args, map[string]bool{"--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt block validate <package.yaml>")
		}
		services, err := localServices(".", ".takt/config.yaml")
		if err != nil {
			return err
		}
		catalog, err := services.CatalogService.ValidateBlockPackage(fs.Arg(0))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, catalog)
	default:
		return fmt.Errorf("unknown block subcommand %q", subcommand)
	}
}

func compatibilityCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt compatibility <matrix|fields|schema|check> ...")
	}
	switch args[0] {
	case "matrix", "fields", "schema":
		fs := newFlagSet("compatibility " + args[0])
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: takt compatibility %s [--json]", args[0])
		}
		services, err := localServices(".", ".takt/config.yaml")
		if err != nil {
			return err
		}
		switch args[0] {
		case "matrix":
			return printResult(*jsonOut, services.Compatibility.Matrix())
		case "fields":
			return printResult(*jsonOut, services.Compatibility.Fields())
		default:
			return printResult(*jsonOut, services.Compatibility.Matrix().SchemaSubset)
		}
	case "check":
		fs := newFlagSet("compatibility check")
		workspace := fs.String("workspace", ".", "control workspace")
		configPath := fs.String("config", ".takt/config.yaml", "config path")
		live := fs.Bool("live", false, "probe configured binaries/domain adapters")
		strict := fs.Bool("strict", false, "treat compatibility warnings as an error")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--config": true, "--live": false, "--strict": false, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: takt compatibility check [--workspace dir] [--config path] [--live] [--strict]")
		}
		services, err := localServices(*workspace, *configPath)
		if err != nil {
			return err
		}
		report, err := services.Compatibility.Check(context.Background(), *live)
		if err != nil {
			return err
		}
		if err := printResult(*jsonOut, report); err != nil {
			return err
		}
		if report.Status == "error" || (*strict && report.Status == "warning") {
			return fmt.Errorf("compatibility check status: %s", report.Status)
		}
		return nil
	default:
		return fmt.Errorf("usage: takt compatibility <matrix|fields|schema|check> ...")
	}
}

func adapterCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt adapter <list|describe|doctor> ...")
	}
	fs := newFlagSet("adapter " + args[0])
	workspace := fs.String("workspace", ".", "control workspace")
	configPath := fs.String("config", ".takt/config.yaml", "config path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--config": true, "--json": false})); err != nil {
		return err
	}
	services, err := localServices(*workspace, *configPath)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: takt adapter list [--workspace dir] [--config path]")
		}
		rows, err := services.Adapters.List()
		if err != nil {
			return err
		}
		return printResult(*jsonOut, rows)
	case "describe":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt adapter describe <name> [--workspace dir] [--config path]")
		}
		declaration, err := services.Adapters.Describe(context.Background(), fs.Arg(0))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, map[string]any{"name": fs.Arg(0), "declaration": declaration})
	case "doctor":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt adapter doctor <name> [--workspace dir] [--config path]")
		}
		report, err := services.Adapters.Doctor(context.Background(), fs.Arg(0))
		if err != nil {
			return err
		}
		if err := printResult(*jsonOut, report); err != nil {
			return err
		}
		if report.Status == "error" {
			return fmt.Errorf("adapter doctor found configuration problems")
		}
		return nil
	default:
		return fmt.Errorf("usage: takt adapter <list|describe|doctor> ...")
	}
}

func packageCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt package <install|update|uninstall|list|sync|doctor|sign> ...")
	}
	sub := args[0]
	fs := newFlagSet("package " + sub)
	workspace := fs.String("workspace", ".", "control workspace")
	scope := fs.String("scope", "", "package scope: global, corporate, or project")
	ref := fs.String("ref", "", "Git branch, tag, or revision used for updates")
	keyID := fs.String("key-id", "", "signature key id")
	keyFile := fs.String("key", "", "base64 Ed25519 private key file")
	configPath := fs.String("config", ".takt/config.yaml", "config used to verify package adapter requirements")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args[1:], map[string]bool{"--workspace": true, "--scope": true, "--ref": true, "--key-id": true, "--key": true, "--config": true, "--json": false})); err != nil {
		return err
	}
	services, err := localServices(*workspace, *configPath)
	if err != nil {
		return err
	}
	packages := services.Packages
	ctx := context.Background()
	switch sub {
	case "install":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt package install <local-path|git-url> [--scope project|corporate|global] [--ref ref]")
		}
		s := *scope
		if s == "" {
			s = "project"
		}
		entry, err := packages.Install(ctx, fs.Arg(0), application.PackageInstallOptions{Scope: s, Ref: *ref})
		if err != nil {
			return err
		}
		return printResult(*jsonOut, entry)
	case "update":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt package update <name> [--scope scope] [--ref ref]")
		}
		entry, err := packages.Update(ctx, fs.Arg(0), *scope, *ref)
		if err != nil {
			return err
		}
		return printResult(*jsonOut, entry)
	case "uninstall":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt package uninstall <name> [--scope scope]")
		}
		if err := packages.Uninstall(fs.Arg(0), *scope); err != nil {
			return err
		}
		return printResult(*jsonOut, map[string]any{"removed": fs.Arg(0), "scope": *scope})
	case "list":
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: takt package list [--workspace dir]")
		}
		values, err := packages.List()
		if err != nil {
			return err
		}
		return printResult(*jsonOut, values)
	case "sync":
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: takt package sync [--workspace dir]")
		}
		report, err := packages.Sync(ctx)
		if err != nil {
			return err
		}
		return printResult(*jsonOut, report)
	case "doctor":
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: takt package doctor [--workspace dir] [--config path]")
		}
		report, err := packages.Doctor(ctx)
		if err != nil {
			return err
		}
		return printResult(*jsonOut, report)
	case "sign":
		if fs.NArg() != 1 || strings.TrimSpace(*keyID) == "" || strings.TrimSpace(*keyFile) == "" {
			return fmt.Errorf("usage: takt package sign <package-dir> --key-id id --key private-key-file")
		}
		if err := packages.Sign(fs.Arg(0), *keyID, *keyFile); err != nil {
			return err
		}
		return printResult(*jsonOut, map[string]any{"signed": fs.Arg(0), "key_id": *keyID})
	default:
		return fmt.Errorf("usage: takt package <install|update|uninstall|list|sync|doctor|sign> ...")
	}
}
