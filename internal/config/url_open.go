package config

// URLOpenConfig holds the top-level url_open settings that govern what
// happens when a URL is opened from a pane. See docs/behavior.md
// "Opening URLs from a pane".
type URLOpenConfig struct {
	// BrowserShim gates browser-open interception. Unset means enabled:
	// the tri-state pointer keeps an operator's file from being rewritten
	// with a value they never chose.
	BrowserShim *bool `yaml:"browser_shim,omitempty" json:"browser_shim,omitempty"`
}

// BrowserShimEnabled reports whether new panes install the browser-open shim.
func (c *Config) BrowserShimEnabled() bool {
	if c.URLOpen.BrowserShim == nil {
		return true
	}
	return *c.URLOpen.BrowserShim
}
