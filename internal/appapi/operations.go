package appapi

// OperationDescriptor gives transports one canonical identity mapping for a
// registered application operation. Request decoding/defaults still belong to
// Registry; transport-specific titles and JSON Schema remain transport data.
type OperationDescriptor struct {
	ID      string
	MCPTool string
}

var canonicalOperationDescriptors = []OperationDescriptor{
	{ID: "task.start", MCPTool: "takt.task.start"},
	{ID: "task.status", MCPTool: "takt.task.status"},
	{ID: "task.respond", MCPTool: "takt.task.respond"},
	{ID: "task.stop", MCPTool: "takt.task.stop"},
	{ID: "task.explain", MCPTool: "takt.task.explain"},
	{ID: "workflow.list", MCPTool: "takt.workflow.list"},
	{ID: "workflow.describe", MCPTool: "takt.workflow.describe"},
	{ID: "block.list", MCPTool: "takt.block.list"},
	{ID: "block.describe", MCPTool: "takt.block.describe"},
	{ID: "host.begin", MCPTool: "takt.host.begin"},
	{ID: "host.confirm", MCPTool: "takt.host.confirm"},
	{ID: "host.get", MCPTool: "takt.host.get"},
	{ID: "host.find", MCPTool: "takt.host.find"},
	{ID: "host.guard_tool", MCPTool: "takt.host.guard_tool"},
	{ID: "host.guard_completion", MCPTool: "takt.host.guard_completion"},
	{ID: "host.release", MCPTool: "takt.host.release"},
	{ID: "plan.create", MCPTool: "takt.plan"},
	{ID: "plan.get", MCPTool: "takt.plan.get"},
	{ID: "plan.execute", MCPTool: "takt.execute"},
	{ID: "plan.steer", MCPTool: "takt.run.steer"},
	{ID: "plan.promote", MCPTool: "takt.plan.promote"},
	{ID: "run.start", MCPTool: "takt.run.start"},
	{ID: "run.get", MCPTool: "takt.run.get"},
	{ID: "run.list", MCPTool: "takt.run.list"},
	{ID: "run.attention", MCPTool: "takt.run.attention"},
	{ID: "run.summary", MCPTool: "takt.run.summary"},
	{ID: "run.pause", MCPTool: "takt.run.pause"},
	{ID: "run.resume_paused", MCPTool: "takt.run.resume_paused"},
	{ID: "run.retry", MCPTool: "takt.run.retry"},
	{ID: "run.fork", MCPTool: "takt.run.fork"},
	{ID: "run.abandon", MCPTool: "takt.run.abandon"},
	{ID: "run.recover", MCPTool: "takt.run.recover"},
	{ID: "notify.list", MCPTool: "takt.notify.list"},
	{ID: "notify.ack", MCPTool: "takt.notify.ack"},
	{ID: "notify.test", MCPTool: "takt.notify.test"},
	{ID: "run.resume", MCPTool: "takt.run.resume"},
	{ID: "run.answer", MCPTool: "takt.run.answer"},
	{ID: "run.cancel", MCPTool: "takt.run.cancel"},
	{ID: "run.children", MCPTool: "takt.run.children"},
	{ID: "run.artifacts", MCPTool: "takt.run.artifacts"},
	{ID: "run.events", MCPTool: "takt.run.events"},
}

var operationByMCPTool = func() map[string]string {
	out := make(map[string]string, len(canonicalOperationDescriptors))
	for _, descriptor := range canonicalOperationDescriptors {
		out[descriptor.MCPTool] = descriptor.ID
	}
	return out
}()

func CanonicalOperationForMCP(tool string) (string, bool) {
	id, ok := operationByMCPTool[tool]
	return id, ok
}

func CanonicalOperations() []OperationDescriptor {
	return append([]OperationDescriptor(nil), canonicalOperationDescriptors...)
}
