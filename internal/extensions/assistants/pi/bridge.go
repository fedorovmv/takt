package pi

import core "takt/internal/assistant"

type Request = core.Request
type Result = core.Result
type ProtocolModel = core.ProtocolModel
type ProtocolUsage = core.ProtocolUsage
type Policy = core.Policy
type CapabilityDeclaration = core.CapabilityDeclaration

type limitedBuffer = core.LimitedBuffer

const (
	CapabilityToolPolicy        = core.CapabilityToolPolicy
	CapabilitySkills            = core.CapabilitySkills
	CapabilitySandboxFilesystem = core.CapabilitySandboxFilesystem
	EventSessionStarted         = core.EventSessionStarted
	EventSessionResumed         = core.EventSessionResumed
	EventMessage                = core.EventMessage
	EventUsage                  = core.EventUsage
	EventDiagnostic             = core.EventDiagnostic
	EventCompleted              = core.EventCompleted
	EventFailed                 = core.EventFailed
)

var (
	effectiveSession  = core.EffectiveSession
	renderArg         = core.RenderArg
	compactJSON       = core.CompactJSON
	newLimitedBuffer  = core.NewLimitedBuffer
	mergeCapabilities = core.MergeCapabilities
)
