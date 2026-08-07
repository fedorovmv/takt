package domainadapter

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

	"takt/internal/spec"
	sdk "takt/sdk/domainadapter"
)

type Process struct{ Spec spec.DomainAdapterSpec }

type processEnvelope struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Request    *InvokeRequest    `json:"request,omitempty"`
	Reconcile  *ReconcileRequest `json:"reconcile,omitempty"`
}

type processResponse struct {
	APIVersion  string           `json:"apiVersion"`
	Kind        string           `json:"kind"`
	Declaration *Declaration     `json:"declaration,omitempty"`
	Result      *Result          `json:"result,omitempty"`
	Reconcile   *ReconcileResult `json:"reconcile,omitempty"`
}

func (p *Process) Describe(ctx context.Context) (Declaration, error) {
	raw, err := p.call(ctx, processEnvelope{APIVersion: ProtocolV1Alpha1, Kind: "DescribeRequest"})
	if err != nil {
		return Declaration{}, err
	}
	var response processResponse
	if err := decodeStrict(raw, &response); err != nil {
		return Declaration{}, fmt.Errorf("decode domain adapter describe: %w", err)
	}
	if response.APIVersion != ProtocolV1Alpha1 || response.Kind != "DescribeResponse" || response.Declaration == nil {
		return Declaration{}, fmt.Errorf("invalid domain adapter describe response")
	}
	value := NormalizeDeclaration(*response.Declaration)
	if err := ValidateDeclaration(value); err != nil {
		return Declaration{}, err
	}
	if value.Domain != p.Spec.Domain {
		return Declaration{}, fmt.Errorf("domain adapter declared %q, configured as %q", value.Domain, p.Spec.Domain)
	}
	return value, nil
}

func (p *Process) Invoke(ctx context.Context, request InvokeRequest) (Result, error) {
	raw, err := p.call(ctx, processEnvelope{APIVersion: ProtocolV1Alpha1, Kind: "InvokeRequest", Request: &request})
	if err != nil {
		return Result{}, err
	}
	var response processResponse
	if err := decodeStrict(raw, &response); err != nil {
		return Result{}, fmt.Errorf("decode domain adapter result: %w", err)
	}
	if response.APIVersion != ProtocolV1Alpha1 || response.Kind != "InvokeResponse" || response.Result == nil {
		return Result{}, fmt.Errorf("invalid domain adapter invoke response")
	}
	if err := validateResult(*response.Result); err != nil {
		return Result{}, err
	}
	return *response.Result, nil
}

func (p *Process) Reconcile(ctx context.Context, request ReconcileRequest) (ReconcileResult, error) {
	raw, err := p.call(ctx, processEnvelope{APIVersion: ProtocolV1Alpha1, Kind: "ReconcileRequest", Reconcile: &request})
	if err != nil {
		return ReconcileResult{}, err
	}
	var response processResponse
	if err := decodeStrict(raw, &response); err != nil {
		return ReconcileResult{}, fmt.Errorf("decode domain adapter reconcile: %w", err)
	}
	if response.APIVersion != ProtocolV1Alpha1 || response.Kind != "ReconcileResponse" || response.Reconcile == nil {
		return ReconcileResult{}, fmt.Errorf("invalid domain adapter reconcile response")
	}
	if err := validateReconcile(*response.Reconcile); err != nil {
		return ReconcileResult{}, err
	}
	return *response.Reconcile, nil
}

func (p *Process) call(ctx context.Context, value processEnvelope) ([]byte, error) {
	if len(p.Spec.Argv) == 0 {
		return nil, fmt.Errorf("process domain adapter requires argv")
	}
	if p.Spec.Timeout != "" {
		timeout, err := time.ParseDuration(p.Spec.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid domain adapter timeout: %w", err)
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, p.Spec.Argv[0], p.Spec.Argv[1:]...)
	cmd.Env = os.Environ()
	for key, val := range p.Spec.Env {
		cmd.Env = append(cmd.Env, key+"="+val)
	}
	cmd.Stdin = bytes.NewReader(append(body, '\n'))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{W: &stdout, N: outputLimit(p.Spec.MaxOutputBytes)}
	cmd.Stderr = &limitedWriter{W: &stderr, N: outputLimit(p.Spec.MaxOutputBytes)}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("domain adapter process: %w", ctx.Err())
		}
		return nil, fmt.Errorf("domain adapter process: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return bytes.TrimSpace(stdout.Bytes()), nil
}

func outputLimit(value int) int64 {
	if value <= 0 {
		return 4 << 20
	}
	return int64(value)
}

type limitedWriter struct {
	W       io.Writer
	N       int64
	written int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.N > 0 && w.written+int64(len(p)) > w.N {
		return 0, fmt.Errorf("domain adapter output exceeds %d bytes", w.N)
	}
	n, err := w.W.Write(p)
	w.written += int64(n)
	return n, err
}

func decodeStrict(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateResult(value Result) error { return sdk.ValidateResult(value) }

func validateReconcile(value ReconcileResult) error { return sdk.ValidateReconcileResult(value) }
