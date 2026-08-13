package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"panemux/internal/board"
	"panemux/internal/commandcenter"
)

// loopbackIPv4 is the literal IPv4 loopback host string, as it appears in a
// Host header or server.host config value (as opposed to a parsed net.IP).
const loopbackIPv4 = "127.0.0.1"

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
// otherwise. An earlier revision of this doc comment claimed the server's
// CORS policy protects it; that claim was wrong and has been corrected —
// CORS only controls whether a cross-origin script can *read* a response
// body, it never rejects the request from reaching the handler at all, and
// a non-browser client (curl, another process on the LAN) ignores CORS
// entirely. The real guard is below, checked directly against the request
// rather than delegated to a header a client fully controls:
//
//  1. r.RemoteAddr's IP must be loopback. This is what actually restricts
//     this endpoint to the local machine — it rejects any client that
//     genuinely isn't the box panemux is running on, including a client on
//     a LAN that reaches a `server.host` bound to a non-loopback address
//     (a configuration internal/config/validate.go's own
//     non-loopback-requires-token rule explicitly allows). Handing the
//     token to any such client would defeat the entire reason that rule
//     requires a token in the first place.
//  2. r.Host must also resolve to a loopback authority. RemoteAddr alone is
//     not enough: DNS rebinding (a domain whose DNS answer changes to
//     127.0.0.1 after the browser's same-origin check already passed) makes
//     an attacker-controlled page's requests arrive with a genuinely
//     loopback RemoteAddr — the TCP connection really is local — while the
//     Host header still carries the attacker's own domain, since browsers
//     send the navigation URL's original host, not the resolved IP. Only
//     the Host check catches that case.
//
// See docs/security.md's "Auth token and transport encryption" section for
// the full rationale, including the accepted limitation this creates: a
// dashboard served from a genuinely non-loopback `server.host` can no
// longer bootstrap its own token through this endpoint at all, by design —
// and the narrower, NOT fully closed gap a same-host reverse proxy still
// opens, which the forwarding-header check below only partially mitigates.
// See that same security.md section for why a complete fix needs an
// operator-configured trusted-proxy allowlist this endpoint does not have.
func (h *Handler) GetBoardSessionToken(w http.ResponseWriter, r *http.Request) {
	if isProxiedRequest(r) || !isLoopbackRemoteAddr(r.RemoteAddr) || !isLoopbackAuthority(r.Host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, boardSessionTokenResponse{
		Token:                h.cfg.Server.AuthToken,
		CommandCenterEnabled: h.commandCenterAvailable,
	})
}

// isProxiedRequest reports whether r carries any of the standard
// client-address forwarding headers (X-Forwarded-For, X-Real-IP, the RFC
// 7239 Forwarded header). A genuine direct request from a local browser
// never carries these — only a request that passed through a reverse proxy
// does — so their mere presence is treated as proof the RemoteAddr/Host
// pair below can no longer be trusted to mean "this really is the local
// machine": a same-host reverse proxy (the exact mitigation this
// repository's own docs recommend for exposing panemux beyond loopback)
// makes every proxied request's RemoteAddr loopback and, under common
// proxy defaults, its Host loopback-looking too, for a client that may be
// anywhere on the internet. This is a partial mitigation, not a complete
// one: a proxy configured not to set any of these headers is not caught by
// it. See docs/security.md's "Auth token and transport encryption" section
// for the accepted residual gap.
func isProxiedRequest(r *http.Request) bool {
	return r.Header.Get("X-Forwarded-For") != "" ||
		r.Header.Get("X-Real-IP") != "" ||
		r.Header.Get("Forwarded") != ""
}

// isLoopbackAuthority reports whether authority (a Host-header-shaped
// "host" or "host:port" string) names a loopback address, regardless of
// port — matching internal/server's own isLocalhostOrigin/isLoopbackHost
// port-agnostic allowance for the Vite dev server's proxied port. "0.0.0.0"
// is accepted too: a wildcard-bound server.host reached via `--open`
// launches the browser at exactly http://0.0.0.0:<port>/, so the
// dashboard's own same-origin request legitimately carries that literal
// Host value — this does not weaken the DNS-rebinding defense, since an
// attacker would need a victim to navigate to a URL whose hostname is
// literally "0.0.0.0", which a DNS answer for an attacker-chosen domain
// cannot produce (Host reflects the URL's original hostname, never the
// resolved IP).
func isLoopbackAuthority(authority string) bool {
	host, _, err := net.SplitHostPort(authority)
	if err != nil {
		host = authority
	}
	return host == "localhost" || host == loopbackIPv4 || host == "::1" || host == "0.0.0.0"
}

// isLoopbackRemoteAddr reports whether remoteAddr (an http.Request's own
// RemoteAddr, always "IP:port" for a real TCP connection) is a loopback IP.
// Unlike isLoopbackAuthority, this is checked against net.IP.IsLoopback
// rather than string equality, since RemoteAddr is the net/http-reported
// address of the actual socket peer, not attacker-controlled request
// content — it can carry any valid loopback representation (including
// IPv4-mapped IPv6 forms), not just the literal strings a Host header
// realistically contains.
func isLoopbackRemoteAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
