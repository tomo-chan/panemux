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
	fetchNone       = "none"
	fetchSameOrigin = "same-origin"
)

// Allowed permits CLI requests without browser headers, but refuses another browser authority.
// Scheme is intentionally not compared to r.TLS: TLS may terminate at a proxy.
func Allowed(r *http.Request) bool {
	sites := r.Header.Values("Sec-Fetch-Site")
	if len(sites) > 1 || (len(sites) == 1 && sites[0] != fetchSameOrigin && sites[0] != fetchNone) {
		return false
	}
	referers := r.Header.Values("Referer")
	if len(referers) > 1 || (len(referers) == 1 && !matchesAuthority(r, referers[0], false)) {
		return false
	}
	origins := r.Header.Values("Origin")
	if len(origins) == 0 {
		if len(sites) == 1 && sites[0] == fetchNone {
			return false
		}
		if len(referers) == 1 {
			return true
		}
		for key := range r.Header {
			if strings.HasPrefix(strings.ToLower(key), "sec-fetch-") {
				return false
			}
		}
		return true
	}
	return len(origins) == 1 && matchesAuthority(r, origins[0], true)
}

func matchesAuthority(r *http.Request, raw string, origin bool) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" ||
		u.Opaque != "" || u.Fragment != "" || strings.Contains(raw, "#") {

		return false
	}
	if origin && (u.Path != "" || u.RawQuery != "" || u.ForceQuery) {
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

// Middleware checks browser evidence before any handler side effects.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Refuse(w, r) {
			next.ServeHTTP(w, r)
		}
	})
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
