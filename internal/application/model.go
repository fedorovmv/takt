package application

import (
	"takt/internal/dynamicplan"
	"takt/internal/hostcontrol"
	"takt/internal/store"
)

type RunState = store.RunState
type Event = store.Event

const (
	RunRunning   = store.RunRunning
	RunWaiting   = store.RunWaiting
	RunPaused    = store.RunPaused
	RunCompleted = store.RunCompleted
	RunFailed    = store.RunFailed
	RunCancelled = store.RunCancelled
	RunAbandoned = store.RunAbandoned
)

// Transport-facing aliases keep CLI and RPC adapters dependent on the
// application boundary rather than concrete plan/host persistence packages.
type PlanRecord = dynamicplan.Record
type HostCapabilities = hostcontrol.Capabilities
type HostSession = hostcontrol.Session

const HostEnforcementAdvisory = hostcontrol.EnforcementAdvisory
