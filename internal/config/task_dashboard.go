package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// defaultTaskDashboardShortcut is the task dashboard's layer-switching key
// when display.task_dashboard_shortcut is not set.
const defaultTaskDashboardShortcut = "S"

// reservedShortcutKeys are the Cmd/Ctrl+Shift letters panemux already binds:
// K opens the command center palette and B the Agent Board dashboard.
var reservedShortcutKeys = map[string]string{
	"K": "the command center palette",
	"B": "the Agent Board dashboard",
}

var shortcutLetterRe = regexp.MustCompile(`^[A-Za-z]$`)

// TaskDashboardShortcutKey is the effective layer-switching letter, in upper
// case. An unset value stays unset in the file, so an unrelated save never
// writes a key the operator did not choose.
func (d DisplayConfig) TaskDashboardShortcutKey() string {
	if d.TaskDashboardShortcut == "" {
		return defaultTaskDashboardShortcut
	}
	return strings.ToUpper(d.TaskDashboardShortcut)
}

func validateDisplay(d DisplayConfig) []string {
	if d.TaskDashboardShortcut == "" {
		return nil
	}
	if !shortcutLetterRe.MatchString(d.TaskDashboardShortcut) {
		return []string{fmt.Sprintf(
			"display.task_dashboard_shortcut %q must be a single letter A-Z", d.TaskDashboardShortcut)}
	}
	if owner, taken := reservedShortcutKeys[strings.ToUpper(d.TaskDashboardShortcut)]; taken {
		return []string{fmt.Sprintf(
			"display.task_dashboard_shortcut %q is already used by %s", d.TaskDashboardShortcut, owner)}
	}
	return nil
}

// TaskDashboardConfig holds the top-level task_dashboard settings.
type TaskDashboardConfig struct {
	// Autolinks turn references in a task's branch name and pull request
	// title into links, the way a GitHub repository's autolink references
	// do. Empty means none.
	Autolinks []AutolinkConfig `yaml:"autolinks,omitempty" json:"autolinks,omitempty"`
}

// AutolinkConfig is one autolink reference, shaped like GitHub's (the
// key_prefix, url_template and is_alphanumeric of its REST API): KeyPrefix
// followed by an identifier links to URLTemplate with every <num> replaced
// by that identifier.
type AutolinkConfig struct {
	KeyPrefix   string `yaml:"key_prefix"  json:"key_prefix"`
	URLTemplate string `yaml:"url_template" json:"url_template"`
	// IsAlphanumeric makes the identifier A-Z (either case), 0-9 and "-",
	// as GitHub defines it. Unset means digits only.
	IsAlphanumeric bool `yaml:"is_alphanumeric,omitempty" json:"is_alphanumeric,omitempty"`
}

// autolinkPlaceholder is where the identifier goes in a url_template.
const autolinkPlaceholder = "<num>"

// URL is the link for the identifier num.
func (a AutolinkConfig) URL(num string) string {
	return strings.ReplaceAll(a.URLTemplate, autolinkPlaceholder, num)
}

// validateTaskDashboard checks every autolink's key_prefix and url_template.
func validateTaskDashboard(d TaskDashboardConfig) []string {
	var errs []string
	for i, link := range d.Autolinks {
		field := fmt.Sprintf("task_dashboard.autolinks[%d]", i)
		if msg := validateAutolinkKeyPrefix(d.Autolinks[:i], link.KeyPrefix); msg != "" {
			errs = append(errs, fmt.Sprintf("%s.key_prefix %q %s", field, link.KeyPrefix, msg))
		}
		if !validAutolinkURLTemplate(link.URLTemplate) {
			errs = append(errs, fmt.Sprintf(
				"%s.url_template %q must be an https URL with a valid host and port, no credentials, "+
					"and %s after the host", field, link.URLTemplate, autolinkPlaceholder))
		}
	}
	return errs
}

// validateAutolinkKeyPrefix refuses an empty prefix, one with whitespace or
// control characters, and — as GitHub does — one that overlaps a prefix
// before it: TICKET and TICK would both match TICKET123.
func validateAutolinkKeyPrefix(earlier []AutolinkConfig, prefix string) string {
	if prefix == "" || strings.IndexFunc(prefix, isSpaceOrControl) >= 0 {
		return "must be non-empty text without whitespace"
	}
	for _, other := range earlier {
		if strings.HasPrefix(prefix, other.KeyPrefix) || strings.HasPrefix(other.KeyPrefix, prefix) {
			return fmt.Sprintf("overlaps key_prefix %q", other.KeyPrefix)
		}
	}
	return ""
}

func isSpaceOrControl(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r)
}

// validAutolinkURLTemplate accepts only an absolute https URL with a host,
// no credentials, no whitespace or control characters, and <num> after the
// host, so an identifier never changes where the link goes. The URL becomes
// the href of a link the dashboard opens, so nothing but https reaches it.
//
// The host and port are held to what the browser's URL parser (the WHATWG
// one behind the frontend's HttpUrlSchema) also accepts, since the browser
// rejects the whole GET /api/tasks response for one link it cannot parse.
// testdata/autolink-url-validation.json is checked by both sides.
func validAutolinkURLTemplate(template string) bool {
	at := strings.Index(template, autolinkPlaceholder)
	if at < 0 || strings.IndexFunc(template, isSpaceOrControl) >= 0 {
		return false
	}
	authority, found := strings.CutPrefix(template[:at], "https://")
	if !found || !strings.ContainsAny(authority, "/?#") {
		return false
	}
	u, err := url.Parse(AutolinkConfig{URLTemplate: template}.URL("0"))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return false
	}
	return validLinkHost(u.Hostname()) && validLinkPort(u.Port())
}

// validLinkHost is an IP address, or dot-separated labels of ASCII letters,
// digits and hyphens. It is narrower than a browser allows, on purpose:
//   - a label starting "xn--" is punycode, which a browser decodes and rejects
//     when that fails; panemux has no decoder to check it with, so
//     internationalized hosts are refused rather than guessed at;
//   - a last label that is a number ("123", "0x1") makes a browser parse the
//     whole host as an IPv4 address, so such a host must be one.
func validLinkHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if label == "" || strings.HasPrefix(strings.ToLower(label), "xn--") {
			return false
		}
		if strings.IndexFunc(label, isNotHostLabelRune) >= 0 {
			return false
		}
	}
	return !isNumericHostLabel(labels[len(labels)-1])
}

func isNotHostLabelRune(r rune) bool {
	return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-'
}

// isNumericHostLabel reports whether the WHATWG URL parser reads label as a
// number: decimal digits, or "0x"/"0X" followed by hex digits (possibly none).
// validLinkHost never passes it an empty label.
func isNumericHostLabel(label string) bool {
	digits, base := label, "0123456789"
	if len(label) >= 2 && label[0] == '0' && (label[1] == 'x' || label[1] == 'X') {
		digits, base = label[2:], "0123456789abcdefABCDEF"
	}
	for _, r := range digits {
		if !strings.ContainsRune(base, r) {
			return false
		}
	}
	return true
}

// validLinkPort is no port, or a TCP port 1-65535. url.Parse has already
// refused anything but digits.
func validLinkPort(port string) bool {
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}
