package localsandbox

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type Policy struct {
	Enforcement string
	Filesystem  string
	Network     string
}

type Decision struct {
	Requested string `json:"requested,omitempty"`
	Status    string `json:"status,omitempty"` // enforced | degraded | not_requested
	Backend   string `json:"backend,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func CommandContext(ctx context.Context, workspace string, policy Policy, name string, args ...string) (*exec.Cmd, Decision, error) {
	enforcement := strings.TrimSpace(policy.Enforcement)
	if enforcement == "" {
		return exec.CommandContext(ctx, name, args...), Decision{Status: "not_requested"}, nil
	}
	if enforcement != "required" && enforcement != "optional" {
		return nil, Decision{}, fmt.Errorf("sandbox enforcement must be required or optional")
	}
	decision := Decision{Requested: enforcement}
	backend, path := availableBackend()
	if backend == "" {
		decision.Status = "degraded"
		decision.Reason = "no supported OS sandbox backend found"
		if enforcement == "required" {
			return nil, decision, fmt.Errorf("OS sandbox required but unavailable")
		}
		return exec.CommandContext(ctx, name, args...), decision, nil
	}
	decision.Status = "enforced"
	decision.Backend = backend
	cmd, err := commandForBackend(ctx, backend, path, workspace, policy, name, args...)
	if err != nil {
		return nil, decision, err
	}
	return cmd, decision, nil
}

func commandForBackend(ctx context.Context, backend, path, workspace string, policy Policy, name string, args ...string) (*exec.Cmd, error) {
	switch backend {
	case "bubblewrap":
		wrapper := []string{"--die-with-parent", "--new-session"}
		if strings.TrimSpace(policy.Filesystem) == "read_only" {
			wrapper = append(wrapper, "--ro-bind", "/", "/")
		} else {
			wrapper = append(wrapper, "--bind", "/", "/")
		}
		if strings.TrimSpace(policy.Network) == "deny" {
			wrapper = append(wrapper, "--unshare-net")
		}
		wrapper = append(wrapper, "--chdir", workspace, "--", name)
		wrapper = append(wrapper, args...)
		return exec.CommandContext(ctx, path, wrapper...), nil
	case "sandbox-exec":
		profile := "(version 1) (allow default)"
		if strings.TrimSpace(policy.Network) == "deny" {
			profile += " (deny network*)"
		}
		if strings.TrimSpace(policy.Filesystem) == "read_only" {
			profile += " (deny file-write*)"
		}
		wrapper := []string{"-p", profile, name}
		wrapper = append(wrapper, args...)
		return exec.CommandContext(ctx, path, wrapper...), nil
	default:
		return nil, fmt.Errorf("unsupported OS sandbox backend %q", backend)
	}
}

func availableBackend() (string, string) {
	if runtime.GOOS == "linux" {
		if path, err := exec.LookPath("bwrap"); err == nil {
			return "bubblewrap", path
		}
	}
	if runtime.GOOS == "darwin" {
		if path, err := exec.LookPath("sandbox-exec"); err == nil {
			return "sandbox-exec", path
		}
	}
	return "", ""
}

func Available() (backend string, ok bool) {
	backend, _ = availableBackend()
	return backend, backend != ""
}
