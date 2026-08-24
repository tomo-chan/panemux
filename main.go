package main

import (
	"context"
	"embed"
	"errors"
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

// The GOOS values browserOpenArgv branches on. Named so the switch and its
// table-driven test cannot drift apart on a typo.
const (
	goosDarwin  = "darwin"
	goosLinux   = "linux"
	goosWindows = "windows"
)

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

	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		os.Exit(parseExitCode(err))
	}

	if opts.showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	cfg, err := loadConfig(opts, defaultConfigLoader)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Must be set before any session is created: the factory reads it when
	// it builds a pane's environment (see internal/session/browseropen.go).
	session.SetBrowserShimEnabled(cfg.BrowserShimEnabled())

	manager := session.NewManager()
	if err := startSessionsFromConfig(cfg, manager, session.CreateFromConfig); err != nil {
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

// parseOptions parses args (os.Args[1:] in production) into cliOptions.
//
// It builds its own FlagSet rather than using the process-global
// flag.CommandLine: the global one can only be populated once per process, so
// with it this function could neither be called twice nor be given arguments
// a test chose. See DEVELOPMENT.md's testability rule.
func parseOptions(args []string) (cliOptions, error) {
	var opts cliOptions
	fs := flag.NewFlagSet("panemux", flag.ContinueOnError)
	fs.StringVar(&opts.configPath, "config", "", "Path to YAML config file")
	fs.BoolVar(&opts.openBrowser, "open", false, "Open Chrome automatically")
	fs.IntVar(&opts.port, "port", 0, "Override server port")
	fs.BoolVar(&opts.showVersion, "version", false, "Print version and exit")
	if err := fs.Parse(args); err != nil {
		return cliOptions{}, fmt.Errorf("parsing command line options: %w", err)
	}
	return opts, nil
}

// parseExitCode maps a parseOptions failure onto a process exit code.
//
// -h/--help is not a usage error. Under flag.ExitOnError — which
// flag.CommandLine uses, and which parseOptions used before it was given its
// own FlagSet — the flag package exits 0 for help itself. flag.ContinueOnError
// instead returns flag.ErrHelp like any other error, so without this
// distinction `panemux --help` would print its usage and then exit 2. It is a
// documented invocation (install.sh tells the user to run it), so a script
// running it under `set -e` would see a failure for a command that did exactly
// what was asked.
//
// Split out from main so it can be tested: main itself installs signal
// handlers and runs for the life of the process.
func parseExitCode(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		// flag has already printed the usage text, and unlike a real parse
		// error there is no message to add.
		return 0
	}
	// flag has already printed both the error and the usage text; 2 is the
	// conventional exit code for a usage error.
	return 2
}

// configLoader is loadConfig's injection point for the two package-level
// config entry points, so a test can drive the precedence between them (and
// the failure branch) without reading the developer's own
// ~/.config/panemux/config.yaml.
type configLoader struct {
	load          func(path string) (*config.Config, error)
	loadOrDefault func() (*config.Config, error)
}

var defaultConfigLoader = configLoader{
	load:          config.Load,
	loadOrDefault: config.LoadOrDefault,
}

func loadConfig(opts cliOptions, loader configLoader) (*config.Config, error) {
	var (
		cfg *config.Config
		err error
	)
	if opts.configPath != "" {
		cfg, err = loader.load(opts.configPath)
	} else {
		cfg, err = loader.loadOrDefault()
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

// createSessionFunc is the shape of session.CreateFromConfig. Taking it as an
// argument is what lets startSessionsFromConfig be driven without spawning a
// real PTY or dialing a real SSH host — the same injection api.Handler
// already makes for its own createSession field.
type createSessionFunc func(*config.PaneConfig, map[string]config.SSHConnection) (session.Session, error)

// startSessionsFromConfig is deliberately best-effort: a pane that fails to
// start is logged and skipped, and panemux still comes up, so an operator can
// fix a bad connection from the dashboard instead of from a config file they
// cannot see. That is why it returns nil even when every pane failed.
func startSessionsFromConfig(cfg *config.Config, manager *session.Manager, create createSessionFunc) error {
	panes := cfg.AllPanes()
	for _, pane := range panes {
		sess, err := create(pane, cfg.SSHConnections)
		if err != nil {
			log.Printf("Warning: failed to start session %s (%s): %v", pane.ID, pane.Type, err)
			continue
		}
		manager.Add(sess)
		log.Printf("Started session: %s (%s)", pane.ID, pane.Type)
	}
	return nil
}

// browserOpenArgv returns the command that opens url in Chrome on goos, as a
// program name and discrete arguments — never a shell string. Splitting the
// decision out from the exec call is the humble-object shape
// docs/quality-gateway.md's P2 asks for: every per-OS branch is testable
// here, and what is left below is one unconditional Run.
//
// ok is false for an OS panemux has no opener for, in which case nothing is
// launched rather than a guess being executed.
func browserOpenArgv(goos, url string) (name string, args []string, ok bool) {
	switch goos {
	case goosDarwin:
		return "open", []string{"-a", "Google Chrome", url}, true
	case goosLinux:
		return "google-chrome", []string{"--app=" + url}, true
	case goosWindows:
		return "cmd", []string{"/c", "start", "chrome", url}, true
	default:
		return "", nil, false
	}
}

func openChrome(url string) {
	name, args, ok := browserOpenArgv(runtime.GOOS, url)
	if !ok {
		return
	}
	// G204: name is one of three hardcoded literals above and url is
	// panemux's own listen address; both travel as discrete argv elements,
	// never through a shell. See docs/security.md's general rules.
	cmd := exec.Command(name, args...) //nolint:gosec
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to open browser: %v", err)
	}
}
