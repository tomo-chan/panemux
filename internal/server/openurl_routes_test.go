package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/session"
)

// The open-url route must be reachable on the real server.New() router, not
// only on the flat test router the api package's handler tests build. A
// request carrying an unusable URL is answered by the handler's own
// validation (422); an unregistered route would 404 instead.
func TestServer_OpenURLRouteIsWired(t *testing.T) {
	srv := New(testConfig(), session.NewManager(), nil, nil, nil, emptyFS)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost, "/api/sessions/pane-1/open-url", strings.NewReader(`{"url":"file:///etc/passwd"}`),
	)
	srv.httpSrv.Handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
}

// The route follows the same unauthenticated posture as the rest of
// /api/*: it is not behind the board bearer-token middleware.
func TestServer_OpenURLRouteIsNotBehindBoardAuth(t *testing.T) {
	srv := New(testConfigWithToken("secret-token"), session.NewManager(), nil, nil, nil, emptyFS)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost, "/api/sessions/missing/open-url", strings.NewReader(`{"url":"https://example.com/"}`),
	)
	srv.httpSrv.Handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "session not found")
}

func TestServer_ShutdownClosesPortForwards(t *testing.T) {
	srv := New(testConfig(), session.NewManager(), nil, nil, nil, emptyFS)
	require.NotNil(t, srv.forwards)

	require.NoError(t, srv.Shutdown(context.Background()))

	// The registry is closed: it refuses to open new forwards.
	_, err := srv.forwards.Ensure("pane-1", 45000, stubDialer{})
	assert.Error(t, err)
}

type stubDialer struct{}

func (stubDialer) DialLoopback(_ context.Context, _ int) (net.Conn, error) {
	return nil, errors.New("not used")
}
