package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"takt/internal/control"
	"takt/internal/mcp"
	"takt/internal/notification"
	"takt/internal/store"
	"takt/internal/version"
)

const (
	APIRevision = "takt-daemon/v1alpha1"
	// unixSocketPathLimit is conservative across Darwin (104-byte sun_path
	// including the terminating NUL) and Linux.
	unixSocketPathLimit = 103
)

type Paths struct {
	Socket   string
	Metadata string
	Lock     string
	Log      string
}

func ResolvePaths(workspace, socketOverride string) (Paths, error) {
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return Paths{}, err
	}
	root := filepath.Join(workspace, ".takt")
	socket := strings.TrimSpace(socketOverride)
	explicitSocket := socket != ""
	if socket == "" {
		socket = filepath.Join(root, "daemon.sock")
	} else if !filepath.IsAbs(socket) {
		socket = filepath.Join(workspace, socket)
	}
	socket, err = filepath.Abs(socket)
	if err != nil {
		return Paths{}, err
	}
	if len([]byte(socket)) > unixSocketPathLimit {
		if explicitSocket {
			return Paths{}, fmt.Errorf("daemon Unix socket path is %d bytes; maximum portable length is %d: %s", len([]byte(socket)), unixSocketPathLimit, socket)
		}
		hash := sha256.Sum256([]byte(workspace))
		shortRoot := filepath.Join(os.TempDir(), "takt-daemon", hex.EncodeToString(hash[:8]))
		socket = filepath.Join(shortRoot, "daemon.sock")
		if len([]byte(socket)) > unixSocketPathLimit {
			return Paths{}, fmt.Errorf("temporary daemon Unix socket path is %d bytes; maximum portable length is %d: %s", len([]byte(socket)), unixSocketPathLimit, socket)
		}
	}
	return Paths{Socket: socket, Metadata: filepath.Join(root, "daemon.json"), Lock: filepath.Join(root, "daemon.lock"), Log: filepath.Join(root, "daemon.log")}, nil
}

type Metadata struct {
	API       string    `json:"api"`
	PID       int       `json:"pid"`
	Workspace string    `json:"workspace"`
	Socket    string    `json:"socket"`
	StartedAt time.Time `json:"started_at"`
	Version   string    `json:"version"`
}

type Options struct {
	Workspace  string
	ConfigPath string
	SocketPath string
	ErrOut     io.Writer
}

type Server struct {
	service  *control.Service
	mcp      *mcp.Server
	paths    Paths
	metadata Metadata
	errOut   io.Writer

	lockFile *os.File
	http     *http.Server
	stopOnce sync.Once
	stop     chan struct{}
}

func New(options Options) (*Server, error) {
	service, err := control.New(options.Workspace, options.ConfigPath)
	if err != nil {
		return nil, err
	}
	paths, err := ResolvePaths(service.Workspace, options.SocketPath)
	if err != nil {
		return nil, err
	}
	if options.ErrOut == nil {
		options.ErrOut = os.Stderr
	}
	server := &Server{service: service, paths: paths, errOut: options.ErrOut, stop: make(chan struct{})}
	server.mcp = mcp.New(service, nil, nil, options.ErrOut)
	server.metadata = Metadata{API: APIRevision, PID: os.Getpid(), Workspace: service.Workspace, Socket: paths.Socket, StartedAt: time.Now().UTC(), Version: version.Value}
	return server, nil
}

func (s *Server) Metadata() Metadata { return s.metadata }
func (s *Server) Paths() Paths       { return s.paths }

func (s *Server) Serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.paths.Socket), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.paths.Lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return fmt.Errorf("another Takt daemon is already active for %s", s.metadata.Workspace)
	}
	s.lockFile = lock
	defer s.release()

	if err := removeStaleSocket(s.paths.Socket); err != nil {
		return err
	}
	listener, err := net.Listen("unix", s.paths.Socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(s.paths.Socket, 0o600); err != nil {
		return err
	}
	if err := writeJSONAtomic(s.paths.Metadata, s.metadata, 0o600); err != nil {
		return err
	}
	if recovered, recoverErr := s.service.RecoverInterruptedRuns(context.Background()); recoverErr != nil {
		fmt.Fprintln(s.errOut, "daemon recovery:", recoverErr)
	} else if len(recovered.Recovered) > 0 {
		fmt.Fprintln(s.errOut, "daemon recovered interrupted Runs:", strings.Join(recovered.Recovered, ", "))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/rpc", s.handleRPC)
	mux.HandleFunc("/mcp", s.handleMCP)
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/shutdown", s.handleShutdown)
	s.http = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	serveErr := make(chan error, 1)
	go func() {
		err := s.http.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		s.monitorIdle(monitorCtx)
	}()
	defer func() {
		monitorCancel()
		<-monitorDone
	}()

	select {
	case <-ctx.Done():
		s.requestStop()
	case <-s.stop:
	case err := <-serveErr:
		return err
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.http.Shutdown(shutdownCtx)
	return <-serveErr
}

func (s *Server) requestStop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *Server) release() {
	_ = os.Remove(s.paths.Socket)
	_ = os.Remove(s.paths.Metadata)
	if s.lockFile != nil {
		_ = syscall.Flock(int(s.lockFile.Fd()), syscall.LOCK_UN)
		_ = s.lockFile.Close()
	}
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("daemon socket path exists and is not a Unix socket: %s", path)
	}
	conn, dialErr := net.DialTimeout("unix", path, 150*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("daemon socket is already accepting connections: %s", path)
	}
	return os.Remove(path)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	writeJSON(w, http.StatusOK, s.metadata)
}

type rpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	API    string    `json:"api"`
	Result any       `json:"result,omitempty"`
	Error  *apiError `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	var request rpcRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	result, err := s.call(r.Context(), request.Method, request.Params)
	if err != nil {
		writeJSON(w, http.StatusOK, rpcResponse{API: APIRevision, Error: &apiError{Code: "operation_failed", Message: err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, rpcResponse{API: APIRevision, Result: result})
}

func decodeParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func (s *Server) call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "workflow.list":
		var params struct {
			Profile string `json:"profile"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.ListWorkflows(params.Profile)
	case "workflow.describe":
		var params struct {
			Selector string `json:"selector"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.DescribeWorkflow(params.Selector)
	case "host.begin":
		var params control.HostBeginRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.BeginHostSession(ctx, params)
	case "host.confirm":
		var params control.HostConfirmRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		params.Detached = true
		return s.service.ConfirmHostSession(ctx, params)
	case "host.get":
		var params struct {
			SessionID string `json:"session_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.GetHostSession(params.SessionID)
	case "host.find":
		var params struct {
			Host          string `json:"host"`
			HostSessionID string `json:"host_session_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.FindHostSession(params.Host, params.HostSessionID)
	case "host.guard_tool":
		var params control.HostToolGuardRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.GuardHostTool(params)
	case "host.guard_completion":
		var params control.HostCompletionGuardRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.GuardHostCompletion(params)
	case "host.release":
		var params struct {
			SessionID string `json:"session_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.ReleaseHostSession(params.SessionID)
	case "plan.create":
		var params control.PlanRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.Plan(ctx, params)
	case "plan.get":
		var params struct {
			PlanID string `json:"plan_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.GetPlan(params.PlanID)
	case "plan.execute":
		var params control.ExecutePlanRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		params.Detached = true
		return s.service.ExecutePlan(ctx, params)
	case "plan.steer":
		var params control.SteerRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.Steer(ctx, params)
	case "plan.promote":
		var params struct {
			PlanID string `json:"plan_id"`
			Name   string `json:"name"`
			Force  bool   `json:"force,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.PromotePlanWithOptions(params.PlanID, params.Name, control.PromotePlanOptions{Force: params.Force})
	case "run.start":
		var params control.StartRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		params.Detached = true
		return s.service.Start(ctx, params)
	case "run.get":
		var params struct {
			RunID string `json:"run_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.GetRun(params.RunID)
	case "run.list":
		var params control.RunListRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.ListRuns(params)
	case "run.attention":
		return s.service.Attention()
	case "run.summary":
		var params struct {
			RunID     string `json:"run_id"`
			Recursive bool   `json:"recursive,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.Summary(params.RunID, params.Recursive)
	case "run.pause":
		var params struct {
			RunID string `json:"run_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.Pause(params.RunID)
	case "run.resume_paused":
		var params struct {
			RunID string `json:"run_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.ResumePaused(ctx, params.RunID, true)
	case "run.retry":
		var params control.RetryRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		params.Detached = true
		return s.service.Retry(ctx, params)
	case "run.fork":
		var params control.ForkRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		params.Detached = true
		return s.service.Fork(ctx, params)
	case "run.abandon":
		var params struct {
			RunID  string `json:"run_id"`
			Reason string `json:"reason,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.Abandon(params.RunID, params.Reason)
	case "run.recover":
		return s.service.RecoverInterruptedRuns(ctx)
	case "notify.list":
		var params struct {
			UnreadOnly bool `json:"unread_only,omitempty"`
			Limit      int  `json:"limit,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return (notification.Dispatcher{Workspace: s.metadata.Workspace}).List(params.UnreadOnly, params.Limit)
	case "notify.ack":
		var params struct {
			ID string `json:"id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return (notification.Dispatcher{Workspace: s.metadata.Workspace}).Ack(params.ID)
	case "notify.test":
		var params struct {
			Message string `json:"message,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return (notification.Dispatcher{Workspace: s.metadata.Workspace}).Test(params.Message)
	case "notify.dispatch":
		return (notification.Dispatcher{Workspace: s.metadata.Workspace}).Dispatch()
	case "run.resume":
		var params struct {
			RunID string `json:"run_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.Resume(ctx, params.RunID)
	case "run.answer":
		var params struct {
			RunID  string `json:"run_id"`
			NodeID string `json:"node_id"`
			Value  string `json:"value"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.Answer(ctx, params.RunID, params.NodeID, params.Value)
	case "run.cancel":
		var params struct {
			RunID  string `json:"run_id"`
			Reason string `json:"reason"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.Cancel(params.RunID, params.Reason)
	case "run.children":
		var params struct {
			RunID string `json:"run_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.Children(params.RunID)
	case "run.artifacts":
		var params struct {
			RunID, NodeID, Type string
			Recursive           bool
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.Artifacts(params.RunID, control.ArtifactQuery{NodeID: params.NodeID, Type: params.Type, Recursive: params.Recursive})
	case "run.events":
		var params struct {
			RunID         string `json:"run_id"`
			AfterRevision uint64 `json:"after_revision"`
			Limit         int    `json:"limit"`
			WaitMS        int    `json:"wait_ms"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return s.service.Events(ctx, params.RunID, params.AfterRevision, params.Limit, time.Duration(params.WaitMS)*time.Millisecond)
	default:
		return nil, fmt.Errorf("unknown daemon method %q", method)
	}
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8*1024*1024))
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	payload, respond := s.mcp.HandleJSON(r.Context(), body)
	if !respond {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	runID := strings.TrimSpace(r.URL.Query().Get("run_id"))
	if runID == "" {
		writeHTTPError(w, http.StatusBadRequest, "run_id_required", "run_id is required")
		return
	}
	after, err := strconv.ParseUint(defaultString(r.URL.Query().Get("after_revision"), "0"), 10, 64)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_revision", err.Error())
		return
	}
	limit, err := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "200"))
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeHTTPError(w, http.StatusInternalServerError, "streaming_unavailable", "HTTP streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	_ = encoder.Encode(map[string]any{"type": "subscription.started", "run_id": runID, "after_revision": after})
	flusher.Flush()
	for {
		result, eventErr := s.service.Events(r.Context(), runID, after, limit, 25*time.Second)
		if eventErr != nil {
			_ = encoder.Encode(map[string]any{"type": "subscription.error", "error": eventErr.Error()})
			flusher.Flush()
			return
		}
		for _, event := range result.Events {
			if err := encoder.Encode(event); err != nil {
				return
			}
			after = event.Revision
		}
		if len(result.Events) == 0 {
			_ = encoder.Encode(map[string]any{"type": "subscription.heartbeat", "run_id": runID, "revision": after})
		}
		flusher.Flush()
		state, stateErr := s.service.GetRun(runID)
		if stateErr == nil && terminalStatus(state.Status) {
			// The state and event journal are committed together, but the terminal
			// state can become visible after Events returned and before GetRun. Drain
			// through the terminal state's revision before closing the subscription.
			for after < state.Revision {
				drain, drainErr := s.service.Events(r.Context(), runID, after, limit, 0)
				if drainErr != nil {
					_ = encoder.Encode(map[string]any{"type": "subscription.error", "error": drainErr.Error()})
					flusher.Flush()
					return
				}
				if len(drain.Events) == 0 {
					break
				}
				for _, event := range drain.Events {
					if err := encoder.Encode(event); err != nil {
						return
					}
					after = event.Revision
				}
				flusher.Flush()
			}
			if after >= state.Revision {
				_ = encoder.Encode(map[string]any{"type": "subscription.completed", "run_id": runID, "status": state.Status, "revision": after})
				flusher.Flush()
				return
			}
		}
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
}

func terminalStatus(status string) bool {
	return status == store.RunCompleted || status == store.RunFailed || status == store.RunCancelled || status == store.RunAbandoned
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stopping": true, "pid": s.metadata.PID})
	go s.requestStop()
}

func (s *Server) monitorIdle(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.service.AdvanceDynamicPlans(context.Background()); err != nil {
				fmt.Fprintln(s.errOut, "daemon dynamic plan monitor:", err)
			}
			if expired, err := s.service.ExpireIdleExternal(context.Background(), now.UTC()); err != nil {
				fmt.Fprintln(s.errOut, "daemon idle monitor:", err)
			} else if len(expired) > 0 {
				fmt.Fprintln(s.errOut, "daemon expired idle external nodes:", strings.Join(expired, ", "))
			}
			if emitted, err := (notification.Dispatcher{Workspace: s.metadata.Workspace}).Dispatch(); err != nil {
				fmt.Fprintln(s.errOut, "daemon notifications:", err)
			} else if len(emitted) > 0 {
				fmt.Fprintln(s.errOut, "daemon notifications emitted:", len(emitted))
			}
		}
	}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeHTTPError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, rpcResponse{API: APIRevision, Error: &apiError{Code: code, Message: message}})
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".daemon-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
