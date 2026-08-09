package notification

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"takt/internal/store"
	"takt/internal/yamlcodec"
)

type Config struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Events     []string `json:"events,omitempty"`
	Sinks      []Sink   `json:"sinks,omitempty"`
	InboxLimit int      `json:"inbox_limit,omitempty"`
}

type Sink struct {
	Type    string   `json:"type"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Timeout string   `json:"timeout,omitempty"`
}

type Item struct {
	ID             string            `json:"id"`
	Event          string            `json:"event"`
	RunID          string            `json:"run_id,omitempty"`
	Status         string            `json:"status,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	Message        string            `json:"message"`
	Command        string            `json:"command,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	AcknowledgedAt *time.Time        `json:"acknowledged_at,omitempty"`
	Deliveries     map[string]string `json:"deliveries,omitempty"`
}

type Snapshot struct {
	Initialized bool                   `json:"initialized,omitempty"`
	Runs        map[string]RunSnapshot `json:"runs"`
}

type RunSnapshot struct {
	Status          string `json:"status"`
	AttentionReason string `json:"attention_reason,omitempty"`
	AttentionKey    string `json:"attention_key,omitempty"`
	Revision        uint64 `json:"revision"`
	RecoveryCount   int    `json:"recovery_count,omitempty"`
}

type Dispatcher struct {
	Workspace string
}

func (d Dispatcher) Root() string { return filepath.Join(d.Workspace, ".takt", "notifications") }
func (d Dispatcher) ConfigPath() string {
	return filepath.Join(d.Workspace, ".takt", "notifications.yaml")
}
func (d Dispatcher) SnapshotPath() string { return filepath.Join(d.Root(), "state.json") }
func (d Dispatcher) InboxDir() string     { return filepath.Join(d.Root(), "inbox") }

func (d Dispatcher) LoadConfig() (Config, error) {
	config := Config{APIVersion: "takt/v1alpha1", Kind: "NotificationConfig", Events: []string{"approval.required", "question.required", "tool_approval.required", "attention.required", "run.completed", "run.failed", "run.paused", "run.abandoned", "worker.lost"}, InboxLimit: 500}
	raw, err := os.ReadFile(d.ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return Config{}, err
	}
	if err := yamlcodec.Unmarshal(raw, &config); err != nil {
		return Config{}, fmt.Errorf("decode notification config: %w", err)
	}
	if config.Kind != "" && config.Kind != "NotificationConfig" {
		return Config{}, fmt.Errorf("notification config kind must be NotificationConfig")
	}
	if config.InboxLimit == 0 {
		config.InboxLimit = 500
	}
	if config.InboxLimit < 50 || config.InboxLimit > 10000 {
		return Config{}, fmt.Errorf("notification inbox_limit must be between 50 and 10000")
	}
	for i := range config.Sinks {
		config.Sinks[i].Type = strings.ToLower(strings.TrimSpace(config.Sinks[i].Type))
		switch config.Sinks[i].Type {
		case "desktop", "process", "coding_agent_host":
		default:
			return Config{}, fmt.Errorf("unsupported notification sink %q", config.Sinks[i].Type)
		}
		if config.Sinks[i].Type == "process" && strings.TrimSpace(config.Sinks[i].Command) == "" {
			return Config{}, fmt.Errorf("process notification sink requires command")
		}
		if _, err := notificationTimeout(config.Sinks[i]); err != nil {
			return Config{}, fmt.Errorf("notification sink %d: %w", i, err)
		}
	}
	return config, nil
}

func (d Dispatcher) Dispatch() ([]Item, error) {
	release, err := d.acquireDispatchLock()
	if err != nil {
		return nil, err
	}
	defer release()
	config, err := d.LoadConfig()
	if err != nil {
		return nil, err
	}
	snapshot, err := d.loadSnapshot()
	if err != nil {
		return nil, err
	}
	st := store.FS{Workspace: d.Workspace}
	ids, err := st.ListRunIDs()
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, event := range config.Events {
		allowed[event] = true
	}
	var emitted []Item
	for _, id := range ids {
		run, loadErr := st.Load(id)
		if errors.Is(loadErr, os.ErrNotExist) {
			// Detached start can expose the Run directory just before state.json
			// is atomically published. Skip only that transient state; malformed
			// published records remain fatal.
			continue
		}
		if loadErr != nil {
			return nil, loadErr
		}
		currentAttention, currentAttentionKey := attentionState(run)
		previous := snapshot.Runs[id]
		var candidates []Item
		if snapshot.Initialized && previous.Status != run.Status {
			event := terminalEvent(run.Status)
			if event != "" {
				candidates = append(candidates, newItem(event, run, run.ErrorCode, statusMessage(run), "status:"+run.Status))
			}
		}
		if snapshot.Initialized && currentAttention != "" && currentAttentionKey != previous.AttentionKey {
			candidates = append(candidates, newItem(attentionEvent(currentAttention), run, currentAttention, attentionMessage(run), "attention:"+currentAttentionKey))
		}
		if snapshot.Initialized && run.RecoveryCount > previous.RecoveryCount {
			candidates = append(candidates, newItem("worker.lost", run, "worker_lost", fmt.Sprintf("Run %s recovered after losing its executor", run.ID), fmt.Sprintf("recovery:%d", run.RecoveryCount)))
		}
		for _, item := range candidates {
			if !allowed[item.Event] {
				continue
			}
			created, err := d.persistAndDeliver(&item, config.Sinks)
			if err != nil {
				return nil, err
			}
			if created {
				emitted = append(emitted, item)
			}
		}
		snapshot.Runs[id] = RunSnapshot{Status: run.Status, AttentionReason: currentAttention, AttentionKey: currentAttentionKey, Revision: run.Revision, RecoveryCount: run.RecoveryCount}
	}
	snapshot.Initialized = true
	if err := d.saveSnapshot(snapshot); err != nil {
		return nil, err
	}
	if err := d.pruneInbox(config.InboxLimit); err != nil {
		return nil, err
	}
	return emitted, nil
}

func (d Dispatcher) Test(message string) (*Item, error) {
	config, err := d.LoadConfig()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(message) == "" {
		message = "Takt notification test"
	}
	item := Item{ID: fmt.Sprintf("notice-%d-test", time.Now().UTC().UnixNano()), Event: "notification.test", Message: message, CreatedAt: time.Now().UTC(), Deliveries: map[string]string{}}
	if _, err := d.persistAndDeliver(&item, config.Sinks); err != nil {
		return nil, err
	}
	return &item, nil
}

func (d Dispatcher) List(unreadOnly bool, limit int) ([]Item, error) {
	entries, err := os.ReadDir(d.InboxDir())
	if errors.Is(err, os.ErrNotExist) {
		return []Item{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(d.InboxDir(), entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		var item Item
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		if unreadOnly && item.AcknowledgedAt != nil {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (d Dispatcher) Ack(id string) (*Item, error) {
	if !strings.HasPrefix(id, "notice-") || strings.ContainsAny(id, `/\\`) || strings.Contains(id, "..") {
		return nil, fmt.Errorf("invalid notification id %q", id)
	}
	path := filepath.Join(d.InboxDir(), id+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var item Item
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	item.AcknowledgedAt = &now
	if err := writeJSONAtomic(path, item, 0o600); err != nil {
		return nil, err
	}
	return &item, nil
}

func (d Dispatcher) persistAndDeliver(item *Item, sinks []Sink) (bool, error) {
	if item.Deliveries == nil {
		item.Deliveries = map[string]string{}
	}
	if err := os.MkdirAll(d.InboxDir(), 0o700); err != nil {
		return false, err
	}
	path := filepath.Join(d.InboxDir(), item.ID+".json")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	for i, sink := range sinks {
		key := fmt.Sprintf("%s:%d", sink.Type, i)
		var err error
		switch sink.Type {
		case "coding_agent_host":
			item.Deliveries[key] = "queued"
			continue
		case "desktop":
			err = deliverDesktop(sink, *item)
		case "process":
			err = deliverProcess(sink, *item)
		}
		if err != nil {
			item.Deliveries[key] = "failed: " + err.Error()
		} else {
			item.Deliveries[key] = "delivered"
		}
	}
	if err := writeJSONAtomic(path, item, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func deliverProcess(sink Sink, item Item) error {
	raw, err := json.Marshal(item)
	if err != nil {
		return err
	}
	timeout, err := notificationTimeout(sink)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, sink.Command, sink.Args...)
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Stdin = strings.NewReader(string(raw) + "\n")
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("notification sink timed out after %s", timeout)
	}
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func deliverDesktop(sink Sink, item Item) error {
	title := "Takt: " + item.Event
	body := item.Message
	var cmd *exec.Cmd
	timeout, err := notificationTimeout(sink)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	switch goruntime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %s with title %s", appleQuote(body), appleQuote(title))
		cmd = exec.CommandContext(ctx, "osascript", "-e", script)
	case "linux":
		cmd = exec.CommandContext(ctx, "notify-send", title, body)
	default:
		return fmt.Errorf("desktop notifications are unsupported on %s", goruntime.GOOS)
	}
	cmd.WaitDelay = 250 * time.Millisecond
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("notification sink timed out after %s", timeout)
	}
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func appleQuote(value string) string {
	return fmt.Sprintf("%q", strings.ReplaceAll(value, "\\", "\\\\"))
}

func newItem(event string, run *store.RunState, reason, message, dedupe string) Item {
	sum := sha256.Sum256([]byte(run.ID + "\x00" + event + "\x00" + dedupe))
	id := "notice-" + hex.EncodeToString(sum[:12])
	return Item{ID: id, Event: event, RunID: run.ID, Status: run.Status, Reason: reason, Message: message, Command: "takt run summary " + run.ID, CreatedAt: time.Now().UTC(), Deliveries: map[string]string{}}
}

func attentionEvent(reason string) string {
	switch reason {
	case "approval":
		return "approval.required"
	case "question":
		return "question.required"
	case "tool_approval":
		return "tool_approval.required"
	default:
		return "attention.required"
	}
}

func terminalEvent(status string) string {
	switch status {
	case store.RunCompleted:
		return "run.completed"
	case store.RunFailed:
		return "run.failed"
	case store.RunPaused:
		return "run.paused"
	case store.RunAbandoned:
		return "run.abandoned"
	default:
		return ""
	}
}

func statusMessage(run *store.RunState) string {
	if run.Error != "" {
		return fmt.Sprintf("Run %s is %s: %s", run.ID, run.Status, run.Error)
	}
	return fmt.Sprintf("Run %s is %s", run.ID, run.Status)
}

func attentionState(run *store.RunState) (string, string) {
	if run.Status == store.RunWaiting && run.Waiting != nil && run.Waiting.Kind != "child_run" {
		if run.Waiting.Kind != "" {
			return run.Waiting.Kind, fmt.Sprintf("%s:%s:%d", run.Waiting.Kind, run.Waiting.NodeID, run.Revision)
		}
		return "approval", fmt.Sprintf("approval:%s:%d", run.Waiting.NodeID, run.Revision)
	}
	for nodeID, node := range run.Nodes {
		if node == nil || node.External == nil {
			continue
		}
		for _, call := range node.External.ToolCalls {
			if call != nil && call.ApprovalNeeded && (call.Status == "requested" || call.Status == "waiting") {
				return "tool_approval", fmt.Sprintf("tool_approval:%s:%s:%d", nodeID, call.CallID, run.Revision)
			}
		}
	}
	return "", ""
}

func attentionMessage(run *store.RunState) string {
	if run.Waiting != nil && run.Waiting.Message != "" {
		return run.Waiting.Message
	}
	return fmt.Sprintf("Run %s requires attention", run.ID)
}

func (d Dispatcher) loadSnapshot() (Snapshot, error) {
	raw, err := os.ReadFile(d.SnapshotPath())
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{Runs: map[string]RunSnapshot{}}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	var value Snapshot
	if err := json.Unmarshal(raw, &value); err != nil {
		return Snapshot{}, err
	}
	if value.Runs == nil {
		value.Runs = map[string]RunSnapshot{}
	}
	return value, nil
}

func (d Dispatcher) saveSnapshot(value Snapshot) error {
	return writeJSONAtomic(d.SnapshotPath(), value, 0o600)
}

func notificationTimeout(sink Sink) (time.Duration, error) {
	if strings.TrimSpace(sink.Timeout) == "" {
		return 10 * time.Second, nil
	}
	value, err := time.ParseDuration(sink.Timeout)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout %q: %w", sink.Timeout, err)
	}
	if value < time.Second || value > 2*time.Minute {
		return 0, fmt.Errorf("timeout must be between 1s and 2m")
	}
	return value, nil
}

func (d Dispatcher) acquireDispatchLock() (func(), error) {
	if err := os.MkdirAll(d.Root(), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(d.Root(), ".dispatch.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func (d Dispatcher) pruneInbox(limit int) error {
	if limit <= 0 {
		return nil
	}
	items, err := d.List(false, 0)
	if err != nil {
		return err
	}
	if len(items) <= limit {
		return nil
	}
	// List is newest-first. Prefer deleting acknowledged entries among the
	// overflow, then oldest unread entries if the cap is still exceeded.
	overflow := len(items) - limit
	remove := make([]Item, 0, overflow)
	for i := len(items) - 1; i >= 0 && len(remove) < overflow; i-- {
		if items[i].AcknowledgedAt != nil {
			remove = append(remove, items[i])
		}
	}
	if len(remove) < overflow {
		selected := map[string]bool{}
		for _, item := range remove {
			selected[item.ID] = true
		}
		for i := len(items) - 1; i >= 0 && len(remove) < overflow; i-- {
			if !selected[items[i].ID] {
				remove = append(remove, items[i])
			}
		}
	}
	for _, item := range remove {
		if err := os.Remove(filepath.Join(d.InboxDir(), item.ID+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".notice-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	writer := bufio.NewWriter(tmp)
	if _, err := writer.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := writer.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
