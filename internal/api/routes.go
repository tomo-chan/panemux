package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// BoardRoutePrefix is the path prefix for the only part of the HTTP API that
// is gated behind the board bearer token. It is exported so the server that
// mounts these routes can name the same prefix its auth middleware guards
// instead of repeating the literal.
const BoardRoutePrefix = "/api/board"

// Mount registers the whole /api surface on r.
//
// This function is the single definition of the route table. Production
// wiring in internal/server and this package's own handler tests both go
// through it, so a route cannot be renamed, moved, or added in one place and
// silently missed in the other. That used to be possible: the handler tests
// built their own chi router listing the routes by hand, and the two had
// already drifted — /api/board/* was registered there flat and with no
// middleware, a shape that never existed in production. See issue #178.
//
// boardAuth wraps the BoardRoutePrefix sub-router. Production passes the
// bearer-token middleware. A nil value mounts those routes with no
// middleware, which is what handler-level tests want: whether the token is
// actually enforced is internal/server's contract and is tested there,
// against the router this repository really serves.
func (h *Handler) Mount(r chi.Router, boardAuth func(http.Handler) http.Handler) {
	r.Route("/api", func(r chi.Router) {
		r.Get("/layout", h.GetLayout)
		r.Put("/layout", h.PutLayout)
		r.Get("/workspaces", h.GetWorkspaces)
		r.Post("/workspaces", h.PostWorkspace)
		r.Put("/workspaces/active", h.PutActiveWorkspace)
		r.Put("/workspaces/tab-position", h.PutWorkspaceTabPosition)
		r.Put("/workspaces/vertical-bar-width", h.PutWorkspaceVerticalBarWidth)
		r.Put("/workspaces/{id}", h.PutWorkspace)
		r.Delete("/workspaces/{id}", h.DeleteWorkspace)
		r.Put("/workspaces/{id}/layout", h.PutWorkspaceLayout)
		r.Get("/sessions", h.GetSessions)
		r.Post("/sessions", h.PostSession)
		r.Delete("/sessions/{id}", h.DeleteSession)
		r.Post("/sessions/{id}/restart", h.RestartSession)
		r.Post("/sessions/{id}/open-vscode", h.PostOpenVSCode)
		r.Post("/sessions/{id}/open-url", h.PostOpenURL)
		r.Get("/sessions/{id}/git-info", h.GetGitInfo)
		r.Get("/display", h.GetDisplay)
		r.Get("/ssh-connections", h.GetSSHConnections)
		r.Get("/ssh-config/hosts", h.GetSSHConfigHosts)
		r.Post("/ssh-config/hosts", h.PostSSHConfigHost)
		r.Get("/detect-shell", h.GetDetectShell)
		r.Get("/directories", h.GetDirectories)
		// Deliberately NOT placed under /board/ — chi routes any path
		// starting with /api/board/ into the BoardRoutePrefix sub-router
		// below regardless of where else a handler for that path is
		// registered, so a route literally named /api/board/session-token
		// would always be caught by boardAuth even when defined here. See
		// GetBoardSessionToken's own doc comment for why this one route
		// must stay unauthenticated.
		r.Get("/session-token", h.GetBoardSessionToken)
	})
	r.Route(BoardRoutePrefix, func(r chi.Router) {
		if boardAuth != nil {
			r.Use(boardAuth)
		}
		r.Get("/status", h.GetBoardStatus)
		r.Get("/messages", h.GetBoardMessages)
		r.Post("/broadcast", h.PostBoardBroadcast)
		r.Get("/command/history", h.GetBoardCommandHistory)
	})
}
