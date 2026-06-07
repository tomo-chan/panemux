// Package app owns shared backend bootstrap for browser and desktop modes.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sync"

	"panemux/internal/config"
	"panemux/internal/server"
	"panemux/internal/session"
)

type Mode string

const (
	ModeBrowser Mode = "browser"
	ModeDesktop Mode = "desktop"
)

type Options struct {
	ConfigPath string
	Mode       Mode
	Port       int
}

type Runtime struct {
	Config      *config.Config
	Manager     *session.Manager
	Server      *server.Server
	listener    net.Listener
	errCh       chan error
	shutdownErr error
	BaseURL     string

	startOnce sync.Once
	shutOnce  sync.Once
}

func Bootstrap(opts Options, frontendFS fs.FS) (*Runtime, error) {
	cfg, err := loadConfig(opts)
	if err != nil {
		return nil, err
	}

	manager := session.NewManager()
	if err := startSessionsFromConfig(cfg, manager); err != nil {
		return nil, err
	}

	runtime := &Runtime{
		Config:  cfg,
		Manager: manager,
		Server:  server.New(cfg, manager, frontendFS),
		errCh:   make(chan error, 1),
	}

	if opts.Mode == ModeDesktop {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			manager.CloseAll()
			return nil, fmt.Errorf("listening on loopback: %w", err)
		}
		runtime.listener = listener
		runtime.BaseURL = "http://" + listener.Addr().String()
		return runtime, nil
	}

	runtime.BaseURL = "http://" + runtime.Server.Addr()
	return runtime, nil
}

func (r *Runtime) Start() {
	r.startOnce.Do(func() {
		go func() {
			if r.listener != nil {
				r.errCh <- r.Server.Serve(r.listener)
				return
			}
			r.errCh <- r.Server.Start()
		}()
	})
}

func (r *Runtime) Errors() <-chan error {
	return r.errCh
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	r.shutOnce.Do(func() {
		err := r.Server.Shutdown(ctx)
		r.Manager.CloseAll()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		r.shutdownErr = err
	})
	return r.shutdownErr
}

func loadConfig(opts Options) (*config.Config, error) {
	var (
		cfg *config.Config
		err error
	)
	if opts.ConfigPath != "" {
		cfg, err = config.Load(opts.ConfigPath)
	} else {
		cfg, err = config.LoadOrDefault()
	}
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	if opts.Mode == ModeDesktop {
		cfg.Server.Host = "127.0.0.1"
		cfg.Server.Port = 0
		return cfg, nil
	}

	if opts.Port != 0 {
		cfg.Server.Port = opts.Port
	}
	return cfg, nil
}

func startSessionsFromConfig(cfg *config.Config, manager *session.Manager) error {
	panes := cfg.AllPanes()
	for _, pane := range panes {
		sess, err := session.CreateFromConfig(pane, cfg.SSHConnections)
		if err != nil {
			log.Printf("Warning: failed to start session %s (%s): %v", pane.ID, pane.Type, err)
			continue
		}
		manager.Add(sess)
		log.Printf("Started session: %s (%s)", pane.ID, pane.Type)
	}
	return nil
}
