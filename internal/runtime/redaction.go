package runtime

import (
	"takt/internal/redact"
	"takt/internal/store"
)

func cloneRunStateForPersistence(state *store.RunState) (*store.RunState, error) {
	return redact.CloneRunState(state)
}

func redactRunState(r *redact.Redactor, state *store.RunState) {
	redact.RedactRunState(r, state)
}
