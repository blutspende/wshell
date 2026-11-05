package wshell

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestCORSActualRequestOriginHandling(t *testing.T) {
	tests := []struct {
		name            string
		origin          string
		wantAllowOrigin string
	}{
		{
			name:            "allowed origin",
			origin:          "https://portal.example.com",
			wantAllowOrigin: "https://portal.example.com",
		},
		{
			name:   "rejected origin",
			origin: "https://attacker.example",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := newCORSTestBuilder(t).
				AllowOrigins("https://portal.example.com").
				AllowCredentials(true)

			response := performCORSRequest(
				t,
				builder,
				http.MethodGet,
				tt.origin,
				"",
				"",
			)

			if response.Code != http.StatusOK {
				t.Fatalf("status: got %d want %d", response.Code, http.StatusOK)
			}
			if got := response.Header().Get(echo.HeaderAccessControlAllowOrigin); got != tt.wantAllowOrigin {
				t.Fatalf("Access-Control-Allow-Origin: got %q want %q", got, tt.wantAllowOrigin)
			}
			wantCredentials := ""
			if tt.wantAllowOrigin != "" {
				wantCredentials = "true"
			}
			if got := response.Header().Get(echo.HeaderAccessControlAllowCredentials); got != wantCredentials {
				t.Fatalf("Access-Control-Allow-Credentials: got %q want %q", got, wantCredentials)
			}
			assertHeaderContains(t, response.Header().Values(echo.HeaderVary), echo.HeaderOrigin)
		})
	}
}

func TestCORSPreflightUsesConfiguredOriginMethodsAndHeaders(t *testing.T) {
	builder := newCORSTestBuilder(t).
		AllowOrigins("https://portal.example.com").
		AllowMethods(http.MethodGet, http.MethodPatch).
		AllowHeaders(echo.HeaderAuthorization, echo.HeaderContentType, "X-API-Key").
		AllowCredentials(true)

	response := performCORSRequest(
		t,
		builder,
		http.MethodOptions,
		"https://portal.example.com",
		http.MethodPatch,
		"Authorization, Content-Type, X-API-Key",
	)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", response.Code, http.StatusNoContent)
	}
	assertHeaderEquals(t, response, echo.HeaderAccessControlAllowOrigin, "https://portal.example.com")
	assertHeaderEquals(t, response, echo.HeaderAccessControlAllowMethods, "GET,PATCH")
	assertHeaderEquals(t, response, echo.HeaderAccessControlAllowHeaders, "Authorization,Content-Type,X-API-Key")
	assertHeaderEquals(t, response, echo.HeaderAccessControlAllowCredentials, "true")
	assertHeaderContains(t, response.Header().Values(echo.HeaderVary), echo.HeaderOrigin)
	assertHeaderContains(t, response.Header().Values(echo.HeaderVary), echo.HeaderAccessControlRequestMethod)
	assertHeaderContains(t, response.Header().Values(echo.HeaderVary), echo.HeaderAccessControlRequestHeaders)
}

func TestCORSPreflightRejectsUnconfiguredOrigin(t *testing.T) {
	builder := newCORSTestBuilder(t).
		AllowOrigins("https://portal.example.com").
		AllowMethods(http.MethodGet)

	response := performCORSRequest(
		t,
		builder,
		http.MethodOptions,
		"https://attacker.example",
		http.MethodGet,
		"",
	)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", response.Code, http.StatusNoContent)
	}
	for _, header := range []string{
		echo.HeaderAccessControlAllowOrigin,
		echo.HeaderAccessControlAllowMethods,
		echo.HeaderAccessControlAllowHeaders,
		echo.HeaderAccessControlAllowCredentials,
	} {
		if got := response.Header().Get(header); got != "" {
			t.Fatalf("%s: got %q want empty", header, got)
		}
	}
}

func TestCORSPreflightDoesNotGrantUnconfiguredMethod(t *testing.T) {
	builder := newCORSTestBuilder(t).
		AllowOrigins("https://portal.example.com").
		AllowMethods(http.MethodGet, http.MethodPatch)

	response := performCORSRequest(
		t,
		builder,
		http.MethodOptions,
		"https://portal.example.com",
		http.MethodDelete,
		"",
	)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", response.Code, http.StatusNoContent)
	}
	assertHeaderEquals(t, response, echo.HeaderAccessControlAllowMethods, "GET,PATCH")
	if strings.Contains(response.Header().Get(echo.HeaderAccessControlAllowMethods), http.MethodDelete) {
		t.Fatal("Access-Control-Allow-Methods unexpectedly grants DELETE")
	}
}

func TestCORSDefaultsAllowAllOriginsWithoutCredentials(t *testing.T) {
	response := performCORSRequest(
		t,
		newCORSTestBuilder(t),
		http.MethodOptions,
		"https://portal.example.com",
		http.MethodHead,
		"",
	)

	assertHeaderEquals(t, response, echo.HeaderAccessControlAllowOrigin, "*")
	assertHeaderEquals(t, response, echo.HeaderAccessControlAllowMethods, "GET,HEAD,POST,PUT,PATCH,DELETE")
	if got := response.Header().Get(echo.HeaderAccessControlAllowCredentials); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials: got %q want empty", got)
	}
}

func TestCORSRejectsWildcardOriginWithCredentials(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
		want    string
	}{
		{name: "default wildcard", origins: []string{"*"}, want: "wildcard origin"},
		{name: "implicit wildcard from empty origins", origins: []string{}, want: "explicit allowed origin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewWebServer("cors-test").
				WithLoggerFunc(func(LogEvent) {}).
				AllowOrigins(tt.origins...).
				AllowCredentials(true)

			err := builder.prepare()
			if err == nil {
				t.Fatal("expected non-explicit origin with credentials to be rejected")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error: got %q, want explanation containing %q", err, tt.want)
			}
		})
	}
}

func newCORSTestBuilder(t *testing.T) *APIBuilder {
	t.Helper()
	return NewWebServer("cors-test").
		WithLoggerFunc(func(LogEvent) {}).
		CustomGET("/resource", func(c echo.Context) error {
			return c.NoContent(http.StatusOK)
		})
}

func performCORSRequest(
	t *testing.T,
	builder *APIBuilder,
	method string,
	origin string,
	requestedMethod string,
	requestedHeaders string,
) *httptest.ResponseRecorder {
	t.Helper()
	if err := builder.prepare(); err != nil {
		t.Fatalf("prepare server: %v", err)
	}

	request := httptest.NewRequest(method, "/resource", nil)
	request.Header.Set(echo.HeaderOrigin, origin)
	if requestedMethod != "" {
		request.Header.Set(echo.HeaderAccessControlRequestMethod, requestedMethod)
	}
	if requestedHeaders != "" {
		request.Header.Set(echo.HeaderAccessControlRequestHeaders, requestedHeaders)
	}
	response := httptest.NewRecorder()
	builder.echo.ServeHTTP(response, request)
	return response
}

func assertHeaderEquals(t *testing.T, response *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if got := response.Header().Get(name); got != want {
		t.Fatalf("%s: got %q want %q", name, got, want)
	}
}

func assertHeaderContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.TrimSpace(part) == want {
				return
			}
		}
	}
	t.Fatalf("header values %q do not contain %q", values, want)
}
