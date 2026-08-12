package wshell

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestRateLimiterInMemoryLimitsRequestsPerSourceIP(t *testing.T) {
	e := echo.New()
	handlerCalls := 0
	handler := RateLimiter(nil, 2, time.Minute)(func(c echo.Context) error {
		handlerCalls++
		return c.NoContent(http.StatusNoContent)
	})

	tests := []struct {
		name       string
		sourceIP   string
		wantStatus int
	}{
		{name: "first request", sourceIP: "192.0.2.1", wantStatus: http.StatusNoContent},
		{name: "second request", sourceIP: "192.0.2.1", wantStatus: http.StatusNoContent},
		{name: "limit exceeded", sourceIP: "192.0.2.1", wantStatus: http.StatusTooManyRequests},
		{name: "different source has own bucket", sourceIP: "192.0.2.2", wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/resource", nil), recorder)
			ctx.Set(ContextKey_SourceIP, tt.sourceIP)

			err := handler(ctx)
			if tt.wantStatus == http.StatusTooManyRequests {
				httpErr, ok := err.(*echo.HTTPError)
				if !ok {
					t.Fatalf("error type: got %T want *echo.HTTPError", err)
				}
				if httpErr.Code != tt.wantStatus {
					t.Fatalf("status: got %d want %d", httpErr.Code, tt.wantStatus)
				}
				if httpErr.Message != "Rate limit exceeded" {
					t.Fatalf("message: got %q want %q", httpErr.Message, "Rate limit exceeded")
				}
				return
			}

			if err != nil {
				t.Fatalf("middleware returned an error: %v", err)
			}
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status: got %d want %d", recorder.Code, tt.wantStatus)
			}
		})
	}

	if handlerCalls != 3 {
		t.Fatalf("downstream handler calls: got %d want 3", handlerCalls)
	}
}

func TestInMemoryLimiterStartsNewWindowAfterExpiry(t *testing.T) {
	limiter := newInMemoryLimiter()
	const key = "192.0.2.1"

	if !limiter.allow(key, 1, time.Minute) {
		t.Fatal("first request was unexpectedly rejected")
	}
	if limiter.allow(key, 1, time.Minute) {
		t.Fatal("request beyond the limit was unexpectedly allowed")
	}

	limiter.mu.Lock()
	limiter.buckets[key].resetAt = time.Now().Add(-time.Second)
	limiter.mu.Unlock()

	if !limiter.allow(key, 1, time.Minute) {
		t.Fatal("first request in the new window was unexpectedly rejected")
	}

	limiter.mu.Lock()
	bucket := *limiter.buckets[key]
	limiter.mu.Unlock()
	if bucket.count != 1 {
		t.Fatalf("new-window count: got %d want 1", bucket.count)
	}
}
