package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"takt/internal/store"
)

type Client struct {
	paths Paths
	http  *http.Client
}

func NewClient(workspace, socketOverride string) (*Client, error) {
	paths, err := ResolvePaths(workspace, socketOverride)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", paths.Socket)
	}}
	return &Client{paths: paths, http: &http.Client{Transport: transport}}, nil
}

func (c *Client) Paths() Paths { return c.paths }

func (c *Client) Health(ctx context.Context) (*Metadata, error) {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://takt/health", nil)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon health returned %s", response.Status)
	}
	var metadata Metadata
	if err := json.NewDecoder(response.Body).Decode(&metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	raw, err := json.Marshal(rpcRequest{Method: method, Params: mustRaw(params)})
	if err != nil {
		return err
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://takt/rpc", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var envelope struct {
		API    string          `json:"api"`
		Result json.RawMessage `json:"result"`
		Error  *apiError       `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return err
	}
	if envelope.Error != nil {
		return errors.New(envelope.Error.Message)
	}
	if result != nil && len(envelope.Result) > 0 {
		return json.Unmarshal(envelope.Result, result)
	}
	return nil
}

func mustRaw(value any) json.RawMessage {
	if value == nil {
		return json.RawMessage(`{}`)
	}
	raw, _ := json.Marshal(value)
	return raw
}

func (c *Client) MCP(ctx context.Context, raw []byte) ([]byte, bool, error) {
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://takt/mcp", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil, false, nil
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, false, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("daemon MCP returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return body, true, nil
}

func (c *Client) Shutdown(ctx context.Context) error {
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://takt/shutdown", nil)
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon shutdown returned %s", response.Status)
	}
	return nil
}

func (c *Client) Subscribe(ctx context.Context, runID string, after uint64, limit int, consume func(store.Event) error) error {
	query := url.Values{}
	query.Set("run_id", runID)
	query.Set("after_revision", fmt.Sprint(after))
	query.Set("limit", fmt.Sprint(limit))
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://takt/events?"+query.Encode(), nil)
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("event subscription returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var marker struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(line, &marker)
		if strings.HasPrefix(marker.Type, "subscription.") {
			continue
		}
		var event store.Event
		if err := json.Unmarshal(line, &event); err != nil {
			return err
		}
		if err := consume(event); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func WaitForHealth(ctx context.Context, client *Client, interval time.Duration) (*Metadata, error) {
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var last error
	for {
		metadata, err := client.Health(ctx)
		if err == nil {
			return metadata, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("daemon did not become healthy: %w", last)
		case <-ticker.C:
		}
	}
}
