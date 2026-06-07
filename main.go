package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"panemux/internal/app"
)

var version = "dev"

type cliOptions struct {
	configPath  string
	mode        string
	openBrowser bool
	showVersion bool
	port        int
}

//go:embed frontend/dist
var frontendFS embed.FS

func main() {
	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		log.Fatalf("Failed to parse options: %v", err)
	}

	if opts.showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	if validateErr := validateOptions(opts); validateErr != nil {
		log.Fatalf("Invalid options: %v", validateErr)
	}

	switch opts.mode {
	case string(app.ModeDesktop):
		err = runDesktop(opts)
	default:
		err = runBrowser(opts)
	}
	if err != nil {
		log.Fatalf("Failed to run panemux: %v", err)
	}
}

func parseOptions(args []string) (cliOptions, error) {
	var opts cliOptions

	flags := flag.NewFlagSet("panemux", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	opts.mode = string(app.ModeBrowser)
	flags.StringVar(&opts.configPath, "config", "", "Path to YAML config file")
	flags.StringVar(&opts.mode, "mode", opts.mode, "Run mode: browser or desktop")
	flags.BoolVar(&opts.openBrowser, "open", false, "Open Chrome automatically")
	flags.IntVar(&opts.port, "port", 0, "Override server port")
	flags.BoolVar(&opts.showVersion, "version", false, "Print version and exit")

	if err := flags.Parse(args); err != nil {
		return cliOptions{}, fmt.Errorf("parse flags: %w", err)
	}

	return opts, nil
}

func validateOptions(opts cliOptions) error {
	switch opts.mode {
	case string(app.ModeBrowser), string(app.ModeDesktop):
	default:
		return fmt.Errorf("unsupported mode %q: must be browser or desktop", opts.mode)
	}

	if opts.mode == string(app.ModeDesktop) {
		if opts.openBrowser {
			return errors.New("desktop mode does not support --open")
		}
		if opts.port != 0 {
			return errors.New("desktop mode does not support --port")
		}
	}

	return nil
}

func runBrowser(opts cliOptions) error {
	runtimeApp, err := app.Bootstrap(app.Options{
		ConfigPath: opts.configPath,
		Mode:       app.ModeBrowser,
		Port:       opts.port,
	}, frontendFS)
	if err != nil {
		return fmt.Errorf("bootstrap browser mode: %w", err)
	}

	log.Printf("Listening on %s", runtimeApp.BaseURL)
	runtimeApp.Start()

	if opts.openBrowser {
		go openChrome(runtimeApp.BaseURL)
	}

	return waitForShutdown(runtimeApp)
}

func waitForShutdown(runtimeApp *app.Runtime) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-runtimeApp.Errors():
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	case <-sigCh:
		log.Println("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runtimeApp.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown error: %w", err)
		}
		if err := <-runtimeApp.Errors(); err != nil {
			return fmt.Errorf("server error: %w", err)
		}
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
