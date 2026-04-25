package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"panemux/internal/config"
	"panemux/internal/server"
	"panemux/internal/session"
)

var version = "dev"

type cliOptions struct {
	configPath  string
	openBrowser bool
	showVersion bool
	port        int
}

//go:embed frontend/dist
var frontendFS embed.FS

func main() {
	opts := parseOptions()

	if opts.showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	cfg, err := loadConfig(opts)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	manager := session.NewManager()
	if err := startSessionsFromConfig(cfg, manager); err != nil {
		log.Fatalf("Failed to start sessions: %v", err)
	}

	srv := server.New(cfg, manager, frontendFS)
	addr := "http://" + srv.Addr()
	log.Printf("Listening on %s", addr)

	if opts.openBrowser {
		go openChrome(addr)
	}

	runServer(srv, manager)
}

func parseOptions() cliOptions {
	var opts cliOptions
	flag.StringVar(&opts.configPath, "config", "", "Path to YAML config file")
	flag.BoolVar(&opts.openBrowser, "open", false, "Open Chrome automatically")
	flag.IntVar(&opts.port, "port", 0, "Override server port")
	flag.BoolVar(&opts.showVersion, "version", false, "Print version and exit")
	flag.Parse()
	return opts
}

func loadConfig(opts cliOptions) (*config.Config, error) {
	var (
		cfg *config.Config
		err error
	)
	if opts.configPath != "" {
		cfg, err = config.Load(opts.configPath)
	} else {
		cfg, err = config.LoadOrDefault()
	}
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if opts.port != 0 {
		cfg.Server.Port = opts.port
	}
	return cfg, nil
}

func runServer(srv *server.Server, manager *session.Manager) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case <-sigCh:
		log.Println("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
		manager.CloseAll()
	}
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

func openChrome(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-a", "Google Chrome", url)
	case "linux":
		cmd = exec.Command("google-chrome", "--app="+url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "chrome", url)
	default:
		return
	}
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to open browser: %v", err)
	}
}
