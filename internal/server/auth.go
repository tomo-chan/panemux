package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

// bearerAuthMiddleware rejects any request whose Authorization header does
// not carry exactly "Bearer <token>" for the given token, using a
// constant-time comparison so response timing cannot be used to guess the
// token byte-by-byte. It runs before next.ServeHTTP is ever called, so
// wrapping a WebSocket upgrade handler with it rejects the handshake before
// any upgrade attempt — no separate WS-specific logic is needed.
//
// This middleware is not yet wired into registerRoutes: doing so today
// would require every existing, currently-unauthenticated frontend request
// to start sending a token it doesn't have. It is built and tested standalone
// so a later change can connect it once there is an auth-aware frontend and
// board endpoints to protect — see docs/agent-board.md's API additions
// section.
func bearerAuthMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !validBearerToken(r.Header.Get("Authorization"), token) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// validBearerToken reports whether header is exactly "Bearer <token>" for a
// non-empty token, so a config with no token configured always rejects
// (fail closed) rather than accepting any request.
func validBearerToken(header, token string) bool {
	if token == "" || !strings.HasPrefix(header, bearerPrefix) {
		return false
	}
	provided := strings.TrimPrefix(header, bearerPrefix)
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}
