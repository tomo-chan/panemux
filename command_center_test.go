package main

import (
	"testing"

	"panemux/internal/config"
)

func TestSetupCommandCenterDisabledReturnsNil(t *testing.T) {
	cfg := &config.Config{
		Server:        config.ServerConfig{Host: "127.0.0.1", Port: 8080, AuthToken: "tok"},
		CommandCenter: config.CommandCenterConfig{Enabled: false},
	}

	runner := setupCommandCenter(cfg)

	if runner != nil {
		t.Fatal("expected nil runner when command_center.enabled is false")
	}
}

func TestSetupCommandCenterEnabledWithTokenReturnsRunner(t *testing.T) {
	cfg := &config.Config{
		Server:        config.ServerConfig{Host: "127.0.0.1", Port: 8080, AuthToken: "tok"},
		CommandCenter: config.CommandCenterConfig{Enabled: true},
	}

	runner := setupCommandCenter(cfg)

	if runner == nil {
		t.Fatal("expected a non-nil runner when command_center.enabled is true and a token is set")
	}
}

func TestSetupCommandCenterEnabledWithoutTokenReturnsNil(t *testing.T) {
	cfg := &config.Config{
		Server:        config.ServerConfig{Host: "127.0.0.1", Port: 8080, AuthToken: ""},
		CommandCenter: config.CommandCenterConfig{Enabled: true},
	}

	runner := setupCommandCenter(cfg)

	if runner != nil {
		t.Fatal("expected nil runner when no auth token is configured, even if command_center.enabled is true")
	}
}

func TestCommandCenterBaseURL(t *testing.T) {
	cases := []struct {
		name string
		host string
		want string
	}{
		{name: "loopback host used as-is", host: "127.0.0.1", want: "http://127.0.0.1:8080"},
		{name: "empty wildcard becomes loopback", host: "", want: "http://127.0.0.1:8080"},
		{name: "0.0.0.0 wildcard becomes loopback", host: "0.0.0.0", want: "http://127.0.0.1:8080"},
		{name: "IPv6 wildcard becomes IPv6 loopback", host: "::", want: "http://[::1]:8080"},
		{
			// A specific non-wildcard, non-loopback bind is NOT reachable via
			// 127.0.0.1 — binding a specific interface restricts the listening
			// socket to that one address. Must use the configured host itself.
			name: "specific non-loopback host used as-is, not substituted",
			host: "192.168.1.50",
			want: "http://192.168.1.50:8080",
		},
		{name: "IPv6 literal host is bracketed", host: "::1", want: "http://[::1]:8080"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Server: config.ServerConfig{Host: tc.host, Port: 8080}}
			got := commandCenterBaseURL(cfg)
			if got != tc.want {
				t.Fatalf("commandCenterBaseURL(host=%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
	}
}
