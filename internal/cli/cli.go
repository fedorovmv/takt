package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"takt/internal/apperror"
	"takt/internal/application"
	"takt/internal/bootstrap"
	"takt/internal/experimental/dynamicflow"
	experimentallearning "takt/internal/experimental/learning"
	"takt/internal/extensions"
	"takt/internal/maintenance"
	"takt/internal/tooling"
	"takt/internal/version"
)

func Run(args []string) error { return RunContext(context.Background(), args) }

func RunContext(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "init":
		return initCmd(ctx, args[1:])
	case "validate":
		return validateCmd(ctx, args[1:])
	case "run":
		return runDispatchCmd(ctx, args[1:])
	case "task":
		return taskCmd(ctx, args[1:])
	case "learn":
		return learnCmd(ctx, args[1:])
	case "runs":
		return runsCmd(ctx, args[1:])
	case "attention":
		return attentionCmd(ctx, args[1:])
	case "notify":
		return notifyCmd(ctx, args[1:])
	case "plan":
		return planCmd(ctx, args[1:])
	case "execute":
		return executeCmd(ctx, args[1:])
	case "steer":
		return steerCmd(ctx, args[1:])
	case "host":
		return hostCmd(ctx, args[1:])
	case "workflow":
		return workflowCmd(ctx, args[1:])
	case "block":
		return blockCmd(ctx, args[1:])
	case "adapter":
		return adapterCmd(ctx, args[1:])
	case "compatibility":
		return compatibilityCmd(ctx, args[1:])
	case "package":
		return packageCmd(ctx, args[1:])
	case "answer":
		return answerCmd(ctx, args[1:])
	case "resume":
		return resumeCmd(ctx, args[1:])
	case "status":
		return statusCmd(ctx, args[1:])
	case "children":
		return childrenCmd(ctx, args[1:])
	case "artifacts":
		return artifactsCmd(ctx, args[1:])
	case "cancel":
		return cancelCmd(ctx, args[1:])
	case "worktree":
		return worktreeCmd(ctx, args[1:])
	case "command":
		return commandCmd(ctx, args[1:])
	case "eval":
		return evalCmd(ctx, args[1:])
	case "mcp":
		return mcpCmd(ctx, args[1:])
	case "daemon":
		return daemonCmd(ctx, args[1:])
	case "events":
		return eventsCmd(ctx, args[1:])
	case "version":
		fmt.Println("takt v" + version.Value)
		return nil
	default:
		return usage()
	}
}

func absoluteIfExistingFile(value string) (string, error) {
	if value == "" || filepath.IsAbs(value) {
		return value, nil
	}
	info, err := os.Stat(value)
	if err != nil || info.IsDir() {
		return value, nil
	}
	return filepath.Abs(value)
}

type serviceView struct {
	RunService       *application.RunService
	CatalogService   *application.CatalogService
	AuthoringService *application.AuthoringService
	WorktreeService  *application.WorktreeService
	CommandService   *application.CommandService
	ExternalService  *application.ExternalService
	PlanService      *dynamicflow.PlanService
	TaskService      *dynamicflow.TaskService
	ForkService      *dynamicflow.ForkService
	HostService      *dynamicflow.HostService
	Adapters         *extensions.AdapterService
	Packages         *extensions.PackageService
	Notifications    *extensions.NotificationService
	Blocks           *extensions.BlockService
	Compatibility    *tooling.CompatibilityService
	Evaluation       *tooling.EvaluationService
	Learning         *experimentallearning.Service
	Maintenance      *maintenance.Service
}

func localServices(workspace, configPath string) (*serviceView, error) {
	app, err := bootstrap.New(workspace, configPath)
	if err != nil {
		return nil, err
	}
	return &serviceView{
		RunService: app.Core.RunService, CatalogService: app.Core.CatalogService,
		AuthoringService: app.Core.AuthoringService, WorktreeService: app.Core.WorktreeService,
		CommandService: app.Core.CommandService, ExternalService: app.Core.ExternalService,
		PlanService: app.Experimental.PlanService, TaskService: app.Experimental.TaskService,
		ForkService: app.Experimental.ForkService, HostService: app.Experimental.HostService,
		Adapters: app.Extensions.Adapters, Packages: app.Extensions.Packages,
		Notifications: app.Extensions.Notifications, Blocks: app.Extensions.Blocks,
		Compatibility: app.Tooling.Compatibility, Evaluation: app.Tooling.Evaluation,
		Learning: app.Learning, Maintenance: app.Maintenance,
	}, nil
}

func controlService(workspace string) (*serviceView, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	return localServices(abs, "")
}

func answerCmd(ctx context.Context, args []string) error {
	fs := newFlagSet("answer")
	workspace := fs.String("workspace", ".", "workspace")
	value := fs.String("value", "", "answer value")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--value": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: takt answer <run-id> <node-id> --value text")
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	state, err := service.RunService.Answer(ctx, fs.Arg(0), fs.Arg(1), *value)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, state)
}

func resumeCmd(ctx context.Context, args []string) error {
	fs := newFlagSet("resume")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt resume <run-id>")
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	state, err := service.RunService.Resume(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	return printResult(*jsonOut, state)
}

func statusCmd(ctx context.Context, args []string) error {
	fs := newFlagSet("status")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt status <run-id>")
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	state, err := service.RunService.GetRun(fs.Arg(0))
	if err != nil {
		return err
	}
	return printResult(*jsonOut, state)
}

func childrenCmd(ctx context.Context, args []string) error {
	fs := newFlagSet("children")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt children <run-id>")
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	children, err := service.RunService.Children(fs.Arg(0))
	if err != nil {
		return err
	}
	return printResult(*jsonOut, children)
}

func artifactsCmd(ctx context.Context, args []string) error {
	fs := newFlagSet("artifacts")
	workspace := fs.String("workspace", ".", "workspace")
	nodeID := fs.String("node", "", "filter by producer node id")
	artifactType := fs.String("type", "", "filter by semantic artifact type")
	recursive := fs.Bool("recursive", false, "include artifacts from all descendant Runs")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--node": true, "--type": true, "--recursive": false, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt artifacts <run-id> [--node id] [--type type] [--recursive]")
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	result, err := service.RunService.Artifacts(fs.Arg(0), application.ArtifactQuery{NodeID: *nodeID, Type: *artifactType, Recursive: *recursive})
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
}

func cancelCmd(ctx context.Context, args []string) error {
	fs := newFlagSet("cancel")
	workspace := fs.String("workspace", ".", "workspace")
	reason := fs.String("reason", "cancelled by user", "cancellation reason")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--reason": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt cancel <run-id> [--reason text]")
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	result, err := service.RunService.Cancel(ctx, fs.Arg(0), *reason)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
}

type worktreeListEntry struct {
	RunID              string `json:"run_id"`
	RunStatus          string `json:"run_status"`
	Path               string `json:"path"`
	Branch             string `json:"branch"`
	BaseCommit         string `json:"base_commit,omitempty"`
	Dirty              bool   `json:"dirty,omitempty"`
	Removed            bool   `json:"removed,omitempty"`
	RetainedReason     string `json:"retained_reason,omitempty"`
	ExecutionWorkspace string `json:"execution_workspace,omitempty"`
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func interspersed(args []string, takesValue map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name := arg
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			name = name[:idx]
			continue
		}
		if takesValue[name] && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

func flagPresent(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func readInput(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if b, err := os.ReadFile(v); err == nil {
		return string(b), nil
	}
	return v, nil
}
func printResult(jsonOut bool, value any) error {
	if state, ok := value.(*application.RunState); ok {
		value = state.PublicView()
	}
	if jsonOut {
		b, err := json.MarshalIndent(map[string]any{"ok": true, "result": value}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("%+v\n", value)
	return nil
}

func WantsJSON(args []string) bool {
	value := false
	if len(args) > 0 {
		switch args[0] {
		case "run", "plan", "execute", "steer", "answer", "resume", "status", "children", "artifacts", "worktree", "eval", "adapter", "compatibility", "learn":
			value = true
		case "command":
			value = len(args) > 1 && args[1] == "run"
		}
	}
	for _, arg := range args {
		if arg == "--json" || arg == "--json=true" {
			value = true
		}
		if arg == "--json=false" {
			value = false
		}
	}
	return value
}

func PrintErrorJSON(err error) error {
	description := apperror.Describe(err)
	payload := map[string]any{"ok": false, "error": map[string]any{
		"code": description.Code, "message": err.Error(), "retryable": description.Retryable, "details": description.Details,
	}}
	b, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	fmt.Fprintln(os.Stderr, string(b))
	return nil
}

func usage() error {
	return fmt.Errorf(`usage: takt <command>

stable: init validate run runs workflow answer resume status children artifacts events cancel worktree command adapter package mcp daemon version
extensions: block notify attention
experimental: task plan execute steer host learn
tooling: eval compatibility`)
}
