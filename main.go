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

	"panemux/internal/board"
	"panemux/internal/commandcenter"
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
	// __board-mcp-server is a hidden subcommand, not an ordinary flag: the
	// command center's own claude -p subprocess re-invokes this same binary
	// as its MCP server (see docs/agent-board.md's Command center section),
	// so it must be recognized before flag.Parse ever runs.
	if len(os.Args) > 1 && os.Args[1] == commandcenter.BoardMCPServerSubcommand {
		if err := runBoardMCPServer(context.Background(), os.Getenv, os.Stdin, os.Stdout); err != nil {
			log.Fatalf("board mcp server: %v", err)
		}
		return
	}

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

	boardCache, boardRelay, boardBootstrap := setupBoard(cfg, manager)
	commandRunner := setupCommandCenter(cfg)

	srv := server.New(cfg, manager, boardCache, boardRelay, commandRunner, frontendFS)
	addr := "http://" + srv.Addr()
	log.Printf("Listening on %s", addr)

	if opts.openBrowser {
		go openChrome(addr)
	}

	runServer(srv, manager, boardRelay, boardBootstrap)
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
	cfg.EnsureAuthToken()
	return cfg, nil
}

func runServer(
	srv *server.Server, manager *session.Manager, boardRelay *board.Relay, boardBootstrap *bootstrapWatcher,
) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	boardCtx, cancelBoard := context.WithCancel(context.Background())
	defer cancelBoard()
	// Skip the poll loop entirely when no host is reachable at all: with
	// zero AgmsgClients every poll is a guaranteed no-op forever (nothing
	// to read, nothing to relay), so starting it would just tick a ticker
	// for the life of the process for no reason — including for the many
	// panemux users who have never configured agent board at all.
	if boardRelay.HasClients() {
		go boardRelay.Run(boardCtx, defaultBoardPollInterval)
	}
	// Same reasoning as boardRelay.HasClients() above: no board-enabled pane
	// at all means every tick would be a guaranteed no-op.
	if boardBootstrap.HasWork() {
		go boardBootstrap.Run(boardCtx, defaultBootstrapPollInterval)
	}

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
		cancelBoard()
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
		cmd = exec.Command("google-chrome", "--app="+url) //nolint:gosec // G204: URL is local server host/port
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "chrome", url)
	default:
		return
	}
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to open browser: %v", err)
	}
}
