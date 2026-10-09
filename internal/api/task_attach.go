package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

// The board's temporary attach (issue #283): a tmux client the task dashboard
// opens on a task's running tmux session, served by /ws/{id} like a pane but
// never part of the layout. It is destroyed by DELETE, or once no WebSocket
// has read it for boardAttachGrace; destroying it ends only that client.

// boardAttachGrace is how long an attach no WebSocket reads is kept, so a
// reload or a dropped connection can reconnect to the same client.
const boardAttachGrace = 10 * time.Second

// boardAttachIDPrefix starts every board attach's session ID.
const boardAttachIDPrefix = "board-"

type boardAttachTimer interface{ Stop() bool }

// TmuxAttachFactory creates a board attach's tmux client: session id, on the
// connection ("" for the panemux host), attached to tmuxSession.
type TmuxAttachFactory func(
	id, title, connection, tmuxSession string, sshConns map[string]config.SSHConnection,
) (session.Session, error)

type boardAttach struct {
	timer       boardAttachTimer // running while no WebSocket reads the session
	taskID      string
	sessionID   string
	tmuxSession string
	// connection is a host terminal's ssh_connections entry.
	connection string
	// generation tells a timer that fired after it was stopped that it no
	// longer counts.
	generation int
	// host marks a host terminal (issue #314) rather than a task's attach.
	host bool
}

// boardAttaches holds the board's attaches, one per task, and its host
// terminals, each under a key of its own.
type boardAttaches struct {
	afterFunc func(time.Duration, func()) boardAttachTimer
	byTask    map[string]*boardAttach
	bySession map[string]*boardAttach
	// creating holds a channel per task whose attach is being created; it
	// is closed when the creation ends either way.
	creating map[string]chan struct{}
	// hostOpening counts, per connection, the host terminals being opened.
	hostOpening map[string]int
	mu          sync.Mutex
}

func newBoardAttaches() *boardAttaches {
	return &boardAttaches{
		afterFunc: func(d time.Duration, fn func()) boardAttachTimer { return time.AfterFunc(d, fn) },
		byTask:    map[string]*boardAttach{},
		bySession: map[string]*boardAttach{},
		creating:  map[string]chan struct{}{},

		hostOpening: map[string]int{},
	}
}

func (b *boardAttaches) isAttach(sessionID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.bySession[sessionID]
	return ok
}

// forgetLocked drops an attach from the registry. b.mu must be held. A
// timer of the attach that fires afterwards finds it gone from bySession;
// that holds because registerBoardAttach makes a new *boardAttach each time,
// so a forgotten pointer is never registered again.
func (b *boardAttaches) forgetLocked(attach *boardAttach) {
	if attach.timer != nil {
		attach.timer.Stop()
		attach.timer = nil
	}
	delete(b.byTask, attach.taskID)
	delete(b.bySession, attach.sessionID)
}

// forget drops the attach of sessionID and returns it, or nil when there was
// none of that kind: a host terminal when host is set, a task's attach
// otherwise.
func (b *boardAttaches) forget(sessionID string, host bool) *boardAttach {
	b.mu.Lock()
	defer b.mu.Unlock()
	attach, ok := b.bySession[sessionID]
	if !ok || attach.host != host {
		return nil
	}
	b.forgetLocked(attach)
	return attach
}

func (b *boardAttaches) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, attach := range b.bySession {
		b.forgetLocked(attach)
	}
}

// taskAttachRequest is the body of POST /api/tasks/attach.
type taskAttachRequest struct {
	ID string `json:"id"`
}

// taskAttachResponse answers POST /api/tasks/attach.
type taskAttachResponse struct {
	SessionID   string `json:"session_id"`
	TmuxSession string `json:"tmux_session"`
}

// PostTaskAttach opens the board's attach to a task's running tmux session,
// or returns the one already open for that task. The tmux session and host
// come from a fresh collection of the task's host, never from the request.
func (h *Handler) PostTaskAttach(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	var req taskAttachRequest
	if !decodeStrict(w, r, taskRecordBodyLimit, &req) {
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	existing, done, err := h.reserveBoardAttach(r, req.ID)
	if err != nil {
		return // the request ended while another creation was running
	}
	if existing != nil {
		writeJSON(w, existing)
		return
	}
	defer done()

	task, err := h.tasks.FindTask(r.Context(), req.ID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, tasks.ErrTaskNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	if task.Location.Kind != tasks.LocationTmux || !task.Location.Attachable {
		http.Error(w, "the task is not running in a tmux session a pane can attach to", http.StatusConflict)
		return
	}

	sessionID := h.newBoardAttachID()
	name := task.Location.TmuxSession
	sess, err := h.createTmuxAttach(sessionID, name, task.Host, name, h.sshConnections())
	if err != nil {
		http.Error(w, fmt.Sprintf("attach to tmux session %q: %v", name, err), http.StatusBadGateway)
		return
	}
	h.registerBoardAttach(&boardAttach{taskID: req.ID, sessionID: sess.ID(), tmuxSession: name}, sess)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(taskAttachResponse{SessionID: sessionID, TmuxSession: name})
}

// reserveBoardAttach returns the task's open attach, or reserves the task's
// creation and returns the func that releases it. A request for a task whose
// attach is being created waits for that creation rather than starting a
// second one. An attach whose tmux client has exited is removed so a new one
// can be made. A host terminal's entry counts as absent: this route serves
// and removes only a task's attach.
func (h *Handler) reserveBoardAttach(r *http.Request, taskID string) (*taskAttachResponse, func(), error) {
	b := h.boardAttaches
	for {
		b.mu.Lock()
		if attach, ok := b.byTask[taskID]; ok && !attach.host {
			sess, live := h.manager.Get(attach.sessionID)
			if live && sess.State() != session.StateExited {
				b.mu.Unlock()
				return &taskAttachResponse{SessionID: attach.sessionID, TmuxSession: attach.tmuxSession}, nil, nil
			}
			b.forgetLocked(attach)
			b.mu.Unlock()
			h.removeBoardAttachSession(attach.sessionID)
			continue
		}
		if wait, ok := b.creating[taskID]; ok {
			b.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-r.Context().Done():
				return nil, nil, fmt.Errorf("wait for the task's attach: %w", r.Context().Err())
			}
		}
		done := make(chan struct{})
		b.creating[taskID] = done
		b.mu.Unlock()
		return nil, func() {
			b.mu.Lock()
			delete(b.creating, taskID)
			b.mu.Unlock()
			close(done)
		}, nil
	}
}

// registerBoardAttach serves sess as attach, which must be new, and starts
// its grace period: until a WebSocket subscribes, nothing reads it.
func (h *Handler) registerBoardAttach(attach *boardAttach, sess session.Session) {
	b := h.boardAttaches
	b.mu.Lock()
	b.byTask[attach.taskID] = attach
	b.bySession[attach.sessionID] = attach
	h.armBoardAttachLocked(attach)
	b.mu.Unlock()

	h.manager.Add(sess)
	h.manager.Watch(attach.sessionID, func(subscribers int) {
		h.boardAttachSubscribers(attach, subscribers)
	})
}

func (h *Handler) boardAttachSubscribers(attach *boardAttach, subscribers int) {
	b := h.boardAttaches
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bySession[attach.sessionID] != attach {
		return
	}
	if subscribers > 0 {
		if attach.timer != nil {
			attach.timer.Stop()
			attach.timer = nil
			attach.generation++
		}
		return
	}
	if attach.timer == nil {
		h.armBoardAttachLocked(attach)
	}
}

// armBoardAttachLocked starts attach's grace period. b.mu must be held.
func (h *Handler) armBoardAttachLocked(attach *boardAttach) {
	b := h.boardAttaches
	attach.generation++
	generation := attach.generation
	attach.timer = b.afterFunc(boardAttachGrace, func() {
		b.mu.Lock()
		if b.bySession[attach.sessionID] != attach || attach.generation != generation {
			b.mu.Unlock()
			return
		}
		b.forgetLocked(attach)
		b.mu.Unlock()
		h.removeBoardAttachSession(attach.sessionID)
	})
}

func (h *Handler) removeBoardAttachSession(sessionID string) {
	if err := h.manager.Remove(sessionID); err != nil {
		log.Printf("board attach %s: %v", sessionID, err)
	}
}

// newBoardAttachID returns a session ID no registered session has.
func (h *Handler) newBoardAttachID() string {
	for {
		buf := make([]byte, 8)
		// crypto/rand.Read never returns an error (since Go 1.24 it crashes
		// the program instead), so there is no error to handle.
		_, _ = rand.Read(buf)
		id := boardAttachIDPrefix + hex.EncodeToString(buf)
		if _, taken := h.manager.Get(id); !taken {
			return id
		}
	}
}

// DeleteTaskAttach destroys a board attach: its tmux client ends, and the
// task's tmux session and agent keep running.
func (h *Handler) DeleteTaskAttach(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	attach := h.boardAttaches.forget(chi.URLParam(r, "id"), false)
	if attach == nil {
		http.Error(w, "no such board attach", http.StatusNotFound)
		return
	}
	// The ID logged and removed is the registry's own, minted by
	// newBoardAttachID, not the request's.
	h.removeBoardAttachSession(attach.sessionID)
	w.WriteHeader(http.StatusNoContent)
}

// SetTmuxAttachFactory replaces how a board attach's tmux client is created.
// It is the seam the server's route tests use instead of a real tmux.
func (h *Handler) SetTmuxAttachFactory(fn TmuxAttachFactory) {
	h.createTmuxAttach = fn
}

// SetSessionFactory replaces how a host terminal's session is created. It is
// the seam the server's route tests use instead of a real ssh connection.
func (h *Handler) SetSessionFactory(
	fn func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error),
) {
	h.createSession = fn
}
