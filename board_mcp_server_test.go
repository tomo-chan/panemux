package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func fakeGetenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestRunBoardMCPServerMissingBaseURLReturnsError(t *testing.T) {
	err := runBoardMCPServer(context.Background(), fakeGetenv(map[string]string{
		envBoardMCPToken: "tok",
	}), strings.NewReader(""), &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected an error when PANEMUX_BOARD_BASE_URL is unset")
	}
}

func TestRunBoardMCPServerMissingTokenReturnsError(t *testing.T) {
	err := runBoardMCPServer(context.Background(), fakeGetenv(map[string]string{
		envBoardMCPBaseURL: "http://127.0.0.1:8080",
	}), strings.NewReader(""), &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected an error when PANEMUX_BOARD_TOKEN is unset")
	}
}

func TestRunBoardMCPServerServesInitializeRequest(t *testing.T) {
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	var stdout bytes.Buffer

	err := runBoardMCPServer(context.Background(), fakeGetenv(map[string]string{
		envBoardMCPBaseURL: "http://127.0.0.1:1",
		envBoardMCPToken:   "tok",
	}), stdin, &stdout)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), `"protocolVersion"`) {
		t.Fatalf("expected an initialize response in stdout, got %q", stdout.String())
	}
}
