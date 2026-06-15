package server

import (
	"context"
	"net"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
)

func TestServeWithInjectedListener(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0

	srv := New(cfg, session.NewManager(), fstest.MapFS{
		"frontend/dist/index.html": {Data: []byte("ok")},
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(listener)
	}()

	resp, err := http.Get("http://" + listener.Addr().String() + "/")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
	assert.NoError(t, <-errCh)
}
