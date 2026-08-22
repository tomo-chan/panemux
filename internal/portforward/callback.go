// Package portforward manages loopback TCP port forwards that make a pane's
// OAuth callback listener reachable at the same port on the panemux host.
//
// CLI login flows (`claude` MCP OAuth, `gh auth login`, and friends) start a
// listener on the loopback interface of the machine the CLI runs on and hand
// the browser an authorization URL whose redirect target is
// http://localhost:<port>/…. For `ssh` and `ssh_tmux` panes that listener
// lives on the remote host, so the browser's callback would resolve to the
// wrong machine. Forwarding the identical port on the panemux host makes the
// callback land where the CLI is waiting for it.
package portforward

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	// minForwardablePort is the lowest port panemux will bind. Privileged
	// ports would need root, and a callback URL that omits its port
	// (implying 80 or 443) is never an OAuth loopback listener in practice.
	minForwardablePort = 1024
	maxPort            = 65535
)

// redirectParamNames are the query parameters an authorization URL uses to
// carry its loopback callback target, normalized by normalizeParamName so
// spelling variants such as `redirectUri` and `redirect-uri` match too.
var redirectParamNames = []string{
	"redirecturi",
	"redirecturl",
	"callbackuri",
	"callbackurl",
}

// ValidateOpenURL reports whether raw is a URL panemux is willing to open in
// the operator's browser. Only http and https are accepted: a pane can ask
// for a URL to be opened (see the OSC browser-open path in
// docs/behavior.md), and schemes such as file: or javascript: would turn
// that into a local-file read or script execution in the dashboard's origin.
func ValidateOpenURL(raw string) error {
	_, err := parseHTTPURL(raw)
	return err
}

func parseHTTPURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return nil, errors.New("only http and https URLs can be opened")
	}
	if u.Host == "" {
		return nil, errors.New("url has no host")
	}
	return u, nil
}

// CallbackPort returns the loopback port a URL expects its OAuth callback on,
// if any. The clicked URL's own host is checked first (a URL that already
// points at loopback is its own callback target), then the redirect
// parameters an authorization URL carries.
func CallbackPort(raw string) (int, bool) {
	u, err := parseHTTPURL(raw)
	if err != nil {
		return 0, false
	}
	if port, ok := loopbackPort(u); ok {
		return port, true
	}

	byName := make(map[string][]string)
	for key, values := range u.Query() {
		name := normalizeParamName(key)
		byName[name] = append(byName[name], values...)
	}
	// Iterate the known names in a fixed order so a URL carrying several
	// redirect parameters always resolves to the same port.
	for _, name := range redirectParamNames {
		for _, value := range byName[name] {
			redirect, err := parseHTTPURL(value)
			if err != nil {
				continue
			}
			if port, ok := loopbackPort(redirect); ok {
				return port, true
			}
		}
	}
	return 0, false
}

// normalizeParamName lowercases a query parameter name and drops the
// separators that distinguish otherwise identical spellings
// (`redirect_uri`, `redirect-uri`, `redirectUri`).
func normalizeParamName(name string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(name))
}

func loopbackPort(u *url.URL) (int, bool) {
	if !isLoopbackHost(u.Hostname()) {
		return 0, false
	}
	rawPort := u.Port()
	if rawPort == "" {
		return 0, false
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || !forwardablePort(port) {
		return 0, false
	}
	return port, true
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func forwardablePort(port int) bool {
	return port >= minForwardablePort && port <= maxPort
}
