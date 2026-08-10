package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"panemux/internal/board"
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
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, boardBroadcastResponse{Delivered: delivered})
}
