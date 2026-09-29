package portforward

import "testing"

type callbackPortCase struct {
	name     string
	raw      string
	wantPort int
	wantOK   bool
}

var callbackPortCases = []callbackPortCase{
	{
		name:     "encoded loopback redirect_uri",
		raw:      "https://example.com/auth?client_id=x&redirect_uri=http%3A%2F%2Flocalhost%3A8085%2Fcallback",
		wantPort: 8085,
		wantOK:   true,
	},
	{
		name:     "plain loopback redirect_uri",
		raw:      "https://example.com/auth?redirect_uri=http://127.0.0.1:41234/oauth",
		wantPort: 41234,
		wantOK:   true,
	},
	{
		name:     "redirect_url spelling",
		raw:      "https://example.com/auth?redirect_url=http%3A%2F%2Flocalhost%3A9999%2Fcb",
		wantPort: 9999,
		wantOK:   true,
	},
	{
		name:     "callback_url spelling",
		raw:      "https://example.com/auth?callback_url=http%3A%2F%2Flocalhost%3A9998%2Fcb",
		wantPort: 9998,
		wantOK:   true,
	},
	{
		name:     "callback_uri spelling",
		raw:      "https://example.com/auth?callback_uri=http%3A%2F%2Flocalhost%3A9997%2Fcb",
		wantPort: 9997,
		wantOK:   true,
	},
	{
		name:     "mixed case parameter name",
		raw:      "https://example.com/auth?RedirectUri=http%3A%2F%2Flocalhost%3A9996%2Fcb",
		wantPort: 9996,
		wantOK:   true,
	},
	{
		name:     "IPv6 loopback redirect",
		raw:      "https://example.com/auth?redirect_uri=http%3A%2F%2F%5B%3A%3A1%5D%3A7777%2Fcb",
		wantPort: 7777,
		wantOK:   true,
	},
	{
		name:     "uppercase loopback host",
		raw:      "https://example.com/auth?redirect_uri=http%3A%2F%2FLOCALHOST%3A7778%2Fcb",
		wantPort: 7778,
		wantOK:   true,
	},
	{
		name:     "127.x.x.x loopback range",
		raw:      "https://example.com/auth?redirect_uri=http%3A%2F%2F127.0.0.53%3A7779%2Fcb",
		wantPort: 7779,
		wantOK:   true,
	},
	{
		name:     "clicked URL is itself a loopback URL",
		raw:      "http://localhost:3000/dashboard",
		wantPort: 3000,
		wantOK:   true,
	},
	{
		name:     "clicked loopback URL wins over redirect parameter",
		raw:      "http://127.0.0.1:3000/x?redirect_uri=http%3A%2F%2Flocalhost%3A4000%2Fcb",
		wantPort: 3000,
		wantOK:   true,
	},
	{
		name:   "non-loopback redirect is not forwarded",
		raw:    "https://example.com/auth?redirect_uri=https%3A%2F%2Fapp.example.com%2Fcb",
		wantOK: false,
	},
	{
		name:   "loopback redirect on a privileged port is not forwarded",
		raw:    "https://example.com/auth?redirect_uri=http%3A%2F%2Flocalhost%2Fcb",
		wantOK: false,
	},
	{
		name:   "loopback redirect on port 443 default is not forwarded",
		raw:    "https://example.com/auth?redirect_uri=https%3A%2F%2Flocalhost%2Fcb",
		wantOK: false,
	},
	{
		name:   "non-http redirect scheme is ignored",
		raw:    "https://example.com/auth?redirect_uri=myapp%3A%2F%2Flocalhost%3A8085%2Fcb",
		wantOK: false,
	},
	{
		name:   "no query string",
		raw:    "https://example.com/auth",
		wantOK: false,
	},
	{
		name:   "unrelated query parameters",
		raw:    "https://example.com/auth?state=abc&scope=read",
		wantOK: false,
	},
	{
		name:   "redirect parameter is not a URL",
		raw:    "https://example.com/auth?redirect_uri=not-a-url",
		wantOK: false,
	},
	{
		name:   "redirect port out of range",
		raw:    "https://example.com/auth?redirect_uri=http%3A%2F%2Flocalhost%3A99999%2Fcb",
		wantOK: false,
	},
	// The forwardable range's own boundaries. Every case above is at least
	// one port clear of both ends — 3000 and 99999 are the closest — so
	// `port > minForwardablePort && port < maxPort` would have refused to
	// forward the lowest and highest ports panemux is willing to bind with
	// the suite still green. Issue #190.
	{
		name:   "one below the lowest forwardable port",
		raw:    "http://localhost:1023/cb",
		wantOK: false,
	},
	{
		name:     "the lowest forwardable port itself",
		raw:      "http://localhost:1024/cb",
		wantPort: 1024,
		wantOK:   true,
	},
	{
		name:     "one above the lowest forwardable port",
		raw:      "http://localhost:1025/cb",
		wantPort: 1025,
		wantOK:   true,
	},
	{
		name:     "one below the highest forwardable port",
		raw:      "http://localhost:65534/cb",
		wantPort: 65534,
		wantOK:   true,
	},
	{
		name:     "the highest forwardable port itself",
		raw:      "http://localhost:65535/cb",
		wantPort: 65535,
		wantOK:   true,
	},
	{
		name:   "one above the highest forwardable port",
		raw:    "http://localhost:65536/cb",
		wantOK: false,
	},
	{
		name:   "unparseable URL",
		raw:    "http://%zz",
		wantOK: false,
	},
	{
		name:   "empty URL",
		raw:    "",
		wantOK: false,
	},
}

func TestCallbackPort(t *testing.T) {
	for _, tt := range callbackPortCases {
		t.Run(tt.name, func(t *testing.T) {
			port, ok := CallbackPort(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("CallbackPort(%q) ok = %v, want %v", tt.raw, ok, tt.wantOK)
			}
			if ok && port != tt.wantPort {
				t.Fatalf("CallbackPort(%q) port = %d, want %d", tt.raw, port, tt.wantPort)
			}
		})
	}
}

func TestValidateOpenURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "https", raw: "https://example.com/auth"},
		{name: "http", raw: "http://localhost:8085/cb"},
		{name: "uppercase scheme", raw: "HTTPS://example.com/auth"},
		{name: "empty", raw: "", wantErr: true},
		{name: "file scheme", raw: "file:///etc/passwd", wantErr: true},
		{name: "javascript scheme", raw: "javascript:alert(1)", wantErr: true},
		{name: "scheme relative", raw: "//example.com/auth", wantErr: true},
		{name: "no host", raw: "https:///path", wantErr: true},
		{name: "unparseable", raw: "http://%zz", wantErr: true},
		{name: "embedded newline", raw: "https://example.com/a\nb", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOpenURL(tt.raw)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateOpenURL(%q) = nil, want error", tt.raw)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateOpenURL(%q) = %v, want nil", tt.raw, err)
			}
		})
	}
}
