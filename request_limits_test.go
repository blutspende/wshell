package wshell

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestRequestBodyLimit(t *testing.T) {
	for _, tc := range []struct {
		name          string
		limit         int64
		body          string
		unknownLength bool
		wantStatus    int
	}{
		{"below limit", 8, "1234567", false, 200},
		{"at limit", 8, "12345678", false, 200},
		{"known length exceeds limit", 8, "123456789", false, 413},
		{"unknown length exceeds limit", 8, "123456789", true, 413},
		{"unknown length at limit", 8, "12345678", true, 200},
		{"disabled", 0, "123456789", true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := NewWebServer("body-limit").WithLoggerFunc(func(LogEvent) {}).WithRequestBodyLimit(tc.limit)
			customStatus := 0
			builder.CustomErrorHandling(func(status int, message string, c echo.Context) { customStatus = status; _ = c.String(status, message) })
			builder.echo.POST("/body", func(c echo.Context) error {
				data, err := io.ReadAll(c.Request().Body)
				if err != nil {
					return err
				}
				return c.String(200, string(data))
			})
			if err := builder.prepare(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/body", strings.NewReader(tc.body))
			if tc.unknownLength {
				req.ContentLength = -1
				req.TransferEncoding = []string{"chunked"}
			}
			rec := httptest.NewRecorder()
			builder.echo.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == 413 && customStatus != 413 {
				t.Fatalf("custom error handler status=%d", customStatus)
			}
		})
	}
}

func TestBodyLimitBeforeAuthenticationReads(t *testing.T) {
	document := strings.Replace(openAPI32Document("QUERY"), `"paths":`, `"security":[{"BearerAuth":[]}],"components":{"securitySchemes":{"BearerAuth":{"type":"http","scheme":"bearer"}}},"paths":`, 1)
	for _, unknownLength := range []bool{false, true} {
		builder := NewWebServer("auth-body-limit").WithLoggerFunc(func(LogEvent) {}).WithRequestBodyLimit(32).
			UseOpenAPISpecs("/v1", writeOpenAPITestFile(t, document), "api.yaml")
		builder.echo.Add("QUERY", "/v1/widgets/:id", func(c echo.Context) error { t.Error("unauthenticated handler called"); return c.NoContent(200) })
		if err := builder.prepare(); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			body       string
			wantStatus int
		}{
			{`{"kind":"widget"}`, 401},
			{strings.Repeat(" ", 64) + `{"kind":"widget"}`, 413},
		} {
			req := httptest.NewRequest("QUERY", "/v1/widgets/1", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if unknownLength {
				req.ContentLength = -1
				req.TransferEncoding = []string{"chunked"}
			}
			rec := httptest.NewRecorder()
			builder.echo.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("unknownLength=%v: status=%d want=%d body=%s", unknownLength, rec.Code, tc.wantStatus, rec.Body.String())
			}
		}
	}
}

func TestHTTPTimeoutDefaultsAndOverrides(t *testing.T) {
	defaults := HTTPTimeouts{10 * time.Second, 30 * time.Second, 30 * time.Second, 120 * time.Second}
	custom := HTTPTimeouts{time.Second, 2 * time.Second, 0, 4 * time.Second}
	for _, tc := range []struct {
		name     string
		timeouts *HTTPTimeouts
		want     HTTPTimeouts
	}{
		{"defaults", nil, defaults}, {"custom and disabled write deadline", &custom, custom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := NewWebServer("timeouts")
			if tc.timeouts != nil {
				builder.WithHTTPTimeouts(*tc.timeouts)
			}
			if err := builder.prepare(); err != nil {
				t.Fatal(err)
			}
			for _, server := range []*http.Server{builder.echo.Server, builder.echo.TLSServer} {
				got := HTTPTimeouts{server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout, server.IdleTimeout}
				if got != tc.want {
					t.Fatalf("timeouts=%+v want=%+v", got, tc.want)
				}
			}
			if tc.timeouts == nil && builder.requestBodyLimit != 10<<20 {
				t.Fatalf("default body limit=%d", builder.requestBodyLimit)
			}
		})
	}
}

func TestBodyLimitBeforeSchemaDecoding(t *testing.T) {
	builder := NewWebServer("schema-body-limit").WithLoggerFunc(func(LogEvent) {}).WithRequestBodyLimit(32).
		UseOpenAPISpecs("/v1", writeOpenAPITestFile(t, openAPI32Document("QUERY")), "api.yaml")
	builder.echo.Add("QUERY", "/v1/widgets/:id", func(c echo.Context) error { return c.NoContent(200) })
	if err := builder.prepare(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body       string
		wantStatus int
	}{
		{`{"kind":"widget"}`, 200},
		{strings.Repeat(" ", 64) + `{"kind":"widget"}`, 413},
	} {
		req := httptest.NewRequest("QUERY", "/v1/widgets/1", strings.NewReader(tc.body))
		req.ContentLength = -1
		req.TransferEncoding = []string{"chunked"}
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		builder.echo.ServeHTTP(rec, req)
		if rec.Code != tc.wantStatus {
			t.Fatalf("schema decoder: status=%d want=%d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
		}
	}
}

func TestReadHeaderTimeoutClosesIncompleteRequest(t *testing.T) {
	builder := NewWebServer("header-timeout").WithLoggerFunc(func(LogEvent) {}).
		WithHTTPTimeouts(HTTPTimeouts{ReadHeaderTimeout: 100 * time.Millisecond})
	server, err := builder.Start(0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(server.Address().(*net.TCPAddr).Port))
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n"); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Read(make([]byte, 1))
	if err != io.EOF {
		t.Fatalf("incomplete headers were not closed by server: %v", err)
	}
}

func TestInvalidResourceLimitsPreventStartup(t *testing.T) {
	for _, builder := range []*APIBuilder{
		NewWebServer("negative-body").WithRequestBodyLimit(-1),
		NewWebServer("negative-header").WithHTTPTimeouts(HTTPTimeouts{ReadHeaderTimeout: -1}),
		NewWebServer("negative-read").WithHTTPTimeouts(HTTPTimeouts{ReadTimeout: -1}),
		NewWebServer("negative-write").WithHTTPTimeouts(HTTPTimeouts{WriteTimeout: -1}),
		NewWebServer("negative-idle").WithHTTPTimeouts(HTTPTimeouts{IdleTimeout: -1}),
		NewWebServer("invalid-throttle").ThrottleRequests(1, 0),
	} {
		t.Run(builder.applicationName, func(t *testing.T) {
			running, err := builder.Start(0)
			if err == nil || running != nil || builder.echo.Listener != nil {
				t.Fatalf("invalid limits accepted: running=%v err=%v", running, err)
			}
		})
	}
}
