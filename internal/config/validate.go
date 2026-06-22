package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"panemux/internal/sshconfig"
)

var tmuxSessionNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

const (
	directionHorizontal          = "horizontal"
	directionVertical            = "vertical"
	paneTypeLocal                = "local"
	paneTypeSSH                  = "ssh"
	paneTypeTmux                 = "tmux"
	paneTypeSSHTmux              = "ssh_tmux"
	tabPositionTop               = "top"
	tabPositionBottom            = "bottom"
	tabPositionLeft              = "left"
	tabPositionRight             = "right"
	minWorkspaceVerticalBarWidth = 180
	maxWorkspaceVerticalBarWidth = 520
)

// Validate checks the configuration for correctness.
// It collects all errors and returns them as a single combined error.
func (c *Config) Validate() error {
	var errs []string

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		errs = append(errs, fmt.Sprintf("server.port %d is out of range (1-65535)", c.Server.Port))
	}

	sshConns := c.SSHConnections
	if sshConns == nil {
		sshConns = make(map[string]SSHConnection)
	}
	// Also accept hosts from ~/.ssh/config as valid connections.
	// This allows panes to reference ssh config host aliases without
	// duplicating connection details in ssh_connections.
	sshCfgPath := c.sshConfigPath
	if sshCfgPath == "" {
		sshCfgPath = sshconfig.DefaultPath()
	}
	if hosts, err := sshconfig.ParseHosts(sshCfgPath); err == nil {
		for _, h := range hosts {
			if _, exists := sshConns[h.Name]; !exists {
				sshConns[h.Name] = SSHConnection{Host: h.Hostname}
			}
		}
	}
	errs = append(errs, validateWorkspaces(c.normalizedWorkspaces(), sshConns)...)

	seen := make(map[string]bool)
	for _, pane := range c.AllPanes() {
		if seen[pane.ID] {
			errs = append(errs, fmt.Sprintf("duplicate pane id: %q", pane.ID))
		}
		seen[pane.ID] = true
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func validateWorkspaces(workspaces WorkspacesConfig, sshConns map[string]SSHConnection) []string {
	var errs []string
	if workspaces.TabPosition != "" {
		if err := ValidateWorkspaceTabPosition(workspaces.TabPosition); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if workspaces.VerticalBarWidth != 0 {
		if err := ValidateWorkspaceVerticalBarWidth(workspaces.VerticalBarWidth); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(workspaces.Items) == 0 {
		errs = append(errs, "workspaces.items must not be empty")
		return errs
	}

	seenIDs := make(map[string]bool)
	activeFound := workspaces.Active == ""
	for i, workspace := range workspaces.Items {
		if workspace.ID == "" {
			errs = append(errs, fmt.Sprintf("workspace[%d] id must not be empty", i))
		}
		if strings.TrimSpace(workspace.Title) == "" {
			errs = append(errs, fmt.Sprintf("workspace[%d] title must not be empty", i))
		}
		if seenIDs[workspace.ID] {
			errs = append(errs, fmt.Sprintf("duplicate workspace id: %q", workspace.ID))
		}
		seenIDs[workspace.ID] = true
		if workspace.ID == workspaces.Active {
			activeFound = true
		}
		errs = append(errs, validateLayoutNode(workspace.Layout, sshConns)...)
	}
	if !activeFound {
		errs = append(errs, fmt.Sprintf("active workspace %q is not defined", workspaces.Active))
	}
	return errs
}

// ValidateWorkspaceTabPosition validates the workspace tab bar placement.
func ValidateWorkspaceTabPosition(position string) error {
	switch position {
	case tabPositionTop, tabPositionBottom, tabPositionLeft, tabPositionRight:
		return nil
	default:
		return fmt.Errorf("invalid tab_position %q: must be top, bottom, left, or right", position)
	}
}

func ValidateWorkspaceVerticalBarWidth(width int) error {
	if width < minWorkspaceVerticalBarWidth || width > maxWorkspaceVerticalBarWidth {
		return fmt.Errorf(
			"invalid vertical_bar_width %d: must be between %d and %d",
			width,
			minWorkspaceVerticalBarWidth,
			maxWorkspaceVerticalBarWidth,
		)
	}
	return nil
}

// ValidatePane validates a standalone PaneConfig without ssh_connections context.
func ValidatePane(p *PaneConfig) error {
	errs := validatePane(p, nil)
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// ValidateLayout validates a standalone LayoutNode without ssh_connections context.
func ValidateLayout(node LayoutNode) error {
	errs := validateLayoutNode(node, nil)
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func validateLayoutNode(node LayoutNode, sshConns map[string]SSHConnection) []string {
	var errs []string
	if node.Direction != "" && node.Direction != directionHorizontal && node.Direction != directionVertical {
		errs = append(errs, fmt.Sprintf("invalid direction %q: must be horizontal or vertical", node.Direction))
	}
	errs = append(errs, validateChildren(node.Children, sshConns)...)
	return errs
}

func validateChildren(children []LayoutChild, sshConns map[string]SSHConnection) []string {
	var errs []string
	if len(children) == 0 {
		return errs
	}

	var total float64
	for i, child := range children {
		if child.Size <= 0 {
			errs = append(errs, fmt.Sprintf("child[%d] size %.2f must be positive", i, child.Size))
		}
		total += child.Size
	}
	if total < 99.9 || total > 100.1 {
		errs = append(errs, fmt.Sprintf("child sizes sum to %.2f, must be 100 (±0.1)", total))
	}

	for i, child := range children {
		if child.Direction != "" && child.Direction != directionHorizontal && child.Direction != directionVertical {
			errs = append(
				errs,
				fmt.Sprintf(
					"child[%d] invalid direction %q: must be horizontal or vertical",
					i,
					child.Direction,
				),
			)
		}
		if child.Pane != nil {
			errs = append(errs, validatePane(child.Pane, sshConns)...)
		}
		errs = append(errs, validateChildren(child.Children, sshConns)...)
	}
	return errs
}

func validatePane(p *PaneConfig, sshConns map[string]SSHConnection) []string {
	var errs []string

	if p.ID == "" {
		errs = append(errs, "pane id must not be empty")
	}

	switch p.Type {
	case paneTypeLocal, paneTypeSSH, paneTypeTmux, paneTypeSSHTmux:
		// valid
	default:
		errs = append(
			errs,
			fmt.Sprintf(
				"pane %q has invalid type %q: must be local, ssh, tmux, or ssh_tmux",
				p.ID,
				p.Type,
			),
		)
	}

	if p.Type == paneTypeSSH || p.Type == paneTypeSSHTmux {
		if p.Connection == "" {
			errs = append(errs, fmt.Sprintf("pane %q: ssh connection name must not be empty", p.ID))
		} else if sshConns != nil {
			if _, ok := sshConns[p.Connection]; !ok {
				errs = append(errs, fmt.Sprintf("pane %q: connection %q not defined in ssh_connections", p.ID, p.Connection))
			}
		}
	}

	if p.Shell != "" && !strings.HasPrefix(p.Shell, "/") {
		errs = append(errs, fmt.Sprintf("pane %q: shell must be an absolute path, got %q", p.ID, p.Shell))
	}

	if p.Type == paneTypeTmux || p.Type == paneTypeSSHTmux {
		if p.TmuxSession == "" {
			errs = append(errs, fmt.Sprintf("pane %q: tmux_session must not be empty", p.ID))
		} else if !tmuxSessionNameRe.MatchString(p.TmuxSession) {
			errs = append(
				errs,
				fmt.Sprintf(
					"pane %q: tmux_session %q contains invalid characters "+
						"(allowed: a-z A-Z 0-9 - _ .)",
					p.ID,
					p.TmuxSession,
				),
			)
		}
	}

	return errs
}
