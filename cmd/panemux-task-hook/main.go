package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"panemux/internal/taskhook"
)

const (
	hookMode    = "record-permission"
	managedMode = "register-managed-launch"
	rootFlag    = "--root"
)

var exitProcess = os.Exit

func main() { exitProcess(cli(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func cli(args []string, input io.Reader, output, diagnostic io.Writer) int {
	hook := len(args) > 0 && args[0] == hookMode
	managed := len(args) > 0 && args[0] == managedMode
	if len(args) < 3 || args[1] != rootFlag || (!managed && len(args) != 3) {
		if hook {
			return 0
		}
		_, _ = fmt.Fprintln(diagnostic, "task hook helper unavailable")
		return 1
	}
	root := args[2]
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	c := taskhook.Command{PID: os.Getpid(), Generation: os.Getenv("PANEMUX_TASK_OWNER_GENERATION"),
		Verifier: &taskhook.Verifier{Store: taskhook.NewStore(root), Registry: taskhook.NewRegistry(root),
			Host: taskhook.NewNativeHost(os.Geteuid())}}
	if managed {
		options, err := taskhook.ParseManagedArgs(args[3:])
		if err != nil {
			_, _ = fmt.Fprintln(diagnostic, "task hook helper unavailable")
			return 1
		}
		c.Managed = options
	}
	if c.Run(ctx, args[0], input, output) != nil {
		_, _ = fmt.Fprintln(diagnostic, "task hook helper unavailable")
		return 1
	}
	return 0
}
