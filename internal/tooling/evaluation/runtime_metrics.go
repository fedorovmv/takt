package evaluation

import (
	"time"

	"takt/internal/store"
)

type eventReader interface {
	ReadEvents(id string, afterRevision uint64, limit int) ([]store.Event, error)
}

func applyRuntimeMetrics(record *RunRecord, state *store.RunState, repository store.Repository, qualityNode string) {
	reader, ok := repository.(eventReader)
	if !ok || state == nil {
		return
	}
	events, err := reader.ReadEvents(state.ID, 0, 100000)
	if err != nil {
		return
	}
	applyRuntimeMetricsFromEvents(record, state, events, qualityNode)
}

func applyRuntimeMetricsFromEvents(record *RunRecord, state *store.RunState, events []store.Event, qualityNode string) {
	if record == nil || state == nil {
		return
	}
	var qualityCompletedAt time.Time
	for _, event := range events {
		switch event.Type {
		case "node.retry.scheduled", "node.retry", "provider.retry.scheduled":
			record.RetryScheduled++
			if fingerprint, ok := event.Data["fingerprint"].(string); ok && fingerprint != "" {
				record.RetryFingerprints = append(record.RetryFingerprints, fingerprint)
			}
		case "node.completed":
			if qualityNode != "" && event.NodeID == qualityNode && qualityCompletedAt.IsZero() {
				qualityCompletedAt = event.Time
			}
		}
	}
	if qualitySucceeded(*record) && !qualityCompletedAt.IsZero() && !state.CreatedAt.IsZero() {
		value := qualityCompletedAt.Sub(state.CreatedAt).Milliseconds()
		if value < 0 {
			value = 0
		}
		record.TimeToValidMS = &value
	}
}
