package wshell

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestSourceIPCanonicalization(t *testing.T) {
	tests := []struct {
		name         string
		cloudflare   string
		forwardedFor string
		remoteAddr   string
		want         string
	}{
		{
			name:       "Cloudflare IPv4",
			cloudflare: " 203.0.113.10 ",
			remoteAddr: "127.0.0.1:1234",
			want:       "203.0.113.10",
		},
		{
			name:       "Cloudflare IPv6",
			cloudflare: " 2001:0db8:0:0:0:0:0:1 ",
			remoteAddr: "127.0.0.1:1234",
			want:       "2001:db8::1",
		},
		{
			name:         "Cloudflare takes precedence over X-Forwarded-For",
			cloudflare:   "203.0.113.10",
			forwardedFor: "198.51.100.20, 198.51.100.30",
			remoteAddr:   "127.0.0.1:1234",
			want:         "203.0.113.10",
		},
		{
			name:         "last X-Forwarded-For entry",
			forwardedFor: " 198.51.100.20, 2001:0db8:0:0:0:0:0:2 ",
			remoteAddr:   "127.0.0.1:1234",
			want:         "2001:db8::2",
		},
		{
			name:       "direct IPv4 RemoteAddr",
			remoteAddr: "127.0.0.1:1234",
			want:       "127.0.0.1",
		},
		{
			name:       "direct IPv6 RemoteAddr",
			remoteAddr: "[::1]:1234",
			want:       "::1",
		},
		{
			name:       "RemoteAddr without port",
			remoteAddr: "2001:0db8:0:0:0:0:0:3",
			want:       "2001:db8::3",
		},
		{
			name:       "IPv4-mapped IPv6 is unmapped",
			remoteAddr: "[::ffff:192.0.2.128]:1234",
			want:       "192.0.2.128",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runSourceIPMiddleware(t, tt.cloudflare, tt.forwardedFor, tt.remoteAddr)
			if got != tt.want {
				t.Fatalf("unexpected source IP: got %q want %q", got, tt.want)
			}
			if strings.ContainsAny(got, "/[]") {
				t.Fatalf("source IP contains a CIDR suffix or brackets: %q", got)
			}
			if _, err := netip.ParseAddr(got); err != nil {
				t.Fatalf("source IP is not a plain IP address: %q: %v", got, err)
			}
		})
	}
}

func TestSourceIPInvalidSelectedValue(t *testing.T) {
	tests := []struct {
		name         string
		cloudflare   string
		forwardedFor string
		remoteAddr   string
	}{
		{
			name:         "invalid Cloudflare value",
			cloudflare:   "not-an-ip",
			forwardedFor: "198.51.100.30",
			remoteAddr:   "127.0.0.1:1234",
		},
		{
			name:         "invalid selected X-Forwarded-For entry",
			forwardedFor: "198.51.100.20, not-an-ip",
			remoteAddr:   "127.0.0.1:1234",
		},
		{
			name:       "invalid RemoteAddr",
			remoteAddr: "not-an-ip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runSourceIPMiddleware(t, tt.cloudflare, tt.forwardedFor, tt.remoteAddr)
			if got != "" {
				t.Fatalf("unexpected source IP: got %q want empty string", got)
			}
		})
	}
}

func runSourceIPMiddleware(t *testing.T, cloudflare, forwardedFor, remoteAddr string) string {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	req.Header.Set("CF-Connecting-IP", cloudflare)
	req.Header.Set("X-Forwarded-For", forwardedFor)
	c := e.NewContext(req, httptest.NewRecorder())

	err := SourceIP()(func(c echo.Context) error { return nil })(c)
	if err != nil {
		t.Fatalf("middleware returned an error: %v", err)
	}

	value, ok := c.Get(ContextKey_SourceIP).(string)
	if !ok {
		t.Fatalf("%s missing or not a string", ContextKey_SourceIP)
	}

	return value
}
