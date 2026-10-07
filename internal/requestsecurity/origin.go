// Package requestsecurity checks browser request origins without trusting proxy headers.
package requestsecurity

import (
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const (
	crossSite = "cross-site"
	sameSite  = "same-site"
)

// Allowed permits CLI requests without Origin, but refuses another browser authority.
// Scheme is intentionally not compared to r.TLS: TLS may terminate at a proxy.
func Allowed(r *http.Request) bool {
	for _, site := range r.Header.Values("Sec-Fetch-Site") {
		if site == sameSite || site == crossSite {
			return false
		}
	}
	origins := r.Header.Values("Origin")
	if len(origins) == 0 {
		return true
	}
	if len(origins) != 1 || origins[0] == "" {
		return false
	}
	u, err := url.Parse(origins[0])
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" ||
		u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || strings.Contains(origins[0], "#") {

		return false
	}
	defaultPort := 80
	if u.Scheme == "https" {
		defaultPort = 443
	}
	host, port, ok := authority(u.Host, defaultPort)
	requestHost, requestPort, requestOK := authority(r.Host, defaultPort)
	return ok && requestOK && host == requestHost && port == requestPort
}

// Refuse answers 403 and reports whether the browser request was refused.
func Refuse(w http.ResponseWriter, r *http.Request) bool {
	if Allowed(r) {
		return false
	}
	http.Error(w, "cross-site request refused", http.StatusForbidden)
	return true
}

func authority(raw string, defaultPort int) (string, int, bool) {
	// net/url's IP-literal checks differ across supported Go releases.
	// Validate the raw authority before it can be interpreted as a DNS host.
	if strings.HasPrefix(raw, "[") {
		end := strings.IndexByte(raw, ']')
		if end == -1 {
			return "", 0, false
		}
		ip, err := netip.ParseAddr(raw[1:end])
		if err != nil || !ip.Is6() || ip.Zone() != "" {
			return "", 0, false
		}
	} else if strings.Count(raw, ":") > 1 {
		return "", 0, false
	}

	u, err := url.Parse("http://" + raw)
	if err != nil || u.Host != raw || u.User != nil || u.Path != "" || u.RawQuery != "" ||
		u.Fragment != "" || u.Hostname() == "" {

		return "", 0, false
	}
	host := strings.ToLower(u.Hostname())
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		host = ip.String()
	}
	port := defaultPort
	if strings.HasSuffix(raw, ":") {
		return "", 0, false
	}
	if value := u.Port(); value != "" {
		port, err = strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return "", 0, false
		}
	}
	return host, port, true
}
