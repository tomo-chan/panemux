// Package ws provides the WebSocket handler that bridges terminal sessions to the browser.
package ws

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"panemux/internal/session"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all origins for local use
	},
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

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		//nolint:gosec // G706: sessionID is a config-defined identifier
		log.Printf("ws upgrade error for session %s: %v", sessionID, err)
		return
	}
	defer conn.Close() //nolint:errcheck

	h.sendStatus(conn, "connected")
	done := h.pipeTerminalToWebSocket(conn, sess, sessionID)
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
	sessionID string,
) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.forwardTerminalOutput(conn, sess, sessionID)
	}()
	return done
}

func (h *Handler) forwardTerminalOutput(
	conn *websocket.Conn,
	sess session.Session,
	sessionID string,
) {
	buf := make([]byte, 4096)
	for {
		n, err := sess.Read(buf)
		if n > 0 {
			if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				//nolint:gosec // G706: sessionID is a config-defined identifier
				log.Printf("session %s read error: %v", sessionID, err)
			}
			h.sendStatus(conn, "exited")
			return
		}
	}
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
