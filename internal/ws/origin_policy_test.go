package ws

import (
	"github.com/stretchr/testify/assert"
	"net/http/httptest"
	"testing"
)

func TestOriginPolicy(t *testing.T) {
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
				assert.Equal(t, want, checkOrigin(r))
			})
		}
	}
	t.Run("duplicate origin", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/ws/pane", nil)
		r.Host = "example.test"
		r.Header.Add("Origin", "http://example.test")
		r.Header.Add("Origin", "http://example.test")
		assert.False(t, checkOrigin(r))
	})
}
