package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"panemux/internal/board"
	"panemux/internal/commandcenter"
)

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

// GetBoardStatus returns a snapshot of the board's in-memory status cache.
// No AgmsgClient call happens on this request — the relay is what keeps
// the cache current (see docs/agent-board.md's Architecture section).
func (h *Handler) GetBoardStatus(w http.ResponseWriter, r *http.Request) {
	snapshot := h.boardCache.StatusSnapshot()
	resp := boardStatusResponse{Statuses: make(map[string]boardStatusEntry, len(snapshot))}
	for paneID, s := range snapshot {
		resp.Statuses[paneID] = boardStatusEntry{
			State:     s.State,
			CWD:       s.CWD,
			Branch:    s.Branch,
			Repo:      s.Repo,
			PRURL:     s.PRURL,
			LastTool:  s.LastTool,
			Summary:   s.Summary,
			UpdatedAt: s.UpdatedAt,
		}
	}
	writeJSON(w, resp)
}

type boardMessageResponse struct {
	At   time.Time `json:"at"`
	Host string    `json:"host"`
	Team string    `json:"team"`
	From string    `json:"from"`
	To   string    `json:"to"`
	Body string    `json:"body"`
	Seq  int64     `json:"seq"`
}

type boardMessagesResponse struct {
	Messages []boardMessageResponse `json:"messages"`
}

// GetBoardMessages returns board history newer than the since query
// parameter (BoardCache's own local Seq, not an agmsg-native id — those
// aren't comparable across hosts). Missing since defaults to 0.
func (h *Handler) GetBoardMessages(w http.ResponseWriter, r *http.Request) {
	since := int64(0)
	if raw := r.URL.Query().Get("since"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "invalid since parameter", http.StatusBadRequest)
			return
		}
		since = v
	}

	rows := h.boardCache.MessagesSince(since)
	resp := boardMessagesResponse{Messages: make([]boardMessageResponse, 0, len(rows))}
	for _, cr := range rows {
		resp.Messages = append(resp.Messages, boardMessageResponse{
			Seq:  cr.Seq,
			Host: cr.Row.Host,
			Team: cr.Row.Team,
			From: cr.Row.From,
			To:   cr.Row.To,
			Body: cr.Row.Body,
			At:   cr.Row.At,
		})
	}
	writeJSON(w, resp)
}

type boardBroadcastRequest struct {
	Body string   `json:"body"`
	To   []string `json:"to"`
}

type boardBroadcastResponse struct {
	Delivered []string `json:"delivered"`
}

// boardBroadcastErrorResponse is the 502 body for a Broadcast call that
// failed partway through. Relay.Broadcast is fail-fast, not all-or-nothing,
// on a Send failure: it stops at the first error but still reports which
// pane IDs were successfully delivered before that point. Dropping that
// list (as a plain http.Error would) leaves the caller unable to tell which
// panes already got the message, risking a double-send on naive retry.
type boardBroadcastErrorResponse struct {
	Error     string   `json:"error"`
	Delivered []string `json:"delivered"`
}

// PostBoardBroadcast sends body to every pane ID in to, via the shared
// Relay's own-send-ledger-recording Broadcast — never via PTY injection, so
// it is safe to send to a pane mid-turn.
func (h *Handler) PostBoardBroadcast(w http.ResponseWriter, r *http.Request) {
	var req boardBroadcastRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.To) == 0 {
		writeValidationError(w, "to must contain at least one pane id")
		return
	}
	if req.Body == "" {
		writeValidationError(w, "body must not be empty")
		return
	}

	delivered, err := h.boardBroadcastFn(r.Context(), req.To, req.Body)
	if err != nil {
		var unknownErr *board.UnknownPaneError
		if errors.As(err, &unknownErr) {
			writeValidationError(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(boardBroadcastErrorResponse{Error: err.Error(), Delivered: delivered})
		return
	}
	writeJSON(w, boardBroadcastResponse{Delivered: delivered})
}

type commandHistoryEntryResponse struct {
	At  time.Time       `json:"at"`
	Raw json.RawMessage `json:"raw"`
}

type commandHistoryResponse struct {
	Entries []commandHistoryEntryResponse `json:"entries"`
}

// GetBoardCommandHistory returns the command center's own captured
// turn-by-turn conversation history — read directly from the local history
// file the Runner appends to while streaming a query's output, never
// re-derived from Claude Code's transcript after the fact. See
// docs/agent-board.md's "API and streaming" section. An empty or missing
// history file (the command center has never run, or is not enabled) is
// not an error; it returns an empty list.
func (h *Handler) GetBoardCommandHistory(w http.ResponseWriter, r *http.Request) {
	entries, err := h.commandHistoryFn()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to load command center history: %v", err), http.StatusInternalServerError)
		return
	}
	resp := commandHistoryResponse{Entries: make([]commandHistoryEntryResponse, 0, len(entries))}
	for _, e := range entries {
		resp.Entries = append(resp.Entries, commandHistoryEntryResponse{At: e.At, Raw: e.Raw})
	}
	writeJSON(w, resp)
}

type boardSessionTokenResponse struct {
	Token                string `json:"token"`
	CommandCenterEnabled bool   `json:"command_center_enabled"`
}

// GetBoardSessionToken lets the same-origin dashboard learn the bearer
// token panemux generated or was configured with, so its own JavaScript can
// authenticate the /api/board/* requests and the /ws/board-command
// WebSocket connection it makes on the user's behalf. There is no other way
// for the frontend to learn a token that may have been randomly generated
// on first run (see config.Config.EnsureAuthToken) and is never sent to the
// browser any other way.
//
// This endpoint is deliberately NOT behind bearerAuthMiddleware itself —
// nothing could ever bootstrap the token without already knowing it
// otherwise — and instead relies on the same protection every other
// pre-existing, unauthenticated /api/* route already relies on: the
// server's CORS policy only allows a loopback origin to read a cross-origin
// response body at all (see corsMiddleware/isLocalhostOrigin in
// internal/server), and the operator's own host is already the trust
// boundary for those routes. See docs/security.md's "Auth token and
// transport encryption" section.
func (h *Handler) GetBoardSessionToken(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, boardSessionTokenResponse{
		Token:                h.cfg.Server.AuthToken,
		CommandCenterEnabled: h.cfg.CommandCenter.Enabled,
	})
}

// defaultCommandHistoryFn reads the command center's history file from its
// default location. Resolving the path lazily on every call (rather than
// once at Handler construction) matches commandcenter.LoadHistory's own
// tolerance of a not-yet-existing file — nothing needs to exist before the
// command center's first successful query.
func defaultCommandHistoryFn() ([]commandcenter.HistoryEntry, error) {
	path, err := commandcenter.DefaultHistoryFilePath()
	if err != nil {
		return nil, fmt.Errorf("resolving command center history path: %w", err)
	}
	entries, err := commandcenter.LoadHistory(path)
	if err != nil {
		return nil, fmt.Errorf("loading command center history: %w", err)
	}
	return entries, nil
}
