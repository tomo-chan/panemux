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
	defer f.Close()

	var hosts []Host
	var current *Host

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		if key == "host" {
			// Save previous host if non-wildcard
			if current != nil {
				if current.Hostname == "" {
					current.Hostname = current.Name
				}
				hosts = append(hosts, *current)
			}
			// Start new host block, skip wildcards
			if strings.ContainsAny(val, "*?") {
				current = nil
			} else {
				current = &Host{Name: val}
			}
			continue
		}

		if current == nil {
			continue
		}

		switch key {
		case "hostname":
			current.Hostname = val
		case "user":
			current.User = val
		case "port":
			p, err := strconv.Atoi(val)
			if err == nil {
				current.Port = p
			}
		case "identityfile":
			current.IdentityFile = val
		case "proxyjump":
			current.ProxyJump = val
		case "proxycommand":
			current.ProxyCommand = val
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan ssh config: %w", err)
	}

	// Save last block
	if current != nil {
		if current.Hostname == "" {
			current.Hostname = current.Name
		}
		hosts = append(hosts, *current)
	}

	return hosts, nil
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
	defer f.Close()

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
		sb.WriteString(fmt.Sprintf("    Port %d\n", h.Port))
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
