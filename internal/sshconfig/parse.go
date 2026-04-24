// Package sshconfig parses SSH config files and provides host enumeration.
package sshconfig

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Host represents a single Host block parsed from an SSH config file.
type Host struct {
	Name         string // Host alias (e.g. "prod-web")
	Hostname     string // HostName directive (defaults to Name if not set)
	User         string
	Port         int // 0 = not set (caller uses default 22)
	IdentityFile string
	ProxyJump    string // ProxyJump directive (alias or user@host)
	ProxyCommand string // ProxyCommand directive (shell command acting as stdin/stdout pipe)
}

// DefaultPath returns the default SSH config path (~/.ssh/config).
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".ssh", "config")
	}
	return filepath.Join(home, ".ssh", "config")
}

// ParseHosts parses non-wildcard Host blocks from the given SSH config file.
// If the file does not exist, it returns an empty list and no error.
func ParseHosts(path string) ([]Host, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Host{}, nil
		}
		return nil, fmt.Errorf("open ssh config: %w", err)
	}
	defer f.Close() //nolint:errcheck

	var hosts []Host
	var current *Host

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		key, val, ok := parseDirective(line)
		if !ok {
			continue
		}

		if key == "host" {
			hosts = appendHost(hosts, current)
			current = newHostBlock(val)
			continue
		}

		if current == nil {
			continue
		}
		applyHostDirective(current, key, val)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan ssh config: %w", err)
	}

	return appendHost(hosts, current), nil
}

func parseDirective(line string) (string, string, bool) {
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 2 {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(parts[0])),
		strings.TrimSpace(parts[1]),
		true
}

func newHostBlock(name string) *Host {
	if strings.ContainsAny(name, "*?") {
		return nil
	}
	return &Host{Name: name}
}

func appendHost(hosts []Host, host *Host) []Host {
	if host == nil {
		return hosts
	}
	if host.Hostname == "" {
		host.Hostname = host.Name
	}
	return append(hosts, *host)
}

func applyHostDirective(host *Host, key, val string) {
	switch key {
	case "hostname":
		host.Hostname = val
	case "user":
		host.User = val
	case "port":
		p, err := strconv.Atoi(val)
		if err == nil {
			host.Port = p
		}
	case "identityfile":
		host.IdentityFile = val
	case "proxyjump":
		host.ProxyJump = val
	case "proxycommand":
		host.ProxyCommand = val
	}
}

// AppendHost appends a new Host block to the SSH config file at path.
// The file is created if it does not exist.
func AppendHost(path string, h Host) error {
	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create ssh config dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open ssh config for append: %w", err)
	}
	defer f.Close() //nolint:errcheck

	var sb strings.Builder
	sb.WriteString("\nHost ")
	sb.WriteString(h.Name)
	sb.WriteString("\n")
	sb.WriteString("    HostName ")
	sb.WriteString(h.Hostname)
	sb.WriteString("\n")
	sb.WriteString("    User ")
	sb.WriteString(h.User)
	sb.WriteString("\n")
	if h.Port != 0 {
		fmt.Fprintf(&sb, "    Port %d\n", h.Port)
	}
	if h.IdentityFile != "" {
		sb.WriteString("    IdentityFile ")
		sb.WriteString(h.IdentityFile)
		sb.WriteString("\n")
	}

	_, err = f.WriteString(sb.String())
	if err != nil {
		return fmt.Errorf("write ssh config: %w", err)
	}
	return nil
}
