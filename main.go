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

	"panemux/internal/config"
	"panemux/internal/server"
	"panemux/internal/session"
)

var version = "dev"

//go:embed frontend/dist
var frontendFS embed.FS

func main() {
	var (
		configPath  = flag.String("config", "", "Path to YAML config file")
		openBrowser = flag.Bool("open", false, "Open Chrome automatically")
		port        = flag.Int("port", 0, "Override server port")
		showVersion = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	// Load config
	var cfg *config.Config
	if *configPath != "" {
		var err error
		cfg, err = config.Load(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	} else {
		var err error
		cfg, err = config.LoadOrDefault()
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	}

	if *port != 0 {
		cfg.Server.Port = *port
	}

	// Create session manager and start all sessions defined in config
	manager := session.NewManager()
	if err := startSessionsFromConfig(cfg, manager); err != nil {
		log.Fatalf("Failed to start sessions: %v", err)
	}

	// Start HTTP server
	srv := server.New(cfg, manager, frontendFS)
	addr := fmt.Sprintf("http://%s", srv.Addr())
	log.Printf("Listening on %s", addr)

	if *openBrowser {
		go openChrome(addr)
	}

	// Wait for interrupt signal
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
		ctx, cancel := context.WithTimeout(context.Background(), 5000000000) // 5s
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
