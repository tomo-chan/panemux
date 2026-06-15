// Package server wires together the chi router, API handlers, and embedded frontend.
package server

import (
	"context"
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
	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/ws"
)

// Server is the HTTP server.
type Server struct {
	cfg     *config.Config
	manager *session.Manager
	httpSrv *http.Server
}

// New creates a new server instance.
func New(cfg *config.Config, manager *session.Manager, frontendFS fs.FS) *Server {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)
	r.Use(securityHeadersMiddleware)

	apiHandler := api.NewHandler(cfg, manager)
	wsHandler := ws.NewHandler(manager)
	registerRoutes(r, apiHandler, wsHandler, frontendFS)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &Server{
		cfg:     cfg,
		manager: manager,
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

func registerRoutes(r chi.Router, apiHandler *api.Handler, wsHandler *ws.Handler, frontendFS fs.FS) {
	r.Route("/api", func(r chi.Router) {
		r.Get("/layout", apiHandler.GetLayout)
		r.Put("/layout", apiHandler.PutLayout)
		r.Get("/workspaces", apiHandler.GetWorkspaces)
		r.Post("/workspaces", apiHandler.PostWorkspace)
		r.Put("/workspaces/active", apiHandler.PutActiveWorkspace)
		r.Put("/workspaces/tab-position", apiHandler.PutWorkspaceTabPosition)
		r.Put("/workspaces/{id}", apiHandler.PutWorkspace)
		r.Delete("/workspaces/{id}", apiHandler.DeleteWorkspace)
		r.Put("/workspaces/{id}/layout", apiHandler.PutWorkspaceLayout)
		r.Get("/sessions", apiHandler.GetSessions)
		r.Post("/sessions", apiHandler.PostSession)
		r.Delete("/sessions/{id}", apiHandler.DeleteSession)
		r.Post("/sessions/{id}/restart", apiHandler.RestartSession)
		r.Post("/sessions/{id}/open-vscode", apiHandler.PostOpenVSCode)
		r.Get("/sessions/{id}/git-info", apiHandler.GetGitInfo)
		r.Get("/display", apiHandler.GetDisplay)
		r.Get("/ssh-connections", apiHandler.GetSSHConnections)
		r.Get("/ssh-config/hosts", apiHandler.GetSSHConfigHosts)
		r.Post("/ssh-config/hosts", apiHandler.PostSSHConfigHost)
		r.Get("/detect-shell", apiHandler.GetDetectShell)
		r.Get("/directories", apiHandler.GetDirectories)
	})
	r.Get("/ws/{sessionID}", wsHandler.ServeHTTP)
	registerFrontend(r, frontendFS)
}

func registerFrontend(r chi.Router, frontendFS fs.FS) {
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
			return nil
		}
		return fmt.Errorf("starting HTTP server: %w", err)
	}
	return nil
}

// Serve begins serving with the provided listener.
func (s *Server) Serve(listener net.Listener) error {
	if err := s.httpSrv.Serve(listener); err != nil {
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serving HTTP server: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
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
