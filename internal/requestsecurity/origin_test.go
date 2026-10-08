package requestsecurity

import (
	"github.com/stretchr/testify/assert"
	"net/http/httptest"
	"testing"
)

func TestAllowedOriginPolicy(t *testing.T) {
	cases := []struct {
		name, origin, host string
		want               bool
	}{
		{"same", "http://localhost:8080", "localhost:8080", true},
		{"other port", "http://localhost:3000", "localhost:8080", false},
		{"other loopback name", "http://127.0.0.1:8080", "localhost:8080", false},
		{"default http", "http://example.test", "example.test:80", true},
		{"default https", "https://example.test:443", "example.test", true},
		{"wrong default", "https://example.test", "example.test:80", false},
		{"case", "http://EXAMPLE.test:80", "example.TEST", true},
		{"ipv6 default http", "http://[::1]", "[::1]:80", true},
		{"ipv6 default https", "https://[::1]:443", "[0:0:0:0:0:0:0:1]", true},
		{"ipv6 different address", "http://[::1]:80", "[::2]:80", false},
		{"DNS bracket request host", "http://example.test:80", "[example.test]:80", false},
		{"IPv4 bracket", "http://127.0.0.1:80", "[127.0.0.1]:80", false},
		{"unclosed bracket", "http://example.test:80", "[::1", false},
		{"invalid bracket literal host", "http://example.test:80", "[not:ip]:80", false},
		{"unbracketed IPv6", "http://[::1]:8080", "::1:8080", false},
		{"IPv6 zone host", "http://[fe80::1]:80", "[fe80::1%25eth0]:80", false},
		{"IPv6 zone origin", "http://[fe80::1%25eth0]:80", "[fe80::1%25eth0]:80", false},
		{"ipv6", "http://[0:0:0:0:0:0:0:1]:8080", "[::1]:8080", true},
		{"scheme limitation", "https://example.test:8080", "example.test:8080", true},
		{"null", "null", "example.test", false},
		{"userinfo", "http://user@example.test", "example.test", false},
		{"path", "http://example.test/", "example.test", false},
		{"query", "http://example.test?x", "example.test", false},
		{"fragment", "http://example.test#x", "example.test", false},
		{"multiple", "http://example.test http://other.test", "example.test", false},
		{"wrong scheme", "ftp://example.test", "example.test", false},
		{"empty port", "http://example.test:", "example.test", false},
		{"zero port", "http://example.test:0", "example.test:0", false},
		{"large port", "http://example.test:65536", "example.test:65536", false},
		{"host empty port", "http://example.test", "example.test:", false},
		{"minimum port", "http://example.test:1", "example.test:1", true},
		{"maximum port", "http://example.test:65535", "example.test:65535", true},
		{"unbracketed invalid authority", "http://example.test:80", "not:an:ip:80", false},
		{"bad host", "http://example.test", "example.test/path", false},
		{"invalid IP literal", "http://[not:ip]", "[not:ip]", false},
		{"non-IP brackets", "http://[example.test]", "[example.test]", false},
		{"origin absent", "", "localhost:8080", true},
	}
	for _, tc := range cases {
		for _, site := range []string{"", "same-origin", "none", "same-site", "cross-site"} {
			t.Run(tc.name+"/"+site, func(t *testing.T) {
				r := httptest.NewRequest("GET", "/ws/pane", nil)
				r.Host = tc.host
				if tc.origin != "" {
					r.Header.Set("Origin", tc.origin)
				}
				r.Header.Set("Sec-Fetch-Site", site)
				want := tc.want && site != "same-site" && site != "cross-site"
				assert.Equal(t, want, Allowed(r))
			})
		}
	}
}

func TestAllowedRejectsDuplicateOrigin(t *testing.T) {
	r := httptest.NewRequest("GET", "/ws/pane", nil)
	r.Host = "example.test"
	r.Header.Add("Origin", "http://example.test")
	r.Header.Add("Origin", "http://example.test")
	assert.False(t, Allowed(r))
}

func TestRefuse(t *testing.T) {
	for _, origin := range []string{"", "http://other.test"} {
		r := httptest.NewRequest("POST", "/api/tasks", nil)
		r.Header.Set("Origin", origin)
		if origin == "" {
			r.Header.Del("Origin")
		}
		w := httptest.NewRecorder()
		refused := Refuse(w, r)
		assert.Equal(t, origin != "", refused)
		if refused {
			assert.Equal(t, 403, w.Code)
		} else {
			assert.Equal(t, 200, w.Code)
		}
	}
}
