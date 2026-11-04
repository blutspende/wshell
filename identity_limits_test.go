package wshell

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

func TestIdentityDocumentSizeLimit(t *testing.T) {
	for _, tc := range []struct {
		name        string
		size        int
		knownLength bool
		wantError   bool
	}{
		{"at limit", maxIdentityDocumentBytes, true, false},
		{"known length exceeds limit", maxIdentityDocumentBytes + 100, true, true},
		{"unknown length exceeds limit", maxIdentityDocumentBytes + 100, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buffer := bytes.NewBufferString(strings.Repeat(" ", tc.size))
			length := int64(-1)
			if tc.knownLength {
				length = int64(tc.size)
			}
			_, err := readIdentityDocument(&http.Response{StatusCode: 200, ContentLength: length, Body: io.NopCloser(buffer)})
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v wantError=%v", err, tc.wantError)
			}
			if read := tc.size - buffer.Len(); read > maxIdentityDocumentBytes+1 {
				t.Fatalf("read %d bytes beyond document limit", read)
			}
		})
	}
}

func TestIdentityHTTPTimeouts(t *testing.T) {
	// These local HTTP dependencies deliberately never complete a response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client := server.Client()
	client.Timeout = 50 * time.Millisecond
	for _, name := range []string{"discovery", "jwks"} {
		t.Run(name, func(t *testing.T) {
			var err error
			if name == "discovery" {
				_, err = resolveJWKSURLWithClient(server.URL, client)
			} else {
				_, err = loadJWKS(server.URL, client)
			}
			var networkError net.Error
			if !errors.As(err, &networkError) || !networkError.Timeout() {
				t.Fatalf("expected bounded HTTP timeout, got %v", err)
			}
		})
	}
}

func TestJWKSRefreshIsRateLimitedAndSupportsRotation(t *testing.T) {
	var requests atomic.Int32
	var rotated atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		keys := `{"kty":"oct","kid":"known","k":"c2VjcmV0","alg":"HS256","use":"sig"}`
		if rotated.Load() {
			keys += `,{"kty":"oct","kid":"rotated","k":"cm90YXRlZA","alg":"HS256","use":"sig"}`
		}
		_, _ = io.WriteString(w, `{"keys":[`+keys+`]}`)
	}))
	defer server.Close()
	jwks, err := loadJWKS(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer jwks.EndBackground()
	keyFor := func(kid string) (any, error) {
		return jwks.Keyfunc(&jwt.Token{Header: map[string]any{"kid": kid, "alg": "HS256"}})
	}
	if _, err := keyFor("known"); err != nil {
		t.Fatal(err)
	}
	rotated.Store(true)
	if _, err := keyFor("rotated"); err != nil {
		t.Fatalf("key rotation was not refreshed: %v", err)
	}
	for _, kid := range []string{"bad-1", "bad-2", "bad-3"} {
		if _, err := keyFor(kid); err == nil {
			t.Fatal("unknown key was accepted")
		}
	}
	if count := requests.Load(); count != 2 {
		t.Fatalf("JWKS fetches=%d want initial load plus one refresh", count)
	}
	if _, err := keyFor("rotated"); err != nil {
		t.Fatalf("bad key IDs affected cached key: %v", err)
	}
}

func TestOversizedDiscoveryPreventsStartup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat(" ", maxIdentityDocumentBytes+1))
	}))
	defer server.Close()
	builder := NewWebServer("oversized-discovery").WithLoggerFunc(func(LogEvent) {}).WithAuthentication(server.URL).
		UseOpenAPISpecs("/v1", writeOpenAPITestFile(t, openAPI32Document("QUERY")), "api.yaml")
	running, err := builder.Start(0)
	if err == nil || running != nil || builder.echo.Listener != nil || !strings.Contains(err.Error(), "1 MiB limit") {
		t.Fatalf("oversized discovery accepted: running=%v err=%v", running, err)
	}
}

func TestOversizedJWKSIsRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat(" ", maxIdentityDocumentBytes+1))
	}))
	defer server.Close()
	jwks, err := loadJWKS(server.URL, server.Client())
	if jwks != nil {
		jwks.EndBackground()
	}
	if err == nil || !strings.Contains(err.Error(), "1 MiB limit") {
		t.Fatalf("oversized JWKS accepted: %v", err)
	}
}
