package ws

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"panemux/internal/taskevents"
)

// taskEventsReadLimit bounds a frame the client sends. The stream reads
// nothing from the client; the limit only keeps a client from making the
// server buffer a large one.
const taskEventsReadLimit = 512

var taskEventsUpgrader = websocket.Upgrader{
	ReadBufferSize:  taskEventsReadLimit,
	WriteBufferSize: 4096,
	CheckOrigin:     checkOrigin,
}

// TaskEventsHandler serves the task event stream, GET /ws/tasks/events
// (issue #293): a snapshot of the running tasks on every host, then a frame
// per change. See docs/behavior/task-events.md.
type TaskEventsHandler struct {
	publisher *taskevents.Publisher
	// refuseCrossSite answers 403 to a request another site's page made and
	// reports whether it did; it is internal/api's rule for the task routes.
	refuseCrossSite func(http.ResponseWriter, *http.Request) bool
}

// NewTaskEventsHandler serves publisher's stream, refusing what
// refuseCrossSite refuses.
func NewTaskEventsHandler(
	publisher *taskevents.Publisher, refuseCrossSite func(http.ResponseWriter, *http.Request) bool,
) *TaskEventsHandler {
	return &TaskEventsHandler{publisher: publisher, refuseCrossSite: refuseCrossSite}
}

// ServeHTTP is not authenticated, like /ws/{sessionID} and GET /api/tasks,
// and like GET /api/tasks refuses a cross-site request before anything is
// collected: subscribing starts observing every host. The upgrade then makes
// the same Origin check as /ws/{sessionID}.
func (h *TaskEventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.refuseCrossSite(w, r) {
		return
	}
	conn, err := taskEventsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade has already answered: 403 for a refused Origin.
		return
	}
	defer func() { _ = conn.Close() }()
	conn.SetReadLimit(taskEventsReadLimit)

	snapshot, frames, cancel := h.publisher.Subscribe()
	defer cancel()

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// A write fails only once the client dropped the connection, which the
	// reader goroutine races to notice first; no test can order the two.
	//coverage:exempt a dropped client's failed write races the reader noticing the drop
	if !writeTaskEventFrame(conn, snapshot) {
		return
	}
	for {
		select {
		case <-closed:
			return
		case frame, ok := <-frames:
			if !ok {
				// Fallen behind, or the server is shutting down: the client
				// reconnects from a fresh snapshot.
				return
			}
			//coverage:exempt a dropped client's failed write races the reader noticing the drop
			if !writeTaskEventFrame(conn, frame) {
				return
			}
		}
	}
}

func writeTaskEventFrame(conn *websocket.Conn, frame taskevents.Frame) bool {
	data, err := json.Marshal(frame)
	//coverage:exempt Frame.MarshalJSON fails only where its own json.Marshal does, marked so there
	if err != nil {
		log.Printf("task events: %v", err)
		return false
	}
	// No write deadline: the stream is meant for a browser on the same
	// machine, whose closed tab closes the socket at once. A connection that
	// stops reading only falls behind, and the publisher closes its
	// subscription (docs/DECISIONLOG.md, issue #277).
	return conn.WriteMessage(websocket.TextMessage, data) == nil
}
