package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionDefault(t *testing.T) {
	if version != "dev" {
		t.Errorf("expected default version %q, got %q", "dev", version)
	}
}

func TestParseOptionsDefaultsToBrowserMode(t *testing.T) {
	t.Parallel()

	opts, err := parseOptions(nil)
	require.NoError(t, err)
	assert.Equal(t, "browser", opts.mode)
}

func TestValidateOptionsRejectsDesktopOpen(t *testing.T) {
	t.Parallel()

	err := validateOptions(cliOptions{mode: "desktop", openBrowser: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--open")
}

func TestValidateOptionsRejectsDesktopPort(t *testing.T) {
	t.Parallel()

	err := validateOptions(cliOptions{mode: "desktop", port: 9090})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--port")
}

func TestValidateOptionsAcceptsBrowserMode(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateOptions(cliOptions{mode: "browser"}))
}

func TestValidateOptionsRejectsUnknownMode(t *testing.T) {
	t.Parallel()

	err := validateOptions(cliOptions{mode: "foobar"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported mode")
}
