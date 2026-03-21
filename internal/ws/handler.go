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
	Cols    uint16 `json:"cols,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
	State   string `json:"state,omitempty"`
	Message string `json:"message,omitempty"`
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

	sess, ok := h.manager.Get(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error for session %s: %v", sessionID, err)
		return
	}
	defer conn.Close()

	// Send initial connected status
	h.sendStatus(conn, "connected")

	// Monitor session state changes
	done := make(chan struct{})

	// terminal → WebSocket (binary frames)
	go func() {
		defer close(done)
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
					log.Printf("session %s read error: %v", sessionID, err)
				}
				h.sendStatus(conn, "exited")
				return
			}
		}
	}()

	// WebSocket → terminal
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		switch msgType {
		case websocket.BinaryMessage:
			// Raw terminal input
			if _, err := sess.Write(data); err != nil {
				log.Printf("session %s write error: %v", sessionID, err)
			}

		case websocket.TextMessage:
			// JSON control message
			var msg ControlMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Printf("invalid control message: %v", err)
				continue
			}
			h.handleControl(conn, sess, msg)
		}
	}

	// Wait for the terminal reader goroutine to finish
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
	conn.WriteMessage(websocket.TextMessage, data)
}
