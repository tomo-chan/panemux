package ws

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"panemux/internal/commandcenter"
)

var boardCommandUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     checkOrigin,
}

// boardCommandWriteTimeout bounds every WriteMessage call this handler
// makes. Without it, a client that stops reading without closing its TCP
// connection (a sleeping laptop, a dropped network with no FIN) makes
// WriteMessage block indefinitely rather than error — the "keep draining
// without writing" logic in streamBoardCommandEvents only helps once a
// write actually fails, so an un-deadlined write can still wedge the
// Runner's single-query busy flag forever.
const boardCommandWriteTimeout = 10 * time.Second

// boardCommandFrame.Type wire values — see docs/agent-board.md's "API and
// streaming" section.
const (
	boardCommandFrameTypeLine  = "line"
	boardCommandFrameTypeError = "error"
	boardCommandFrameTypeDone  = "done"
	boardCommandFrameTypeBusy  = "busy"
)

// boardCommandConn is the subset of *websocket.Conn this handler writes
// through, so tests can substitute a fake connection without a real TCP
// socket. *websocket.Conn satisfies this directly.
type boardCommandConn interface {
	WriteMessage(messageType int, data []byte) error
	SetWriteDeadline(t time.Time) error
}

// boardCommandRunner is the subset of *commandcenter.Runner the WS handler
// needs, so tests can substitute a fake query without a real `claude`
// subprocess. *commandcenter.Runner satisfies this directly.
type boardCommandRunner interface {
	Query(ctx context.Context, prompt string) (<-chan commandcenter.Event, error)
}

// boardCommandRequest is the client->server WS frame: one command center
// prompt per message.
type boardCommandRequest struct {
	Prompt string `json:"prompt"`
}

// boardCommandFrame is the server->client WS frame. Type is one of "line",
// "error", "done", or "busy" — see docs/agent-board.md's "API and
// streaming" section.
//
//nolint:govet // fieldalignment: field order kept as Type/Raw/Message for readability, padding cost is negligible
type boardCommandFrame struct {
	Type    string          `json:"type"`
	Raw     json.RawMessage `json:"raw,omitempty"`
	Message string          `json:"message,omitempty"`
}

// BoardCommandHandler serves WS /ws/board-command: the command center chat
// used by the Spotlight palette. Unlike /ws/{sessionID}, this route
// requires the bearer token, matching every other /api/board/* endpoint —
// see docs/security.md. Browsers cannot set an Authorization header on a
// WebSocket upgrade request, so the token travels as a WebSocket
// subprotocol (`new WebSocket(url, [token])`) instead of a query string,
// keeping it out of server access logs, browser history, and same-origin
// Referer headers.
type BoardCommandHandler struct {
	runner boardCommandRunner
	token  string
}

// NewBoardCommandHandler constructs a BoardCommandHandler backed by runner,
// requiring token on every connection.
func NewBoardCommandHandler(runner boardCommandRunner, token string) *BoardCommandHandler {
	return &BoardCommandHandler{runner: runner, token: token}
}

// ServeHTTP handles GET /ws/board-command.
func (h *BoardCommandHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	proto := r.Header.Get("Sec-WebSocket-Protocol")
	if !validSubprotocolToken(proto, h.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	responseHeader := http.Header{}
	responseHeader.Set("Sec-WebSocket-Protocol", proto)
	conn, err := boardCommandUpgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		log.Printf("board command ws upgrade error: %v", err)
		return
	}
	defer conn.Close() //nolint:errcheck
	conn.SetReadLimit(wsReadLimitBytes)

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType != websocket.TextMessage {
			continue
		}
		if !h.handlePrompt(r.Context(), conn, data) {
			return
		}
	}
}

// handlePrompt processes one client prompt message. It returns false when
// the underlying connection has failed and the caller should stop reading
// further messages.
func (h *BoardCommandHandler) handlePrompt(ctx context.Context, conn boardCommandConn, data []byte) bool {
	var req boardCommandRequest
	if err := json.Unmarshal(data, &req); err != nil {
		frame := boardCommandFrame{Type: boardCommandFrameTypeError, Message: "invalid request: " + err.Error()}
		return writeBoardCommandFrame(conn, frame)
	}

	events, err := h.runner.Query(ctx, req.Prompt)
	if err != nil {
		if errors.Is(err, commandcenter.ErrBusy) {
			return writeBoardCommandFrame(conn, boardCommandFrame{Type: boardCommandFrameTypeBusy})
		}
		return writeBoardCommandFrame(conn, boardCommandFrame{Type: boardCommandFrameTypeError, Message: err.Error()})
	}
	return streamBoardCommandEvents(conn, events)
}

// streamBoardCommandEvents forwards every event to conn, in order. Once a
// write fails, it keeps ranging over events without writing further — never
// abandoning the channel — so the Runner's own goroutine (and its busy
// flag) is never left blocked sending into a channel nobody drains.
func streamBoardCommandEvents(conn boardCommandConn, events <-chan commandcenter.Event) bool {
	ok := true
	for ev := range events {
		if !ok {
			continue
		}
		if !writeBoardCommandFrame(conn, eventToBoardCommandFrame(ev)) {
			ok = false
		}
	}
	return ok
}

func eventToBoardCommandFrame(ev commandcenter.Event) boardCommandFrame {
	switch ev.Type {
	case commandcenter.EventLine:
		return boardCommandFrame{Type: boardCommandFrameTypeLine, Raw: ev.Raw}
	case commandcenter.EventError:
		return boardCommandFrame{Type: boardCommandFrameTypeError, Message: ev.Err}
	case commandcenter.EventDone:
		return boardCommandFrame{Type: boardCommandFrameTypeDone}
	default:
		return boardCommandFrame{Type: boardCommandFrameTypeError, Message: "unknown event type: " + string(ev.Type)}
	}
}

func writeBoardCommandFrame(conn boardCommandConn, frame boardCommandFrame) bool {
	data, err := json.Marshal(frame)
	if err != nil {
		return false
	}
	// A deadline error from the connection itself (e.g. it's already
	// closed) is treated the same as a failed write below — either way the
	// caller must stop writing and fall back to draining only.
	if err := conn.SetWriteDeadline(time.Now().Add(boardCommandWriteTimeout)); err != nil {
		return false
	}
	return conn.WriteMessage(websocket.TextMessage, data) == nil
}

// validSubprotocolToken reports whether proto (the client's offered
// Sec-WebSocket-Protocol value) matches token, using a constant-time
// comparison so response timing cannot be used to guess the token
// byte-by-byte — mirrors internal/server/auth.go's bearerAuthMiddleware,
// duplicated here because that check is header-shaped
// ("Authorization: Bearer <token>") and this one is not.
func validSubprotocolToken(proto, token string) bool {
	if token == "" || proto == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(proto), []byte(token)) == 1
}
