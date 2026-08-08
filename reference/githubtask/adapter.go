package githubtask

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	sdk "takt/sdk/tasksource"
)

const DefaultGHBinary = "gh"

var repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type Adapter struct {
	GHBinary string
	Timeout  time.Duration
}
type issue struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	State     string `json:"state"`
	UpdatedAt string `json:"updatedAt"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (a Adapter) Serve(ctx context.Context, in io.Reader, out, diagnostic io.Writer) int {
	dec := json.NewDecoder(in)
	dec.DisallowUnknownFields()
	var req sdk.ResolveRequest
	if err := dec.Decode(&req); err != nil {
		fmt.Fprintf(diagnostic, "decode request: %v\n", err)
		return 2
	}
	if err := sdk.ValidateResolveRequest(req); err != nil {
		fmt.Fprintf(diagnostic, "validate request: %v\n", err)
		return 2
	}
	task, err := a.Resolve(ctx, req.Reference)
	resp := sdk.ResolveResponse{APIVersion: sdk.ProtocolV1Alpha1, Kind: "ResolveResponse"}
	if err != nil {
		resp.ErrorCode = "GITHUB_ISSUE_RESOLVE"
		resp.Error = err.Error()
	} else {
		resp.Task = task
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(resp); err != nil {
		fmt.Fprintf(diagnostic, "encode response: %v\n", err)
		return 2
	}
	if task == nil {
		return 1
	}
	return 0
}

func (a Adapter) Resolve(ctx context.Context, reference string) (*sdk.Task, error) {
	repo, num, canonical, err := parseReference(reference)
	if err != nil {
		return nil, err
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	binary := strings.TrimSpace(a.GHBinary)
	if binary == "" {
		binary = DefaultGHBinary
	}
	cmd := exec.CommandContext(ctx, binary, "issue", "view", strconv.Itoa(num), "--repo", repo, "--json", "number,title,body,url,labels,state,updatedAt")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("github issue source: %w", ctx.Err())
		}
		return nil, fmt.Errorf("github issue source gh issue view failed: %w: %s", err, trimDiagnostic(stderr.String()))
	}
	var item issue
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&item); err != nil {
		return nil, fmt.Errorf("decode github issue: %w", err)
	}
	if item.Number != num || strings.TrimSpace(item.Title) == "" {
		return nil, fmt.Errorf("github issue response does not match %s", canonical)
	}
	labels := make([]string, 0, len(item.Labels))
	for _, l := range item.Labels {
		if n := strings.TrimSpace(l.Name); n != "" {
			labels = append(labels, n)
		}
	}
	sort.Strings(labels)
	acceptance := extractAcceptance(item.Body)
	revisionInput, _ := json.Marshal(struct {
		Number                        int `json:"number"`
		Title, Body, State, UpdatedAt string
		Labels                        []string `json:"labels"`
	}{item.Number, item.Title, item.Body, item.State, item.UpdatedAt, labels})
	sum := sha256.Sum256(revisionInput)
	task := &sdk.Task{APIVersion: sdk.ProtocolV1Alpha1, Kind: "Task", ID: "github:" + canonical, Title: item.Title, Goal: item.Title, Description: item.Body, Acceptance: acceptance, Labels: labels, Source: sdk.Source{Adapter: "github", Kind: "github.issue", Reference: canonical, Revision: "sha256:" + hex.EncodeToString(sum[:]), URL: item.URL}}
	sdk.NormalizeTask(task)
	if err := sdk.ValidateTask(*task); err != nil {
		return nil, err
	}
	return task, nil
}

func parseReference(raw string) (string, int, string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", 0, "", err
		}
		if !strings.EqualFold(u.Hostname(), "github.com") {
			return "", 0, "", fmt.Errorf("GitHub issue URL host must be github.com")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 4 && parts[2] == "issues" {
			raw = parts[0] + "/" + parts[1] + "#" + parts[3]
		} else {
			return "", 0, "", fmt.Errorf("unsupported GitHub issue URL")
		}
	}
	repo, nText, ok := strings.Cut(raw, "#")
	if !ok || !repoRE.MatchString(repo) {
		return "", 0, "", fmt.Errorf("GitHub issue reference must be owner/repo#number or issue URL")
	}
	n, err := strconv.Atoi(nText)
	if err != nil || n <= 0 {
		return "", 0, "", fmt.Errorf("GitHub issue number must be positive")
	}
	canonical := repo + "#" + strconv.Itoa(n)
	return repo, n, canonical, nil
}
func extractAcceptance(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "- [ ] ") || strings.HasPrefix(lower, "- [x] ") {
			if len(line) > 6 {
				out = append(out, strings.TrimSpace(line[6:]))
			}
		}
	}
	return out
}
func trimDiagnostic(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2048 {
		s = s[:2048] + "..."
	}
	return s
}
