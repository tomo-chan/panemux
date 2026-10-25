// Package server wires together the chi router, API handlers, and embedded frontend.
package server

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"panemux/internal/api"
	"panemux/internal/board"
	"panemux/internal/commandcenter"
	"panemux/internal/config"
	"panemux/internal/portforward"
	"panemux/internal/requestsecurity"
	"panemux/internal/session"
	"panemux/internal/taskevents"
	"panemux/internal/ws"
)

// Server is the HTTP server.
type Server struct {
	cfg      *config.Config
	manager  *session.Manager
	httpSrv  *http.Server
	forwards *portforward.Registry
	// taskEvents publishes the task event stream, /ws/tasks/events.
	taskEvents *taskevents.Publisher
	api        *api.Handler
}

// taskEventOptions configures the task event publisher New builds. The
// contract fixture shortens its cycle; nothing else changes it.
var taskEventOptions = taskevents.Options{}

// New creates a new server instance. commandRunner may be nil when
// command_center.enabled is false — the /ws/board/command route is simply
// not registered in that case, so it 404s like any other undefined route
// rather than panicking on a nil runner.
func New(
	cfg *config.Config, manager *session.Manager, boardCache *board.BoardCache, boardRelay *board.Relay,
	commandRunner *commandcenter.Runner, frontendFS embed.FS, handlers ...*api.Handler,
) *Server {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)
	r.Use(securityHeadersMiddleware)

	var apiHandler *api.Handler
	if len(handlers) > 0 {
		apiHandler = handlers[0]
	} else {
		apiHandler = api.NewHandler(cfg, manager, boardCache, boardRelay)
	}
	apiHandler.SetCommandCenterAvailable(commandRunner != nil)
	// Loopback forwards are process-wide state (they bind ports on this
	// host), so the server owns the registry and closes it on shutdown.
	forwards := portforward.New(portforward.Options{})
	apiHandler.SetPortForwards(forwards)
	wsHandler := ws.NewHandler(manager)
	// The task event publisher observes nothing until the stream has a
	// subscriber; the server owns it and closes it on shutdown.
	taskEvents := taskevents.New(apiHandler.TaskEventSource(), taskEventOptions)
	taskEventsHandler := ws.NewTaskEventsHandler(taskEvents)
	registerRoutes(r, apiHandler, wsHandler, taskEventsHandler, commandRunner, frontendFS, cfg.Server.AuthToken)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &Server{
		cfg:        cfg,
		manager:    manager,
		forwards:   forwards,
		taskEvents: taskEvents,
		api:        apiHandler,
		httpSrv: &http.Server{
			Addr:           addr,
			Handler:        r,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   0, // no timeout for WebSocket connections
			IdleTimeout:    120 * time.Second,
			MaxHeaderBytes: 1 << 20,
		},
	}
}

func registerRoutes(
	r chi.Router, apiHandler *api.Handler, wsHandler *ws.Handler, taskEventsHandler *ws.TaskEventsHandler,
	commandRunner *commandcenter.Runner, frontendFS embed.FS, authToken string,
) {
	// The route table itself lives in internal/api (api.Handler.Mount), so
	// this wiring and the api package's own handler tests cannot describe
	// different routes. What stays here is the decision this package owns:
	// which part of the table is authenticated.
	//
	// api.BoardRoutePrefix is the only part of the API gated behind bearer-
	// token auth today: unlike every other /api/* route and /ws/{sessionID},
	// these endpoints were gated from day one, before the frontend called
	// any of them — GetBoardStatus/GetBoardMessages are now polled by the
	// dashboard's useBoardStatus hook and GetBoardCommandHistory by the
	// command palette/history panel (see docs/agent-board.md), but the
	// auth gate itself was never contingent on that. Retrofitting auth
	// onto the rest of the table, which the frontend already relies on
	// being unauthenticated, is a separate, larger change. See
	// docs/security.md.
	apiHandler.Mount(r, bearerAuthMiddleware(authToken))
	r.Get("/ws/{sessionID}", wsHandler.ServeHTTP)
	// The task event stream is unauthenticated like /ws/{sessionID} and
	// GET /api/tasks, and refuses a cross-site request itself. Its two
	// segments cannot be taken by a pane id, which /ws/{sessionID} matches
	// as one segment. See docs/behavior/task-events.md.
	r.Get("/ws/tasks/events", taskEventsHandler.ServeHTTP)
	// /ws/board/command is only registered when the command center is
	// enabled (commandRunner != nil) — see docs/agent-board.md's Command
	// center section. Unlike /ws/{sessionID}, this route requires the
	// bearer token, mirroring /api/board/*.
	if commandRunner != nil {
		boardCommandHandler := ws.NewBoardCommandHandler(commandRunner, authToken)
		r.Get("/ws/board/command", boardCommandHandler.ServeHTTP)
	}
	registerFrontend(r, frontendFS)
}

func registerFrontend(r chi.Router, frontendFS embed.FS) {
	distFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		r.Get("/*", http.FileServer(http.Dir("frontend/dist")).ServeHTTP)
		return
	}
	fileServer := http.FileServer(http.FS(distFS))
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		// SPA fallback: serve index.html for non-asset routes
		if _, err := distFS.Open(req.URL.Path[1:]); err != nil {
			req.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, req)
	})
}

// Addr returns the server address.
func (s *Server) Addr() string {
	return s.httpSrv.Addr
}

// Start begins listening and serving.
func (s *Server) Start() error {
	if err := s.httpSrv.ListenAndServe(); err != nil {
		if errors.Is(err, http.ErrServerClosed) {
			return http.ErrServerClosed
		}
		return fmt.Errorf("starting HTTP server: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server and closes every loopback port
// forward it opened on this host and every connection the task dashboard
// holds open.
func (s *Server) Shutdown(ctx context.Context) error {
	s.forwards.Close()
	s.taskEvents.Close()
	s.api.Close()
	if err := s.httpSrv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down HTTP server: %w", err)
	}
	return nil
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Guard the entire protected path before CORS/preflight or route dispatch,
		// including unsupported methods and routes not yet registered.
		protected := r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") ||
			r.URL.Path == "/ws" || strings.HasPrefix(r.URL.Path, "/ws/")
		if protected && requestsecurity.Refuse(w, r) {
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && requestsecurity.Allowed(r) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
