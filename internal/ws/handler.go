// Package ws provides the WebSocket handler that bridges terminal sessions to the browser.
package ws

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"panemux/internal/session"
)

const wsReadLimitBytes = 1 << 20 // 1 MB

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     checkOrigin,
}

// checkOrigin validates WebSocket upgrade requests to prevent cross-site WebSocket
// hijacking (CSWSH).
//
// Loopback origins (localhost / 127.0.0.1 / ::1) are permitted regardless of port so
// that the Vite dev server on :5173 can proxy WebSocket traffic to the backend on
// :8080 without requiring changeOrigin in the proxy config.
//
// Requests without an Origin header are allowed; browsers always include Origin on
// cross-origin requests, so the absence of the header indicates a non-browser client
// (e.g. curl) that is not subject to the same-origin policy.
func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if isLoopbackHost(u.Host) {
		return true
	}
	return u.Host == r.Host
}

// isLoopbackHost returns true when the host part of an authority string
// (host or host:port) is a loopback address.
func isLoopbackHost(authority string) bool {
	host, _, err := net.SplitHostPort(authority)
	if err != nil {
		host = authority
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// ControlMessage is a JSON control frame exchanged over WebSocket.
type ControlMessage struct {
	Type    string `json:"type"`
	State   string `json:"state,omitempty"`
	Message string `json:"message,omitempty"`
	Cols    uint16 `json:"cols,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
}

// Handler handles WebSocket connections for terminal sessions.
type Handler struct {
	manager *session.Manager
}

// NewHandler creates a new WebSocket handler.
func NewHandler(manager *session.Manager) *Handler {
	return &Handler{manager: manager}
}

// ServeHTTP handles GET /ws/{sessionID}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	sess := h.sessionForRequest(w, sessionID)
	if sess == nil {
		return
	}

	snapshot, updates, unsubscribe, ok := h.manager.Subscribe(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	defer unsubscribe()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		//nolint:gosec // G706: sessionID is a config-defined identifier
		log.Printf("ws upgrade error for session %s: %v", sessionID, err)
		return
	}
	defer conn.Close() //nolint:errcheck
	conn.SetReadLimit(wsReadLimitBytes)

	h.sendStatus(conn, "connected")
	done := h.pipeTerminalToWebSocket(conn, sess, snapshot, updates, sessionID)
	h.pipeWebSocketToTerminal(conn, sess, sessionID)
	waitForTerminalPipe(done)
}

func (h *Handler) sessionForRequest(w http.ResponseWriter, sessionID string) session.Session {
	sess, ok := h.manager.Get(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return nil
	}
	return sess
}

func (h *Handler) pipeTerminalToWebSocket(
	conn *websocket.Conn,
	sess session.Session,
	snapshot []byte,
	updates <-chan []byte,
	sessionID string,
) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.forwardTerminalOutput(conn, sess, snapshot, updates, sessionID)
	}()
	return done
}

func (h *Handler) forwardTerminalOutput(
	conn *websocket.Conn,
	sess session.Session,
	snapshot []byte,
	updates <-chan []byte,
	sessionID string,
) {
	// Replay the buffered bytes before streaming new output. This is what keeps a
	// workspace switch from showing a blank xterm when the PTY already emitted the
	// prompt while the pane was unmounted.
	if len(snapshot) > 0 {
		h.sendReplay(conn, "start")
		if err := conn.WriteMessage(websocket.BinaryMessage, snapshot); err != nil {
			return
		}
		h.sendReplay(conn, "end")
	}

	for chunk := range updates {
		if len(chunk) == 0 {
			continue
		}
		if writeErr := conn.WriteMessage(websocket.BinaryMessage, chunk); writeErr != nil {
			return
		}
	}

	if sess.State() != session.StateConnected {
		//nolint:gosec // G706: sessionID is a config-defined identifier
		log.Printf("session %s stream closed in state %s", sessionID, sess.State())
	}

	h.sendStatus(conn, "exited")
}

func (h *Handler) pipeWebSocketToTerminal(
	conn *websocket.Conn,
	sess session.Session,
	sessionID string,
) {
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		h.handleWebSocketMessage(conn, sess, sessionID, msgType, data)
	}
}

func (h *Handler) handleWebSocketMessage(
	conn *websocket.Conn,
	sess session.Session,
	sessionID string,
	msgType int,
	data []byte,
) {
	switch msgType {
	case websocket.BinaryMessage:
		// Discard silently if the session is already gone.
		if sess.State() == session.StateExited {
			return
		}
		if _, err := sess.Write(data); err != nil {
			//nolint:gosec // G706: sessionID is a config-defined identifier
			log.Printf("session %s write error: %v", sessionID, err)
		}

	case websocket.TextMessage:
		var msg ControlMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("invalid control message: %v", err)
			return
		}
		h.handleControl(conn, sess, msg)
	}
}

func waitForTerminalPipe(done <-chan struct{}) {
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func (h *Handler) handleControl(conn *websocket.Conn, sess session.Session, msg ControlMessage) {
	switch msg.Type {
	case "resize":
		if msg.Cols > 0 && msg.Rows > 0 {
			if err := sess.Resize(msg.Cols, msg.Rows); err != nil {
				log.Printf("resize error: %v", err)
			}
		}
	}
}

func (h *Handler) sendStatus(conn *websocket.Conn, state string) {
	msg := ControlMessage{Type: "status", State: state}
	data, _ := json.Marshal(msg)
	_ = conn.WriteMessage(websocket.TextMessage, data)
}

func (h *Handler) sendReplay(conn *websocket.Conn, state string) {
	msg := ControlMessage{Type: "replay", State: state}
	data, _ := json.Marshal(msg)
	_ = conn.WriteMessage(websocket.TextMessage, data)
}
