// Package config loads and manages panemux YAML configuration.
package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const configFileMode os.FileMode = 0600

var chmodConfigFile = os.Chmod

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type SSHConnection struct {
	Host           string `yaml:"host"`
	User           string `yaml:"user"`
	KeyFile        string `yaml:"key_file"`
	Password       string `yaml:"password,omitempty"`
	KnownHostsFile string `yaml:"known_hosts_file,omitempty" json:"known_hosts_file,omitempty"`
	Port           int    `yaml:"port"`
}

type DisplayConfig struct {
	ShowHeader    bool `yaml:"show_header"    json:"show_header"`
	ShowStatusBar bool `yaml:"show_status_bar" json:"show_status_bar"`
}

type PaneConfig struct {
	ShowHeader    *bool  `yaml:"show_header,omitempty"    json:"show_header,omitempty"`
	ShowStatusBar *bool  `yaml:"show_status_bar,omitempty" json:"show_status_bar,omitempty"`
	ID            string `yaml:"id"           json:"id"`
	Type          string `yaml:"type"         json:"type"` // local | ssh | tmux | ssh_tmux
	Shell         string `yaml:"shell,omitempty"        json:"shell,omitempty"`
	Cwd           string `yaml:"cwd,omitempty"          json:"cwd,omitempty"`
	Title         string `yaml:"title,omitempty"        json:"title,omitempty"`
	Connection    string `yaml:"connection,omitempty"   json:"connection,omitempty"` // ssh_connections key
	TmuxSession   string `yaml:"tmux_session,omitempty" json:"tmux_session,omitempty"`
}

type LayoutNode struct {
	Pane      *PaneConfig   `yaml:"pane,omitempty"      json:"pane,omitempty"`
	Direction string        `yaml:"direction,omitempty" json:"direction,omitempty"` // horizontal | vertical
	Children  []LayoutChild `yaml:"children,omitempty"  json:"children,omitempty"`
}

type LayoutChild struct {
	Pane      *PaneConfig   `yaml:"pane,omitempty"     json:"pane,omitempty"`
	Direction string        `yaml:"direction,omitempty" json:"direction,omitempty"`
	Children  []LayoutChild `yaml:"children,omitempty"  json:"children,omitempty"`
	Size      float64       `yaml:"size"               json:"size"`
}

type WorkspaceConfig struct {
	ID     string     `yaml:"id"     json:"id"`
	Title  string     `yaml:"title"  json:"title"`
	Layout LayoutNode `yaml:"layout" json:"layout"`
}

type WorkspacesConfig struct {
	Active      string            `yaml:"active,omitempty"       json:"active"`
	TabPosition string            `yaml:"tab_position,omitempty" json:"tab_position"`
	Items       []WorkspaceConfig `yaml:"items,omitempty"       json:"items"`
}

// Config field order controls YAML serialization order for newly written
// config files, so it intentionally prioritizes user-facing output over
// fieldalignment.
type Config struct { //nolint:govet
	Server         ServerConfig             `yaml:"server"`
	SSHConnections map[string]SSHConnection `yaml:"ssh_connections,omitempty"`
	Workspaces     WorkspacesConfig         `yaml:"workspaces,omitempty" json:"workspaces"`
	Layout         LayoutNode               `yaml:"layout,omitempty"`
	Display        DisplayConfig            `yaml:"display,omitempty" json:"display"`

	filePath      string
	sshConfigPath string // overridable for tests; empty = use sshconfig.DefaultPath()
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.filePath = path
	cfg.normalizeWorkspaces()
	cfg.expandPaths()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if err := tightenConfigFilePermissions(path); err != nil {
		//nolint:gosec // G706: local filesystem warning
		log.Printf("Warning: failed to tighten config file permissions to 0600: %v", err)
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
			ShowStatusBar: true,
		},
		Layout: defaultLayout(),
		Workspaces: WorkspacesConfig{
			Active:      "default",
			TabPosition: "top",
			Items: []WorkspaceConfig{
				{
					ID:     "default",
					Title:  "Default",
					Layout: defaultLayout(),
				},
			},
		},
	}
}

func defaultLayout() LayoutNode {
	return LayoutNode{
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
	}
}

func (c *Config) normalizeWorkspaces() {
	c.Workspaces = c.normalizedWorkspaces()
	if active, ok := c.ActiveWorkspace(); ok {
		c.Layout = active.Layout
	}
}

func (c *Config) normalizedWorkspaces() WorkspacesConfig {
	workspaces := c.Workspaces
	if len(workspaces.Items) == 0 {
		workspaces = WorkspacesConfig{
			Active:      "default",
			TabPosition: "top",
			Items: []WorkspaceConfig{
				{
					ID:     "default",
					Title:  "Default",
					Layout: c.Layout,
				},
			},
		}
	}
	if workspaces.TabPosition == "" {
		workspaces.TabPosition = "top"
	}
	if workspaces.Active == "" && len(workspaces.Items) > 0 {
		workspaces.Active = workspaces.Items[0].ID
	}
	return workspaces
}

func (c *Config) ActiveWorkspace() (WorkspaceConfig, bool) {
	if len(c.Workspaces.Items) == 0 {
		return WorkspaceConfig{}, false
	}
	for _, workspace := range c.Workspaces.Items {
		if workspace.ID == c.Workspaces.Active {
			return workspace, true
		}
	}
	return WorkspaceConfig{}, false
}

func (c *Config) ActiveLayout() LayoutNode {
	if workspace, ok := c.ActiveWorkspace(); ok {
		return workspace.Layout
	}
	return c.Layout
}

func (c *Config) SetActiveWorkspace(id string) bool {
	for _, workspace := range c.Workspaces.Items {
		if workspace.ID == id {
			c.Workspaces.Active = id
			c.Layout = workspace.Layout
			return true
		}
	}
	return false
}

func (c *Config) UpdateWorkspaceLayout(id string, layout LayoutNode) bool {
	for i := range c.Workspaces.Items {
		if c.Workspaces.Items[i].ID == id {
			c.Workspaces.Items[i].Layout = layout
			if c.Workspaces.Active == id {
				c.Layout = layout
			}
			return true
		}
	}
	return false
}

func (c *Config) ActiveWorkspaceID() string {
	if c.Workspaces.Active != "" {
		return c.Workspaces.Active
	}
	if len(c.Workspaces.Items) > 0 {
		return c.Workspaces.Items[0].ID
	}
	return "default"
}

func (c *Config) WorkspacesView() WorkspacesConfig {
	return c.normalizedWorkspaces()
}

func (c *Config) SaveWorkspaces() error {
	c.normalizeWorkspaces()
	return c.write()
}

func (c *Config) AddDefaultWorkspace() WorkspaceConfig {
	c.normalizeWorkspaces()
	n := len(c.Workspaces.Items) + 1
	id := c.nextWorkspaceID(n)
	workspace := WorkspaceConfig{
		ID:     id,
		Title:  "Workspace " + strconv.Itoa(n),
		Layout: singleLocalPaneLayout(c.nextPaneID(id + "-main")),
	}
	c.Workspaces.Items = append(c.Workspaces.Items, workspace)
	c.Workspaces.Active = workspace.ID
	c.Layout = workspace.Layout
	return workspace
}

func (c *Config) nextWorkspaceID(start int) string {
	seen := make(map[string]bool, len(c.Workspaces.Items))
	for _, workspace := range c.Workspaces.Items {
		seen[workspace.ID] = true
	}
	for n := start; ; n++ {
		id := "workspace-" + strconv.Itoa(n)
		if !seen[id] {
			return id
		}
	}
}

func (c *Config) nextPaneID(base string) string {
	seen := make(map[string]bool)
	for _, pane := range c.AllPanes() {
		seen[pane.ID] = true
	}
	if !seen[base] {
		return base
	}
	for n := 2; ; n++ {
		id := base + "-" + strconv.Itoa(n)
		if !seen[id] {
			return id
		}
	}
}

func singleLocalPaneLayout(paneID string) LayoutNode {
	return LayoutNode{
		Direction: "horizontal",
		Children: []LayoutChild{
			{
				Size: 100.0,
				Pane: &PaneConfig{
					ID:    paneID,
					Type:  "local",
					Shell: os.Getenv("SHELL"),
					Title: "Terminal",
				},
			},
		},
	}
}

func (c *Config) write() error {
	if c.filePath == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(c.filePath), 0750); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	type configFile struct { //nolint:govet
		Server         ServerConfig             `yaml:"server"`
		SSHConnections map[string]SSHConnection `yaml:"ssh_connections,omitempty"`
		Workspaces     WorkspacesConfig         `yaml:"workspaces,omitempty"`
		Display        DisplayConfig            `yaml:"display,omitempty"`
	}
	data, err := yaml.Marshal(configFile{
		Server:         c.Server,
		SSHConnections: c.SSHConnections,
		Workspaces:     c.Workspaces,
		Display:        c.Display,
	})
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(c.filePath, data, configFileMode); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
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
	for i := range c.Workspaces.Items {
		expandPanesCwd(c.Workspaces.Items[i].Layout.Children)
	}
	if active, ok := c.ActiveWorkspace(); ok {
		c.Layout = active.Layout
	}
}

func expandPanesCwd(children []LayoutChild) {
	for i := range children {
		if children[i].Pane != nil {
			ExpandPanePaths(children[i].Pane)
		}
		expandPanesCwd(children[i].Children)
	}
}

// ExpandPanePaths expands ~/  in the pane's CWD to an absolute path.
func ExpandPanePaths(pane *PaneConfig) {
	if strings.HasPrefix(pane.Cwd, "~/") {
		home, _ := os.UserHomeDir()
		pane.Cwd = filepath.Join(home, pane.Cwd[2:])
	}
}

// ExpandLayoutPaths expands ~/  in all pane CWDs within layout, mirroring
// the expansion done at config load time via Load().
func ExpandLayoutPaths(layout *LayoutNode) {
	expandPanesCwd(layout.Children)
}

// DefaultConfigPath returns the default config file path: ~/.config/panemux/config.yaml.
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".config", "panemux", "config.yaml"), nil
}

// LoadOrDefault loads from the default config path if the file exists,
// otherwise returns a Default config with filePath set to the default path
// so that future saves go there.
func LoadOrDefault() (*Config, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return defaultAfterConfigPathError()
	}
	return loadOrDefaultAt(path)
}

func defaultAfterConfigPathError() (*Config, error) {
	// Preserve the historical startup fallback when the user's home directory
	// cannot be resolved.
	return Default(), nil
}

func loadOrDefaultAt(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := Default()
		cfg.filePath = path
		return cfg, nil
	}
	return Load(path)
}

// SaveLayout updates the layout section and writes the config file.
func (c *Config) SaveLayout(layout LayoutNode) error {
	c.normalizeWorkspaces()
	c.UpdateLayout(layout)
	return c.write()
}

func tightenConfigFilePermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("checking config permissions: %w", err)
	}
	if info.Mode().Perm() == configFileMode {
		return nil
	}
	if err := chmodConfigFile(path, configFileMode); err != nil {
		return fmt.Errorf("tightening config permissions: %w", err)
	}
	return nil
}

// UpdateLayout updates the in-memory layout without persisting to disk.
func (c *Config) UpdateLayout(layout LayoutNode) {
	c.normalizeWorkspaces()
	if !c.UpdateWorkspaceLayout(c.ActiveWorkspaceID(), layout) {
		c.Layout = layout
	}
}

// AllPanes returns a flat list of all pane configs.
func (c *Config) AllPanes() []*PaneConfig {
	var panes []*PaneConfig
	workspaces := c.normalizedWorkspaces()
	for _, workspace := range workspaces.Items {
		collectPanes(workspace.Layout.Children, &panes)
	}
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
	c.normalizeWorkspaces()
	for i := range c.Workspaces.Items {
		c.Workspaces.Items[i].Layout.Children = removePaneChildren(c.Workspaces.Items[i].Layout.Children, paneID)
	}
	if active, ok := c.ActiveWorkspace(); ok {
		c.Layout = active.Layout
	}
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
