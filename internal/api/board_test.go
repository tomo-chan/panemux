package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/session"
)

type broadcastFunc func(ctx context.Context, to []string, body string) ([]string, error)

func setupBoardRouter(cache *board.BoardCache, broadcastFn broadcastFunc) *Handler {
	h := NewHandler(defaultTestConfig(), session.NewManager(), cache, nil)
	if broadcastFn != nil {
		h.boardBroadcastFn = broadcastFn
	}
	return h
}

// loopbackSessionTokenRequest builds a GET /api/session-token request that
// looks like it came from the local machine over a loopback interface with
// no DNS-rebinding trickery: both the TCP peer (RemoteAddr) and the Host
// header claim a loopback authority. GetBoardSessionToken requires both —
// see its own doc comment for why RemoteAddr alone can't distinguish a
// legitimate local dashboard from a DNS-rebound attacker page that also
// genuinely connects to 127.0.0.1.
func loopbackSessionTokenRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/session-token", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "127.0.0.1:8080"
	return req
}

func TestGetBoardSessionToken_ReturnsConfiguredTokenAndCommandCenterState(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Server.AuthToken = "sekret"
	h := NewHandler(cfg, session.NewManager(), board.NewBoardCache(), nil)
	h.SetCommandCenterAvailable(true)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := loopbackSessionTokenRequest()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardSessionTokenResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "sekret", resp.Token)
	assert.True(t, resp.CommandCenterEnabled)
}

func TestGetBoardSessionToken_CommandCenterUnavailable_ReportsFalse(t *testing.T) {
	// cfg.CommandCenter.Enabled alone must not drive this response field:
	// setup can fail after the config check (e.g. no auth token, or a path
	// resolution error in setupCommandCenter), leaving /ws/board-command
	// unregistered even though the operator's config says "enabled". The
	// frontend must be told the route doesn't actually exist, not shown a
	// working-looking palette that 404s on every request.
	cfg := defaultTestConfig()
	cfg.Server.AuthToken = "sekret"
	cfg.CommandCenter.Enabled = true // config says enabled...
	h := NewHandler(cfg, session.NewManager(), board.NewBoardCache(), nil)
	// ...but SetCommandCenterAvailable is never called, matching a Runner
	// that setupCommandCenter decided not to build.
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := loopbackSessionTokenRequest()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardSessionTokenResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.False(t, resp.CommandCenterEnabled)
}

func boolPtr(b bool) *bool { return &b }

// TestGetBoardSessionToken_AgentBoardEnabled covers agentBoardEnabledAnyPane
// (internal/api/board.go): the response field must be true only when at
// least one pane has agent_board.enabled: true, independent of
// CommandCenterEnabled — see agentBoardEnabledAnyPane's own doc comment.
func TestGetBoardSessionToken_AgentBoardEnabled(t *testing.T) {
	tests := []struct {
		mutateLayout func(cfg *config.Config)
		name         string
		want         bool
	}{
		{
			name:         "one pane enabled reports true",
			mutateLayout: func(cfg *config.Config) { cfg.Layout.Children[0].Pane.AgentBoard.Enabled = boolPtr(true) },
			want:         true,
		},
		{
			name:         "pane explicitly disabled reports false",
			mutateLayout: func(cfg *config.Config) { cfg.Layout.Children[0].Pane.AgentBoard.Enabled = boolPtr(false) },
			want:         false,
		},
		{
			name:         "no panes reports false",
			mutateLayout: func(cfg *config.Config) { cfg.Layout.Children = nil },
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultTestConfig()
			cfg.Server.AuthToken = "sekret"
			tt.mutateLayout(cfg)
			h := NewHandler(cfg, session.NewManager(), board.NewBoardCache(), nil)
			r := setupRouterWithHandler(h)

			rec := httptest.NewRecorder()
			req := loopbackSessionTokenRequest()
			r.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			var resp boardSessionTokenResponse
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
			assert.Equal(t, tt.want, resp.AgentBoardEnabled)
		})
	}
}

func TestGetBoardSessionToken_NonLoopbackRemoteAddr_Forbidden(t *testing.T) {
	// A real remote client — even one that sends a Host header matching the
	// server's own configured non-loopback address — must never receive the
	// token: the token's entire purpose is to gate exactly this kind of
	// network-reachable access (see internal/config/validate.go's
	// non-loopback-requires-token rule), so handing it out to any TCP peer
	// that isn't the local machine defeats it regardless of what Host claims.
	cfg := defaultTestConfig()
	cfg.Server.AuthToken = "sekret"
	h := NewHandler(cfg, session.NewManager(), board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/session-token", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Host = "127.0.0.1:8080" // even a "trusted-looking" Host must not help
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestGetBoardSessionToken_DNSRebindingHost_Forbidden(t *testing.T) {
	// DNS rebinding: attacker.example resolves to 127.0.0.1, so the TCP
	// connection genuinely is loopback (RemoteAddr can't tell this apart
	// from a legitimate local dashboard), but the browser still sends the
	// original navigation Host header, which this must reject.
	cfg := defaultTestConfig()
	cfg.Server.AuthToken = "sekret"
	h := NewHandler(cfg, session.NewManager(), board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/session-token", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "attacker.example:8080"
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestGetBoardSessionToken_LocalhostHostname_Allowed(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Server.AuthToken = "sekret"
	h := NewHandler(cfg, session.NewManager(), board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/session-token", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "localhost:5173" // Vite dev server port, matching corsMiddleware's own allowance
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGetBoardSessionToken_WildcardBindHost_Allowed(t *testing.T) {
	// A wildcard-bound server (server.host: "0.0.0.0") reached via `--open`
	// launches the browser at exactly http://0.0.0.0:<port>/, so the
	// dashboard's own same-origin request carries a literal "0.0.0.0" Host
	// header — a legitimate case, not an attacker-supplied one (an attacker
	// would need a victim to specifically navigate to a URL naming
	// "0.0.0.0", which DNS rebinding does not produce).
	cfg := defaultTestConfig()
	cfg.Server.AuthToken = "sekret"
	h := NewHandler(cfg, session.NewManager(), board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/session-token", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "0.0.0.0:8080"
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGetBoardSessionToken_XForwardedForHeader_Forbidden(t *testing.T) {
	// A same-host reverse proxy (the exact mitigation docs/security.md
	// itself recommends for exposing panemux beyond loopback) makes every
	// proxied request arrive with a loopback RemoteAddr and, under common
	// proxy defaults (nginx's $proxy_host, Apache's ProxyPreserveHost Off),
	// a loopback-looking Host too — defeating both checks above for a
	// genuinely remote client. A forwarding header is the one signal a
	// direct local browser request never carries, so its mere presence is
	// treated as proof this request was proxied and must be rejected.
	cfg := defaultTestConfig()
	cfg.Server.AuthToken = "sekret"
	h := NewHandler(cfg, session.NewManager(), board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := loopbackSessionTokenRequest()
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestGetBoardSessionToken_XRealIPHeader_Forbidden(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Server.AuthToken = "sekret"
	h := NewHandler(cfg, session.NewManager(), board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := loopbackSessionTokenRequest()
	req.Header.Set("X-Real-IP", "203.0.113.7")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestGetBoardSessionToken_ForwardedHeader_Forbidden(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Server.AuthToken = "sekret"
	h := NewHandler(cfg, session.NewManager(), board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := loopbackSessionTokenRequest()
	req.Header.Set("Forwarded", "for=203.0.113.7")
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestGetBoardStatus_EmptyCache_ReturnsEmptyObject(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardStatusResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Empty(t, resp.Statuses)
}

func TestGetBoardStatus_PopulatedCache_ReturnsEntries(t *testing.T) {
	cache := board.NewBoardCache()
	cache.RecordStatus("pane-a", board.Status{State: "working", Branch: "feature/x"})
	h := setupBoardRouter(cache, nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardStatusResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Contains(t, resp.Statuses, "pane-a")
	assert.Equal(t, "working", resp.Statuses["pane-a"].State)
	assert.Equal(t, "feature/x", resp.Statuses["pane-a"].Branch)
}

func TestGetBoardMessages_EmptyCache_ReturnsEmptyArray(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/messages", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardMessagesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotNil(t, resp.Messages)
	assert.Empty(t, resp.Messages)
}

func TestGetBoardMessages_MissingSince_DefaultsToZero(t *testing.T) {
	cache := board.NewBoardCache()
	cache.AppendMessage(board.Row{From: "a", To: "b", Body: "hi", At: time.Now()})
	h := setupBoardRouter(cache, nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/messages", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardMessagesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Messages, 1)
	assert.Equal(t, int64(1), resp.Messages[0].Seq)
	assert.Equal(t, "hi", resp.Messages[0].Body)
}

func TestGetBoardMessages_NonNumericSince_400(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/messages?since=not-a-number", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetBoardMessages_SinceExcludesEverything_EmptyResult(t *testing.T) {
	cache := board.NewBoardCache()
	cache.AppendMessage(board.Row{Body: "one"})
	h := setupBoardRouter(cache, nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/messages?since=1", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardMessagesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Empty(t, resp.Messages)
}

func postBoardBroadcast(t *testing.T, r http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	switch v := body.(type) {
	case string:
		reader = bytes.NewReader([]byte(v))
	default:
		data, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(data)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/board/broadcast", reader)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

func TestPostBoardBroadcast_MissingTo_422(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), func(context.Context, []string, string) ([]string, error) {
		t.Fatal("broadcast fn must not be called for a validation failure")
		return nil, nil
	})
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, boardBroadcastRequest{Body: "hello"})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostBoardBroadcast_EmptyBody_422(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), func(context.Context, []string, string) ([]string, error) {
		t.Fatal("broadcast fn must not be called for a validation failure")
		return nil, nil
	})
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, boardBroadcastRequest{To: []string{"pane-a"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostBoardBroadcast_MalformedJSON_400(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, "not json")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPostBoardBroadcast_UnknownPaneError_422(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), func(context.Context, []string, string) ([]string, error) {
		return nil, &board.UnknownPaneError{IDs: []string{"no-such-pane"}}
	})
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, boardBroadcastRequest{To: []string{"no-such-pane"}, Body: "hello"})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "no-such-pane")
}

func TestPostBoardBroadcast_GenericError_502(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), func(context.Context, []string, string) ([]string, error) {
		return nil, errors.New("ssh: connection lost")
	})
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, boardBroadcastRequest{To: []string{"pane-a"}, Body: "hello"})
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	var resp boardBroadcastErrorResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp.Error, "ssh: connection lost")
	assert.Empty(t, resp.Delivered)
}

func TestPostBoardBroadcast_PartialFailure_502ReportsDelivered(t *testing.T) {
	// Relay.Broadcast is fail-fast, not all-or-nothing, on a Send failure: it
	// stops at the first error but reports every pane ID it successfully
	// delivered to before that point. The handler must forward that partial
	// list rather than discarding it, so a caller can tell which panes
	// already got the message instead of risking a double-send on retry.
	h := setupBoardRouter(board.NewBoardCache(), func(_ context.Context, to []string, body string) ([]string, error) {
		return []string{"pane-a"}, errors.New("agent board: broadcast to \"pane-b\" failed: ssh timeout")
	})
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, boardBroadcastRequest{To: []string{"pane-a", "pane-b"}, Body: "hello"})
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	var resp boardBroadcastErrorResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, []string{"pane-a"}, resp.Delivered)
	assert.Contains(t, resp.Error, "pane-b")
}

func TestPostBoardBroadcast_Success_200WithDelivered(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), func(_ context.Context, to []string, body string) ([]string, error) {
		assert.Equal(t, []string{"pane-a", "pane-b"}, to)
		assert.Equal(t, "hello", body)
		return to, nil
	})
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, boardBroadcastRequest{To: []string{"pane-a", "pane-b"}, Body: "hello"})
	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardBroadcastResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, []string{"pane-a", "pane-b"}, resp.Delivered)
}
