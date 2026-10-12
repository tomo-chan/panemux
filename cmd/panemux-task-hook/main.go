package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"panemux/internal/taskhook"
)

func main() {
	args := os.Args[1:]
	hook := len(args) > 0 && args[0] == "record-permission"
	managed := len(args) > 0 && args[0] == "register-managed-launch"
	if len(args) < 3 || args[1] != "--root" || (!managed && len(args) != 3) {
		if hook {
			return
		}
		_, _ = fmt.Fprintln(os.Stderr, "task hook helper unavailable")
		os.Exit(1)
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
			_, _ = fmt.Fprintln(os.Stderr, "task hook helper unavailable")
			os.Exit(1)
		}
		c.Managed = options
	}
	if c.Run(ctx, args[0], os.Stdin, os.Stdout) != nil {
		_, _ = fmt.Fprintln(os.Stderr, "task hook helper unavailable")
		os.Exit(1)
	}
}
