package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"panemux/internal/board"
)

// boardBroadcaster is the subset of *board.Relay PostBoardBroadcast depends
// on, declared locally so tests can substitute a fake without pulling in a
// real Relay/AgmsgClient.
type boardBroadcaster interface {
	Broadcast(ctx context.Context, team string, to []string, body string) []board.BroadcastResult
}

// EnableBoard wires the Agent Board REST endpoints (GET /api/board/status,
// GET /api/board/messages, POST /api/board/broadcast) to a live cache and
// broadcaster. Until called, those endpoints respond 503 rather than
// operating on a nil cache — Agent Board is additive, never load-bearing
// for the rest of the API (see docs/agent-board.md's Design principles).
func (h *Handler) EnableBoard(cache *board.BoardCache, broadcaster boardBroadcaster, defaultTeam string) {
	h.boardCache = cache
	h.boardBroadcaster = broadcaster
	h.boardDefaultTeam = defaultTeam
}

// BoardEnabled reports whether EnableBoard has been called.
func (h *Handler) BoardEnabled() bool {
	return h.boardCache != nil
}

// BoardSessionRestartHookConfigured reports whether
// SetBoardSessionRestartHook has been called. Exported for callers (server
// startup wiring, tests) that need to confirm the hook was actually wired
// without reaching into Handler's internals — mirrors BoardEnabled's
// existing pattern.
func (h *Handler) BoardSessionRestartHookConfigured() bool {
	return h.boardSessionRestartHook != nil
}

// RequireBoardAuth gates a board request behind server.auth_token. An empty
// configured token means no token is required, matching the rest of this
// repo's pre-board, unauthenticated-by-default local API; a non-empty token
// requires an exact `Authorization: Bearer <token>` match. See
// docs/agent-board.md's API additions ("ALL of the following require the
// global bearer-token auth") and docs/security.md's Security model. Reports
// false (having already written the response) when the request must be
// rejected.
func (h *Handler) RequireBoardAuth(w http.ResponseWriter, r *http.Request) bool {
	token := h.cfg.Server.AuthToken
	if token == "" {
		return true
	}
	got := bearerToken(r)
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// BoardAuthMiddleware wraps every /api/board/* route with RequireBoardAuth.
// Exported so internal/server can attach it only to the board route group.
func BoardAuthMiddleware(h *Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !h.RequireBoardAuth(w, r) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	hv := r.Header.Get("Authorization")
	if !strings.HasPrefix(hv, prefix) {
		return ""
	}
	return strings.TrimPrefix(hv, prefix)
}

type boardStatusEntry struct {
	UpdatedAt time.Time `json:"updated_at"`
	State     string    `json:"state,omitempty"`
	CWD       string    `json:"cwd,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	Repo      string    `json:"repo,omitempty"`
	PRURL     string    `json:"pr_url,omitempty"`
	LastTool  string    `json:"last_tool,omitempty"`
	Summary   string    `json:"summary,omitempty"`
}

type boardStatusResponse struct {
	Statuses map[string]boardStatusEntry `json:"statuses"`
}

// GetBoardStatus returns a snapshot of panemux's in-memory board status
// cache. No AgmsgClient call happens on this request — see
// docs/agent-board.md's Architecture section.
func (h *Handler) GetBoardStatus(w http.ResponseWriter, r *http.Request) {
	if h.boardCache == nil {
		http.Error(w, "agent board is not configured", http.StatusServiceUnavailable)
		return
	}
	snap := h.boardCache.StatusSnapshot()
	out := make(map[string]boardStatusEntry, len(snap))
	for paneID, s := range snap {
		out[paneID] = boardStatusEntry{
			State: s.State, CWD: s.CWD, Branch: s.Branch, Repo: s.Repo,
			PRURL: s.PRURL, LastTool: s.LastTool, Summary: s.Summary, UpdatedAt: s.UpdatedAt,
		}
	}
	writeJSON(w, boardStatusResponse{Statuses: out})
}

type boardMessageEntry struct {
	At   time.Time `json:"at"`
	Host string    `json:"host"`
	Team string    `json:"team"`
	From string    `json:"from"`
	To   string    `json:"to"`
	Body string    `json:"body"`
	Seq  int64     `json:"seq"`
}

type boardMessagesResponse struct {
	Messages []boardMessageEntry `json:"messages"`
	Seq      int64               `json:"seq"`
}

// GetBoardMessages returns the dashboard's history feed, paginated with
// ?since=<seq> — BoardCache's own panemux-local sequence number, not an
// agmsg-native id (those aren't comparable across hosts). Like
// GetBoardStatus, this never calls AgmsgClient at request time — see
// docs/agent-board.md's API additions.
func (h *Handler) GetBoardMessages(w http.ResponseWriter, r *http.Request) {
	if h.boardCache == nil {
		http.Error(w, "agent board is not configured", http.StatusServiceUnavailable)
		return
	}
	since, err := parseSinceParam(r.URL.Query().Get("since"))
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}
	msgs := h.boardCache.MessagesSince(since)
	out := make([]boardMessageEntry, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, boardMessageEntry{
			Seq: m.Seq, Host: m.Row.Host, Team: m.Row.Team,
			From: m.Row.From, To: m.Row.To, Body: m.Row.Body, At: m.Row.At,
		})
	}
	writeJSON(w, boardMessagesResponse{Messages: out, Seq: h.boardCache.LatestSeq()})
}

func parseSinceParam(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	return strconv.ParseInt(v, 10, 64) //nolint:wrapcheck // caller only needs ok/not-ok
}

type boardBroadcastRequest struct { //nolint:govet // fieldalignment: clarity preferred
	To   []string `json:"to"`
	Body string   `json:"body"`
}

type boardBroadcastResultEntry struct {
	Pane  string `json:"pane"`
	Error string `json:"error,omitempty"`
}

type boardBroadcastResponse struct {
	Results []boardBroadcastResultEntry `json:"results"`
}

// PostBoardBroadcast sends body from the reserved _panemux identity to
// every pane in the request's `to` list, resolving each pane's host and
// relaying via that host's AgmsgClient — never via PTY injection, so it is
// safe to send to a pane mid-turn. See docs/agent-board.md's API additions.
func (h *Handler) PostBoardBroadcast(w http.ResponseWriter, r *http.Request) {
	if h.boardBroadcaster == nil {
		http.Error(w, "agent board is not configured", http.StatusServiceUnavailable)
		return
	}
	var req boardBroadcastRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.To) == 0 {
		http.Error(w, "to must not be empty", http.StatusUnprocessableEntity)
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		http.Error(w, "body must not be empty", http.StatusUnprocessableEntity)
		return
	}

	results := h.boardBroadcaster.Broadcast(r.Context(), h.boardDefaultTeam, req.To, req.Body)
	out := make([]boardBroadcastResultEntry, 0, len(results))
	for _, res := range results {
		out = append(out, boardBroadcastResultEntry{Pane: res.Pane, Error: res.Error})
	}
	writeJSON(w, boardBroadcastResponse{Results: out})
}
