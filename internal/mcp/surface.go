package mcp

import (
	"fmt"
	"strings"
)

type Surface string

const (
	SurfaceAll      Surface = "all"
	SurfaceAgent    Surface = "agent"
	SurfaceHost     Surface = "host"
	SurfaceWorker   Surface = "worker"
	SurfaceOperator Surface = "operator"
)

func ParseSurface(value string) (Surface, error) {
	surface := Surface(strings.ToLower(strings.TrimSpace(value)))
	if surface == "" {
		surface = SurfaceAgent
	}
	switch surface {
	case SurfaceAll, SurfaceAgent, SurfaceHost, SurfaceWorker, SurfaceOperator:
		return surface, nil
	default:
		return "", fmt.Errorf("unsupported MCP surface %q; use agent, host, worker, operator, or all", value)
	}
}

func surfaceAllows(surface Surface, name string) bool {
	if surface == SurfaceAll {
		return true
	}
	switch surface {
	case SurfaceAgent:
		return strings.HasPrefix(name, "takt.task.")
	case SurfaceHost:
		return strings.HasPrefix(name, "takt.host.")
	case SurfaceWorker:
		return strings.HasPrefix(name, "takt.node.")
	case SurfaceOperator:
		return !strings.HasPrefix(name, "takt.host.") && !strings.HasPrefix(name, "takt.node.") && !strings.HasPrefix(name, "takt.task.")
	default:
		return false
	}
}

func surfaceInstructions(surface Surface) string {
	switch surface {
	case SurfaceAgent:
		return "Use takt.task.start for a user task, takt.task.status to inspect it, takt.task.respond for approval, steering or continuation, takt.task.stop to stop it, and takt.task.explain only when detailed routing or workflow information is needed."
	case SurfaceHost:
		return "This surface is reserved for coding-agent host extensions that bind a host session to Takt and enforce managed mode."
	case SurfaceWorker:
		return "This surface is reserved for external workers. Claim durable nodes, stream normalized events and tool lifecycle records, and submit exactly one terminal node result."
	case SurfaceOperator:
		return "Use workflow, plan, run, block and notification tools for explicit operation and diagnostics of the local Takt runtime."
	default:
		return "Full compatibility surface containing agent, host, worker and operator tools. Prefer a role-specific surface for production integrations."
	}
}
