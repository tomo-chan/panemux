// Package server wires together the chi router, API handlers, and embedded frontend.
package server

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"panemux/internal/api"
	"panemux/internal/board"
	"panemux/internal/commandcenter"
	"panemux/internal/config"
	"panemux/internal/portforward"
	"panemux/internal/session"
	"panemux/internal/ws"
)

// Server is the HTTP server.
type Server struct {
	cfg      *config.Config
	manager  *session.Manager
	httpSrv  *http.Server
	forwards *portforward.Registry
}

// New creates a new server instance. commandRunner may be nil when
// command_center.enabled is false — the /ws/board-command route is simply
// not registered in that case, so it 404s like any other undefined route
// rather than panicking on a nil runner.
func New(
	cfg *config.Config, manager *session.Manager, boardCache *board.BoardCache, boardRelay *board.Relay,
	commandRunner *commandcenter.Runner, frontendFS embed.FS,
) *Server {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)
	r.Use(securityHeadersMiddleware)

	apiHandler := api.NewHandler(cfg, manager, boardCache, boardRelay)
	apiHandler.SetCommandCenterAvailable(commandRunner != nil)
	// Loopback forwards are process-wide state (they bind ports on this
	// host), so the server owns the registry and closes it on shutdown.
	forwards := portforward.New(portforward.Options{})
	apiHandler.SetPortForwards(forwards)
	wsHandler := ws.NewHandler(manager)
	registerRoutes(r, apiHandler, wsHandler, commandRunner, frontendFS, cfg.Server.AuthToken)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &Server{
		cfg:      cfg,
		manager:  manager,
		forwards: forwards,
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
	r chi.Router, apiHandler *api.Handler, wsHandler *ws.Handler, commandRunner *commandcenter.Runner,
	frontendFS embed.FS, authToken string,
) {
	r.Route("/api", func(r chi.Router) {
		r.Get("/layout", apiHandler.GetLayout)
		r.Put("/layout", apiHandler.PutLayout)
		r.Get("/workspaces", apiHandler.GetWorkspaces)
		r.Post("/workspaces", apiHandler.PostWorkspace)
		r.Put("/workspaces/active", apiHandler.PutActiveWorkspace)
		r.Put("/workspaces/tab-position", apiHandler.PutWorkspaceTabPosition)
		r.Put("/workspaces/vertical-bar-width", apiHandler.PutWorkspaceVerticalBarWidth)
		r.Put("/workspaces/{id}", apiHandler.PutWorkspace)
		r.Delete("/workspaces/{id}", apiHandler.DeleteWorkspace)
		r.Put("/workspaces/{id}/layout", apiHandler.PutWorkspaceLayout)
		r.Get("/sessions", apiHandler.GetSessions)
		r.Post("/sessions", apiHandler.PostSession)
		r.Delete("/sessions/{id}", apiHandler.DeleteSession)
		r.Post("/sessions/{id}/restart", apiHandler.RestartSession)
		r.Post("/sessions/{id}/open-vscode", apiHandler.PostOpenVSCode)
		r.Post("/sessions/{id}/open-url", apiHandler.PostOpenURL)
		r.Get("/sessions/{id}/git-info", apiHandler.GetGitInfo)
		r.Get("/display", apiHandler.GetDisplay)
		r.Get("/ssh-connections", apiHandler.GetSSHConnections)
		r.Get("/ssh-config/hosts", apiHandler.GetSSHConfigHosts)
		r.Post("/ssh-config/hosts", apiHandler.PostSSHConfigHost)
		r.Get("/detect-shell", apiHandler.GetDetectShell)
		r.Get("/directories", apiHandler.GetDirectories)
		// Deliberately NOT placed under /board/ — chi routes any path
		// starting with /api/board/ into the r.Route("/api/board", ...)
		// sub-router below regardless of where else a handler for that path
		// is registered, so a route literally named /api/board/session-token
		// would always be caught by bearerAuthMiddleware even when defined
		// here. See GetBoardSessionToken's own doc comment for why this one
		// route must stay unauthenticated.
		r.Get("/session-token", apiHandler.GetBoardSessionToken)
	})
	// /api/board/* is the only part of the API gated behind bearer-token
	// auth today: unlike every other /api/* route and /ws/{sessionID},
	// nothing relies on these endpoints yet (no frontend calls them), so
	// gating them from day one costs nothing — retrofitting auth onto the
	// already-relied-upon unauthenticated routes above is a separate,
	// larger change. See docs/security.md.
	r.Route("/api/board", func(r chi.Router) {
		r.Use(bearerAuthMiddleware(authToken))
		r.Get("/status", apiHandler.GetBoardStatus)
		r.Get("/messages", apiHandler.GetBoardMessages)
		r.Post("/broadcast", apiHandler.PostBoardBroadcast)
		r.Get("/command/history", apiHandler.GetBoardCommandHistory)
	})
	r.Get("/ws/{sessionID}", wsHandler.ServeHTTP)
	// /ws/board-command is only registered when the command center is
	// enabled (commandRunner != nil) — see docs/agent-board.md's Command
	// center section. Unlike /ws/{sessionID}, this route requires the
	// bearer token, mirroring /api/board/*.
	if commandRunner != nil {
		boardCommandHandler := ws.NewBoardCommandHandler(commandRunner, authToken)
		r.Get("/ws/board-command", boardCommandHandler.ServeHTTP)
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
// forward it opened on this host.
func (s *Server) Shutdown(ctx context.Context) error {
	s.forwards.Close()
	if err := s.httpSrv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down HTTP server: %w", err)
	}
	return nil
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isLocalhostOrigin(origin) {
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

// isLocalhostOrigin returns true when the given origin URL's host is a loopback address,
// allowing cross-port requests from the Vite dev server while blocking external sites.
func isLocalhostOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host, _, err := net.SplitHostPort(u.Host)
	if err != nil {
		host = u.Host
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
