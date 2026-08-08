package githubscm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	sdk "takt/sdk/domainadapter"
)

const DefaultGHBinary = "gh"

type Adapter struct{ GHBinary string }

type envelope struct {
	APIVersion string                `json:"apiVersion"`
	Kind       string                `json:"kind"`
	Request    *sdk.InvokeRequest    `json:"request,omitempty"`
	Reconcile  *sdk.ReconcileRequest `json:"reconcile,omitempty"`
}
type response struct {
	APIVersion  string               `json:"apiVersion"`
	Kind        string               `json:"kind"`
	Declaration *sdk.Declaration     `json:"declaration,omitempty"`
	Result      *sdk.Result          `json:"result,omitempty"`
	Reconcile   *sdk.ReconcileResult `json:"reconcile,omitempty"`
}

type changeInput struct {
	Repository          string `json:"repository,omitempty"`
	RepositoryWorkspace string `json:"repository_workspace,omitempty"`
	Number              int    `json:"number,omitempty"`
	Ref                 string `json:"ref,omitempty"`
	Title               string `json:"title,omitempty"`
	Body                string `json:"body,omitempty"`
	Head                string `json:"head,omitempty"`
	Base                string `json:"base,omitempty"`
	BaseCommit          string `json:"base_commit,omitempty"`
	Draft               bool   `json:"draft,omitempty"`
	Review              string `json:"review,omitempty"`
}

func (a Adapter) Serve(ctx context.Context, in io.Reader, out, diagnostic io.Writer) int {
	dec := json.NewDecoder(in)
	dec.DisallowUnknownFields()
	var env envelope
	if err := dec.Decode(&env); err != nil {
		fmt.Fprintf(diagnostic, "decode request: %v\n", err)
		return 2
	}
	if env.APIVersion != sdk.ProtocolV1Alpha1 {
		fmt.Fprintf(diagnostic, "unsupported apiVersion %q\n", env.APIVersion)
		return 2
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	switch env.Kind {
	case "DescribeRequest":
		declaration := sdk.NormalizeDeclaration(sdk.Declaration{Domain: sdk.DomainSCM, Capabilities: sdk.CoreOperations(sdk.DomainSCM), Reconcile: []string{sdk.SCMChangeCreate, sdk.SCMChangeComment, sdk.SCMChangeReview}})
		if err := sdk.ValidateDeclaration(declaration); err != nil {
			fmt.Fprintln(diagnostic, err)
			return 2
		}
		_ = enc.Encode(response{APIVersion: sdk.ProtocolV1Alpha1, Kind: "DescribeResponse", Declaration: &declaration})
		return 0
	case "InvokeRequest":
		if env.Request == nil {
			return protocolFailure(enc, diagnostic, "InvokeRequest requires request")
		}
		result := a.invoke(ctx, *env.Request)
		if err := sdk.ValidateResult(result); err != nil {
			return protocolFailure(enc, diagnostic, err.Error())
		}
		_ = enc.Encode(response{APIVersion: sdk.ProtocolV1Alpha1, Kind: "InvokeResponse", Result: &result})
		return 0
	case "ReconcileRequest":
		if env.Reconcile == nil {
			return protocolFailure(enc, diagnostic, "ReconcileRequest requires reconcile")
		}
		result := a.reconcile(ctx, *env.Reconcile)
		if err := sdk.ValidateReconcileResult(result); err != nil {
			return protocolFailure(enc, diagnostic, err.Error())
		}
		_ = enc.Encode(response{APIVersion: sdk.ProtocolV1Alpha1, Kind: "ReconcileResponse", Reconcile: &result})
		return 0
	default:
		return protocolFailure(enc, diagnostic, "unsupported request kind "+env.Kind)
	}
}

func decodeChangeInput(raw json.RawMessage, out *changeInput) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
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

func protocolFailure(enc *json.Encoder, diagnostic io.Writer, message string) int {
	fmt.Fprintln(diagnostic, message)
	return 2
}

func (a Adapter) invoke(ctx context.Context, req sdk.InvokeRequest) sdk.Result {
	if err := sdk.ValidateInvokeRequest(req); err != nil {
		return failed("INVALID_REQUEST", err.Error())
	}
	if req.Domain != sdk.DomainSCM {
		return failed("DOMAIN_MISMATCH", "GitHub adapter supports scm only")
	}
	if err := sdk.ValidateOperation(req.Operation); err != nil {
		return failed("INVALID_OPERATION", err.Error())
	}
	var input changeInput
	if err := decodeChangeInput(req.Input, &input); err != nil {
		return failed("INVALID_INPUT", err.Error())
	}
	repo, dir, err := resolveRepository(ctx, req.Workspace, input)
	if err != nil {
		return failed("REPOSITORY_RESOLUTION", err.Error())
	}
	marker := idempotencyMarker(req.IdempotencyKey)
	switch req.Operation {
	case sdk.SCMRepositoryGet:
		raw, err := a.gh(ctx, dir, repo, 0, "api", "repos/{owner}/{repo}")
		if err != nil {
			return failed("GH_ERROR", err.Error())
		}
		return completed(raw, "")
	case sdk.SCMChangeGet:
		id, err := changeIdentifier(input)
		if err != nil {
			return failed("INVALID_INPUT", err.Error())
		}
		raw, err := a.gh(ctx, dir, repo, 0, "pr", "view", id, "--json", prFields())
		if err != nil {
			return failed("GH_ERROR", err.Error())
		}
		return completed(raw, receiptFromPR(raw))
	case sdk.SCMChangeCreate:
		if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Head) == "" {
			return failed("INVALID_INPUT", "change.create requires title and head")
		}
		body := withMarker(input.Body, marker)
		args := []string{"pr", "create", "--title", input.Title, "--body", body, "--head", input.Head}
		if input.Base != "" {
			args = append(args, "--base", input.Base)
		}
		if input.Draft {
			args = append(args, "--draft")
		}
		raw, err := a.gh(ctx, dir, repo, 0, args...)
		if err != nil {
			return uncertain(req, err)
		}
		prURL := strings.TrimSpace(string(raw))
		view, viewErr := a.gh(ctx, dir, repo, 0, "pr", "view", prURL, "--json", prFields())
		if viewErr != nil {
			return sdk.Result{Status: "unknown", Output: json.RawMessage(`null`), Receipt: prURL, ErrorCode: "GH_VERIFY_ERROR", Error: viewErr.Error()}
		}
		return completed(view, prURL)
	case sdk.SCMChangeComment:
		id, err := changeIdentifier(input)
		if err != nil {
			return failed("INVALID_INPUT", err.Error())
		}
		if input.Body == "" {
			return failed("INVALID_INPUT", "change.comment requires body")
		}
		endpoint := "repos/{owner}/{repo}/issues/" + id + "/comments"
		raw, err := a.gh(ctx, dir, repo, 0, "api", endpoint, "-f", "body="+withMarker(input.Body, marker))
		if err != nil {
			return uncertain(req, err)
		}
		return completed(raw, receiptFromObject("comment", raw))
	case sdk.SCMChangeReview:
		id, err := changeIdentifier(input)
		if err != nil {
			return failed("INVALID_INPUT", err.Error())
		}
		eventName, err := reviewEvent(input.Review)
		if err != nil {
			return failed("INVALID_INPUT", err.Error())
		}
		endpoint := "repos/{owner}/{repo}/pulls/" + id + "/reviews"
		args := []string{"api", endpoint, "-f", "event=" + eventName}
		if input.Body != "" || marker != "" {
			args = append(args, "-f", "body="+withMarker(input.Body, marker))
		}
		raw, err := a.gh(ctx, dir, repo, 0, args...)
		if err != nil {
			return uncertain(req, err)
		}
		return completed(raw, receiptFromObject("review", raw))
	case sdk.SCMChecksGet:
		id, err := changeIdentifier(input)
		if err != nil {
			return failed("INVALID_INPUT", err.Error())
		}
		raw, err := a.gh(ctx, dir, repo, 8, "pr", "checks", id, "--json", "bucket,completedAt,description,event,link,name,startedAt,state,workflow")
		if err != nil {
			return failed("GH_ERROR", err.Error())
		}
		return completed(raw, "")
	default:
		return failed("UNSUPPORTED_OPERATION", "unsupported SCM operation "+req.Operation)
	}
}

func (a Adapter) reconcile(ctx context.Context, req sdk.ReconcileRequest) sdk.ReconcileResult {
	if err := sdk.ValidateReconcileRequest(req); err != nil {
		return sdk.ReconcileResult{Outcome: "unknown", ErrorCode: "INVALID_REQUEST", Error: err.Error()}
	}
	var input changeInput
	if err := decodeChangeInput(req.Input, &input); err != nil {
		return sdk.ReconcileResult{Outcome: "unknown", ErrorCode: "INVALID_INPUT", Error: err.Error()}
	}
	repo, dir, err := resolveRepository(ctx, req.Workspace, input)
	if err != nil {
		return sdk.ReconcileResult{Outcome: "unknown", ErrorCode: "REPOSITORY_RESOLUTION", Error: err.Error()}
	}
	marker := idempotencyMarker(req.IdempotencyKey)
	switch req.Operation {
	case sdk.SCMChangeCreate:
		if req.Receipt != "" {
			if raw, e := a.gh(ctx, dir, repo, 0, "pr", "view", req.Receipt, "--json", prFields()); e == nil {
				return sdk.ReconcileResult{Outcome: "applied", Output: raw, Receipt: req.Receipt}
			}
		}
		if input.Head == "" || marker == "" {
			return sdk.ReconcileResult{Outcome: "unknown", Error: "change.create reconciliation needs head and idempotency key"}
		}
		args := []string{"pr", "list", "--state", "all", "--head", input.Head, "--limit", "100", "--json", prFields()}
		if input.Base != "" {
			args = append(args, "--base", input.Base)
		}
		raw, e := a.gh(ctx, dir, repo, 0, args...)
		if e != nil {
			return sdk.ReconcileResult{Outcome: "unknown", ErrorCode: "GH_ERROR", Error: e.Error()}
		}
		if item := findMarker(raw, marker); len(item) > 0 {
			return sdk.ReconcileResult{Outcome: "applied", Output: item, Receipt: receiptFromPR(item)}
		}
		return sdk.ReconcileResult{Outcome: "not_applied"}
	case sdk.SCMChangeComment:
		id, e := changeIdentifier(input)
		if e != nil || marker == "" {
			return sdk.ReconcileResult{Outcome: "unknown", Error: "change.comment reconciliation needs change id and idempotency key"}
		}
		raw, e := a.gh(ctx, dir, repo, 0, "api", "repos/{owner}/{repo}/issues/"+id+"/comments", "--paginate")
		if e != nil {
			return sdk.ReconcileResult{Outcome: "unknown", ErrorCode: "GH_ERROR", Error: e.Error()}
		}
		if item := findMarker(raw, marker); len(item) > 0 {
			return sdk.ReconcileResult{Outcome: "applied", Output: item, Receipt: receiptFromObject("comment", item)}
		}
		return sdk.ReconcileResult{Outcome: "not_applied"}
	case sdk.SCMChangeReview:
		id, e := changeIdentifier(input)
		if e != nil || marker == "" {
			return sdk.ReconcileResult{Outcome: "unknown", Error: "change.review reconciliation needs change id and idempotency key"}
		}
		raw, e := a.gh(ctx, dir, repo, 0, "api", "repos/{owner}/{repo}/pulls/"+id+"/reviews", "--paginate")
		if e != nil {
			return sdk.ReconcileResult{Outcome: "unknown", ErrorCode: "GH_ERROR", Error: e.Error()}
		}
		if item := findMarker(raw, marker); len(item) > 0 {
			return sdk.ReconcileResult{Outcome: "applied", Output: item, Receipt: receiptFromObject("review", item)}
		}
		return sdk.ReconcileResult{Outcome: "not_applied"}
	default:
		return sdk.ReconcileResult{Outcome: "unknown", ErrorCode: "RECONCILE_UNSUPPORTED", Error: "reconcile unsupported for " + req.Operation}
	}
}

func (a Adapter) gh(ctx context.Context, dir, repo string, allowedNonZero int, args ...string) (json.RawMessage, error) {
	binary := strings.TrimSpace(a.GHBinary)
	if binary == "" {
		binary = strings.TrimSpace(os.Getenv("TAKT_GITHUB_GH_BINARY"))
	}
	if binary == "" {
		binary = DefaultGHBinary
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append([]string{}, os.Environ()...)
	if repo != "" {
		cmd.Env = append(cmd.Env, "GH_REPO="+repo)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != allowedNonZero {
			return nil, fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		}
	}
	raw := bytes.TrimSpace(stdout.Bytes())
	if len(raw) == 0 {
		return json.RawMessage(`null`), nil
	}
	if json.Valid(raw) {
		return append(json.RawMessage(nil), raw...), nil
	}
	encoded, _ := json.Marshal(string(raw))
	return encoded, nil
}

func resolveRepository(ctx context.Context, workspace string, input changeInput) (string, string, error) {
	candidates := []string{input.RepositoryWorkspace}
	// Prefer an existing local path over slug interpretation. Relative repository
	// IDs such as services/api are common in multi-repo workspaces and are
	// otherwise indistinguishable lexically from owner/repo.
	if input.Repository != "" {
		if filepath.IsAbs(input.Repository) {
			candidates = append(candidates, input.Repository)
		} else if workspace != "" {
			candidates = append(candidates, filepath.Join(workspace, input.Repository))
		}
	}
	if workspace != "" {
		candidates = append(candidates, workspace)
	}
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			if repo, err := repoFromGit(ctx, dir); err == nil {
				return repo, dir, nil
			}
		}
	}
	if looksLikeRepoSlug(input.Repository) {
		return strings.TrimSpace(input.Repository), workspace, nil
	}
	if repo := strings.TrimSpace(os.Getenv("GH_REPO")); repo != "" {
		return repo, workspace, nil
	}
	return "", "", fmt.Errorf("cannot resolve GitHub repository from workspace/input; set repository, repository_workspace, or GH_REPO")
}
func repoFromGit(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin")
	raw, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return parseRemote(strings.TrimSpace(string(raw)))
}
func parseRemote(raw string) (string, error) {
	raw = strings.TrimSuffix(raw, ".git")
	if strings.HasPrefix(raw, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(raw, "git@"), ":", 2)
		if len(parts) == 2 {
			return hostRepo(parts[0], parts[1]), nil
		}
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return hostRepo(u.Host, strings.TrimPrefix(u.Path, "/")), nil
	}
	return "", fmt.Errorf("unsupported git remote %q", raw)
}
func hostRepo(host, path string) string {
	path = strings.Trim(path, "/")
	if host == "github.com" {
		return path
	}
	return host + "/" + path
}
func looksLikeRepoSlug(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, ".") || strings.Contains(v, "\\") {
		return false
	}
	parts := strings.Split(v, "/")
	return len(parts) == 2 || len(parts) == 3
}
func changeIdentifier(i changeInput) (string, error) {
	if i.Number > 0 {
		return strconv.Itoa(i.Number), nil
	}
	for _, v := range []string{i.Ref, i.Head} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v), nil
		}
	}
	return "", fmt.Errorf("change operation requires number, ref, or head")
}
func reviewEvent(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "comment":
		return "COMMENT", nil
	case "approve", "approved":
		return "APPROVE", nil
	case "request_changes", "request-changes", "changes":
		return "REQUEST_CHANGES", nil
	default:
		return "", fmt.Errorf("review must be comment, approve, or request_changes")
	}
}
func idempotencyMarker(key string) string {
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return "<!-- takt-idempotency:" + hex.EncodeToString(sum[:]) + " -->"
}
func withMarker(body, marker string) string {
	body = strings.TrimSpace(body)
	if marker == "" {
		return body
	}
	if strings.Contains(body, marker) {
		return body
	}
	if body == "" {
		return marker
	}
	return body + "\n\n" + marker
}
func prFields() string {
	return "number,title,body,state,url,headRefName,baseRefName,isDraft,mergeable,reviewDecision"
}
func completed(raw json.RawMessage, receipt string) sdk.Result {
	return sdk.Result{Status: "completed", Output: raw, Receipt: receipt}
}
func failed(code, message string) sdk.Result {
	return sdk.Result{Status: "failed", ErrorCode: code, Error: message}
}
func uncertain(req sdk.InvokeRequest, err error) sdk.Result {
	if req.SideEffectMode == "reconcile" {
		return sdk.Result{Status: "unknown", ErrorCode: "GH_UNKNOWN", Error: err.Error()}
	}
	return failed("GH_ERROR", err.Error())
}
func receiptFromPR(raw json.RawMessage) string {
	var v struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.URL
}
func receiptFromObject(kind string, raw json.RawMessage) string {
	var v struct {
		ID  any    `json:"id"`
		URL string `json:"html_url"`
	}
	_ = json.Unmarshal(raw, &v)
	if v.URL != "" {
		return v.URL
	}
	if v.ID != nil {
		return "github:" + kind + ":" + fmt.Sprint(v.ID)
	}
	return ""
}
func findMarker(raw json.RawMessage, marker string) json.RawMessage {
	var values []map[string]any
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	for _, v := range values {
		body, _ := v["body"].(string)
		if strings.Contains(body, marker) {
			b, _ := json.Marshal(v)
			return b
		}
	}
	return nil
}
