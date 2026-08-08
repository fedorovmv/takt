package tasksource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"takt/internal/redact"
	"takt/internal/spec"
	sdk "takt/sdk/tasksource"
)

const defaultTimeout = 10 * time.Second

type Resolver struct {
	Spec spec.TaskSourceSpec
	Name string
}

func (r Resolver) Resolve(ctx context.Context, reference, workspace string) (*sdk.Task, error) {
	req := sdk.ResolveRequest{APIVersion: sdk.ProtocolV1Alpha1, Kind: "ResolveRequest", Reference: reference, Workspace: workspace}
	if err := sdk.ValidateResolveRequest(req); err != nil {
		return nil, err
	}
	if r.Spec.Transport != "process" || len(r.Spec.Argv) == 0 {
		return nil, fmt.Errorf("task source %q requires process transport and argv", r.Name)
	}
	timeout := defaultTimeout
	if r.Spec.Timeout != "" {
		d, err := time.ParseDuration(r.Spec.Timeout)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("task source %q invalid timeout", r.Name)
		}
		timeout = d
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.Spec.Argv[0], r.Spec.Argv[1:]...)
	if workspace != "" {
		cmd.Dir = workspace
	}
	cmd.Env = os.Environ()
	secrets := redact.NewFromEnvironment()
	for k, v := range r.Spec.Env {
		rv, e := secrets.Resolve(v)
		if e != nil {
			return nil, fmt.Errorf("task source %q secret %s: %w", r.Name, k, e)
		}
		cmd.Env = append(cmd.Env, k+"="+rv)
	}
	cmd.Stdin = bytes.NewReader(append(body, '\n'))
	limit := r.Spec.MaxOutputBytes
	if limit <= 0 {
		limit = 4 << 20
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{W: &stdout, N: int64(limit)}
	cmd.Stderr = &limitedWriter{W: &stderr, N: int64(limit)}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("task source %q: %w", r.Name, ctx.Err())
		}
		return nil, fmt.Errorf("task source %q: %w: %s", r.Name, err, strings.TrimSpace(stderr.String()))
	}
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(stdout.Bytes())))
	dec.DisallowUnknownFields()
	var resp sdk.ResolveResponse
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode task source %q response: %w", r.Name, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("task source %q returned multiple JSON values", r.Name)
		}
		return nil, err
	}
	if err := sdk.ValidateResolveResponse(resp); err != nil {
		return nil, fmt.Errorf("task source %q: %w", r.Name, err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("task source %q %s: %s", r.Name, resp.ErrorCode, resp.Error)
	}
	task := *resp.Task
	sdk.NormalizeTask(&task)
	task.Source.Adapter = r.Name
	if err := sdk.ValidateTask(task); err != nil {
		return nil, fmt.Errorf("task source %q normalized task: %w", r.Name, err)
	}
	return &task, nil
}

type limitedWriter struct {
	W          io.Writer
	N, written int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.N > 0 && w.written+int64(len(p)) > w.N {
		return 0, fmt.Errorf("task source output exceeds %d bytes", w.N)
	}
	n, e := w.W.Write(p)
	w.written += int64(n)
	return n, e
}
