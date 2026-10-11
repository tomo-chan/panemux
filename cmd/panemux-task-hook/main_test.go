package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestHelperProcess(t *testing.T) {
	data := os.Getenv("PANEMUX_HELPER_TEST_ARGS")
	if data == "" {
		return
	}
	var args []string
	if json.Unmarshal([]byte(data), &args) != nil {
		os.Exit(90)
	}
	os.Args = append([]string{"panemux-task-hook"}, args...)
	main()
	os.Exit(0)
}

func TestHookProcessDeadlineAndEmptyOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"invalid payload", "blocked stdin", "invalid hook arguments", "selfcheck", "unknown mode",
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			args := []string{"record-permission", "--root", t.TempDir()}
			if name == "invalid hook arguments" {
				args = args[:1]
			}
			if name == "selfcheck" {
				args[0] = "selfcheck"
			}
			if name == "unknown mode" {
				args[0] = "private-untrusted-argument"
			}
			data, _ := json.Marshal(args)
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestHelperProcess$")
			cmd.Env = append(os.Environ(), "PANEMUX_HELPER_TEST_ARGS="+string(data))
			cmd.Stdin = strings.NewReader("{private-malformed-body}")
			if name == "blocked stdin" {
				reader, writer, pipeErr := os.Pipe()
				if pipeErr != nil {
					t.Fatal(pipeErr)
				}
				defer func() { _ = reader.Close(); _ = writer.Close() }()
				cmd.Stdin = reader
			}
			var out, diagnostic bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &diagnostic
			started := time.Now()
			runErr := cmd.Run()
			if ctx.Err() != nil || time.Since(started) > 2*time.Second {
				t.Fatal("helper exceeded deadline")
			}
			if name == "selfcheck" {
				if runErr != nil || !strings.Contains(out.String(), `"protocol_version":1`) {
					t.Fatal("selfcheck failed", runErr)
				}
			} else if name == "unknown mode" {
				if runErr == nil || out.Len() != 0 || diagnostic.String() != "task hook helper unavailable\n" {
					t.Fatal("unsafe diagnosis")
				}
			} else if runErr != nil || out.Len() != 0 || diagnostic.Len() != 0 {
				t.Fatal("hook was not silent success", runErr)
			}
		})
	}
}
