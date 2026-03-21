package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
}

type SSHConnection struct {
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	User           string `yaml:"user"`
	KeyFile        string `yaml:"key_file"`
	Password       string `yaml:"password,omitempty"`
	KnownHostsFile string `yaml:"known_hosts_file,omitempty" json:"known_hosts_file,omitempty"`
}

type DisplayConfig struct {
	ShowHeader    bool `yaml:"show_header"    json:"show_header"`
	ShowStatusBar bool `yaml:"show_status_bar" json:"show_status_bar"`
}

type PaneConfig struct {
	ID            string `yaml:"id"           json:"id"`
	Type          string `yaml:"type"         json:"type"` // local | ssh | tmux | ssh_tmux
	Shell         string `yaml:"shell,omitempty"        json:"shell,omitempty"`
	Cwd           string `yaml:"cwd,omitempty"          json:"cwd,omitempty"`
	Title         string `yaml:"title,omitempty"        json:"title,omitempty"`
	Connection    string `yaml:"connection,omitempty"   json:"connection,omitempty"` // ssh_connections key
	TmuxSession   string `yaml:"tmux_session,omitempty" json:"tmux_session,omitempty"`
	ShowHeader    *bool  `yaml:"show_header,omitempty"    json:"show_header,omitempty"`
	ShowStatusBar *bool  `yaml:"show_status_bar,omitempty" json:"show_status_bar,omitempty"`
}

type LayoutNode struct {
	Direction string        `yaml:"direction,omitempty" json:"direction,omitempty"` // horizontal | vertical
	Children  []LayoutChild `yaml:"children,omitempty"  json:"children,omitempty"`
	Pane      *PaneConfig   `yaml:"pane,omitempty"      json:"pane,omitempty"`
}

type LayoutChild struct {
	Size      float64       `yaml:"size"               json:"size"`
	Pane      *PaneConfig   `yaml:"pane,omitempty"     json:"pane,omitempty"`
	Direction string        `yaml:"direction,omitempty" json:"direction,omitempty"`
	Children  []LayoutChild `yaml:"children,omitempty"  json:"children,omitempty"`
}

type Config struct {
	Server         ServerConfig             `yaml:"server"`
	SSHConnections map[string]SSHConnection `yaml:"ssh_connections,omitempty"`
	Layout         LayoutNode               `yaml:"layout"`
	Display        DisplayConfig            `yaml:"display,omitempty" json:"display"`

	// internal: raw yaml node for comment-preserving writes
	rawNode  *yaml.Node
	filePath string
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var rawNode yaml.Node
	if err := yaml.Unmarshal(data, &rawNode); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	var cfg Config
	if err := rawNode.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decoding config: %w", err)
	}

	cfg.rawNode = &rawNode
	cfg.filePath = path
	cfg.expandPaths()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Port: 8080,
			Host: "127.0.0.1",
		},
		Display: DisplayConfig{
			ShowHeader:    true,
			ShowStatusBar: false,
		},
		Layout: LayoutNode{
			Direction: "horizontal",
			Children: []LayoutChild{
				{
					Size: 100.0,
					Pane: &PaneConfig{
						ID:    "local-main",
						Type:  "local",
						Shell: os.Getenv("SHELL"),
						Title: "Terminal",
					},
				},
			},
		},
	}
}

func (c *Config) expandPaths() {
	for key, conn := range c.SSHConnections {
		home, _ := os.UserHomeDir()
		if strings.HasPrefix(conn.KeyFile, "~/") {
			conn.KeyFile = filepath.Join(home, conn.KeyFile[2:])
		}
		if strings.HasPrefix(conn.KnownHostsFile, "~/") {
			conn.KnownHostsFile = filepath.Join(home, conn.KnownHostsFile[2:])
		}
		c.SSHConnections[key] = conn
	}
	expandPanesCwd(c.Layout.Children)
}

func expandPanesCwd(children []LayoutChild) {
	for i := range children {
		if children[i].Pane != nil && strings.HasPrefix(children[i].Pane.Cwd, "~/") {
			home, _ := os.UserHomeDir()
			children[i].Pane.Cwd = filepath.Join(home, children[i].Pane.Cwd[2:])
		}
		expandPanesCwd(children[i].Children)
	}
}

// SaveLayout updates the layout section and writes back to file, preserving comments.
func (c *Config) SaveLayout(layout LayoutNode) error {
	c.Layout = layout
	if c.filePath == "" {
		return nil // no file to save to
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(c.filePath, data, 0644)
}

// AllPanes returns a flat list of all pane configs.
func (c *Config) AllPanes() []*PaneConfig {
	var panes []*PaneConfig
	collectPanes(c.Layout.Children, &panes)
	return panes
}

func collectPanes(children []LayoutChild, panes *[]*PaneConfig) {
	for i := range children {
		if children[i].Pane != nil {
			*panes = append(*panes, children[i].Pane)
		}
		collectPanes(children[i].Children, panes)
	}
}

// RemovePaneFromLayout removes the pane with the given ID from the layout tree
// and normalizes sibling sizes so they still sum to 100.
func (c *Config) RemovePaneFromLayout(paneID string) {
	c.Layout.Children = removePaneChildren(c.Layout.Children, paneID)
}

func removePaneChildren(children []LayoutChild, paneID string) []LayoutChild {
	var result []LayoutChild
	for _, child := range children {
		if child.Pane != nil && child.Pane.ID == paneID {
			continue
		}
		if len(child.Children) > 0 {
			sub := removePaneChildren(child.Children, paneID)
			if len(sub) == 0 {
				continue
			}
			if len(sub) == 1 {
				// Collapse single remaining child upward, preserving parent size.
				result = append(result, LayoutChild{
					Size:      child.Size,
					Pane:      sub[0].Pane,
					Direction: sub[0].Direction,
					Children:  sub[0].Children,
				})
			} else {
				result = append(result, LayoutChild{
					Size:      child.Size,
					Direction: child.Direction,
					Children:  sub,
				})
			}
		} else {
			result = append(result, child)
		}
	}
	// Normalize sizes to sum to 100.
	if len(result) > 0 {
		var total float64
		for _, c := range result {
			total += c.Size
		}
		if total > 0 {
			for i := range result {
				result[i].Size = (result[i].Size / total) * 100
			}
		}
	}
	return result
}
