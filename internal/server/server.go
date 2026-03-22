package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
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
func New(cfg *config.Config, manager *session.Manager, frontendFS embed.FS) *Server {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	apiHandler := api.NewHandler(cfg, manager)
	wsHandler := ws.NewHandler(manager)

	// REST API
	r.Route("/api", func(r chi.Router) {
		r.Get("/layout", apiHandler.GetLayout)
		r.Put("/layout", apiHandler.PutLayout)
		r.Get("/sessions", apiHandler.GetSessions)
		r.Post("/sessions", apiHandler.PostSession)
		r.Delete("/sessions/{id}", apiHandler.DeleteSession)
		r.Post("/sessions/{id}/restart", apiHandler.RestartSession)
		r.Get("/display", apiHandler.GetDisplay)
		r.Get("/edit-mode", apiHandler.GetEditMode)
		r.Put("/edit-mode", apiHandler.PutEditMode)
		r.Get("/ssh-connections", apiHandler.GetSSHConnections)
		r.Get("/ssh-config/hosts", apiHandler.GetSSHConfigHosts)
		r.Post("/ssh-config/hosts", apiHandler.PostSSHConfigHost)
	})

	// WebSocket
	r.Get("/ws/{sessionID}", wsHandler.ServeHTTP)

	// Static frontend files
	distFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		// Fall back to serving from filesystem if embed fails
		r.Get("/*", http.FileServer(http.Dir("frontend/dist")).ServeHTTP)
	} else {
		fileServer := http.FileServer(http.FS(distFS))
		r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
			// SPA fallback: serve index.html for non-asset routes
			if _, err := distFS.Open(req.URL.Path[1:]); err != nil {
				req.URL.Path = "/"
			}
			fileServer.ServeHTTP(w, req)
		})
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &Server{
		cfg:     cfg,
		manager: manager,
		httpSrv: &http.Server{
			Addr:         addr,
			Handler:      r,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 0, // no timeout for WebSocket connections
		},
	}
}

// Addr returns the server address.
func (s *Server) Addr() string {
	return s.httpSrv.Addr
}

// Start begins listening and serving.
func (s *Server) Start() error {
	return s.httpSrv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
