package application

import (
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
