// Package boardmcp implements the command center's narrow MCP server: three
// tools (board_status, board_messages, board_broadcast) that are thin
// wrappers around panemux's own authenticated REST API. See
// docs/agent-board.md's Command center section — this is deliberately the
// only code that talks to that REST API on the command center subprocess's
// behalf, so the LLM itself never composes an HTTP call or a shell
// invocation.
package boardmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BoardAPIClient is what the MCP server's tool handlers call. Returning raw
// JSON response bodies (rather than decoding into typed DTOs) keeps this
// package decoupled from internal/api's response shapes — the LLM sees
// exactly what a human dashboard request would see.
type BoardAPIClient interface {
	Status(ctx context.Context) (json.RawMessage, error)
	Messages(ctx context.Context, since int64) (json.RawMessage, error)
	Broadcast(ctx context.Context, to []string, body string) (json.RawMessage, error)
}

const httpClientTimeout = 30 * time.Second

// HTTPBoardAPIClient calls panemux's own /api/board/* REST endpoints over
// loopback with a bearer token, the same endpoints the browser dashboard
// uses.
type HTTPBoardAPIClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

// NewHTTPBoardAPIClient constructs a client against baseURL (e.g.
// "http://127.0.0.1:8080") using token as the bearer credential.
func NewHTTPBoardAPIClient(baseURL, token string) *HTTPBoardAPIClient {
	return &HTTPBoardAPIClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: httpClientTimeout},
	}
}

// Status calls GET /api/board/status.
func (c *HTTPBoardAPIClient) Status(ctx context.Context) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, "/api/board/status", nil)
}

// Messages calls GET /api/board/messages, including ?since=<since> only
// when since is non-zero (0 is the REST API's own "from the start" default).
func (c *HTTPBoardAPIClient) Messages(ctx context.Context, since int64) (json.RawMessage, error) {
	path := "/api/board/messages"
	if since != 0 {
		path += "?since=" + strconv.FormatInt(since, 10)
	}
	return c.do(ctx, http.MethodGet, path, nil)
}

// Broadcast calls POST /api/board/broadcast with {"to": to, "body": body}.
func (c *HTTPBoardAPIClient) Broadcast(ctx context.Context, to []string, body string) (json.RawMessage, error) {
	reqBody, err := json.Marshal(map[string]any{"to": to, "body": body})
	if err != nil {
		return nil, fmt.Errorf("encoding broadcast request: %w", err)
	}
	return c.do(ctx, http.MethodPost, "/api/board/broadcast", bytes.NewReader(reqBody))
}

func (c *HTTPBoardAPIClient) do(ctx context.Context, method, path string, body io.Reader) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling panemux board api: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading panemux board api response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("panemux board api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return json.RawMessage(data), nil
}
