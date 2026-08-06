package runtime

import (
	"fmt"
	"sync"
	"time"

	"takt/internal/assistant"
	"takt/internal/execution"
	"takt/internal/store"
)

const maxNormalizedMessageBytes = 64 * 1024

type assistantEventCollector struct {
	mu      sync.Mutex
	events  []assistant.Event
	err     error
	onEvent func()
}

func (c *assistantEventCollector) Emit(event assistant.Event) {
	if c.onEvent != nil {
		c.onEvent()
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if len(event.Message) > maxNormalizedMessageBytes {
		event.Message = event.Message[:maxNormalizedMessageBytes]
		if event.Data == nil {
			event.Data = map[string]any{}
		}
		event.Data["message_truncated"] = true
	}
	if err := assistant.ValidateEvent(event); err != nil {
		c.mu.Lock()
		if c.err == nil {
			c.err = err
		}
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	c.events = append(c.events, event)
	c.mu.Unlock()
}

func (c *assistantEventCollector) Result() ([]assistant.Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]assistant.Event(nil), c.events...), c.err
}

func collectAssistantResultEvents(collector *assistantEventCollector, resolved resolvedAssistantNode, result assistant.Result, runErr error) ([]assistant.Event, error) {
	if result.Output != "" {
		collector.Emit(assistant.Event{Type: assistant.EventMessage, Message: result.Output, Provider: resolved.Model.Provider, SessionID: result.SessionID})
	}
	if result.Usage != nil {
		collector.Emit(assistant.Event{Type: assistant.EventUsage, Usage: result.Usage, Provider: resolved.Model.Provider, SessionID: result.SessionID})
	}
	if runErr != nil {
		collector.Emit(assistant.Event{Type: assistant.EventFailed, Message: runErr.Error(), Provider: resolved.Model.Provider, SessionID: result.SessionID})
	} else {
		collector.Emit(assistant.Event{Type: assistant.EventCompleted, Provider: resolved.Model.Provider, SessionID: result.SessionID})
	}
	events, eventErr := collector.Result()
	if eventErr != nil {
		return events, &execution.Error{Kind: execution.KindProtocol, Op: "assistant events", Err: fmt.Errorf("invalid normalized assistant event: %w", eventErr)}
	}
	return events, nil
}

func (r *Runner) flushAssistantEvents(state *store.RunState, nodeID string, events []assistant.Event, source string) error {
	for _, event := range events {
		data := assistant.EventData(event)
		data["source"] = source
		if err := r.commit(state, "assistant."+event.Type, nodeID, data); err != nil {
			return err
		}
	}
	return nil
}
