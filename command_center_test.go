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
