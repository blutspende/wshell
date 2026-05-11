package wshell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestNewWebServerStartsMinimalServer(t *testing.T) {
	builder := NewWebServer("testapp").
		CustomGET("/healthz", func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})

	port := freePort(t)
	errCh := make(chan error, 1)

	go func() {
		errCh <- builder.Run(port)
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	resp := waitForServer(t, errCh, url)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if string(body) != "ok" {
		t.Fatalf("unexpected response body: got %q want %q", string(body), "ok")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := builder.echo.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown server: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("server exited with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after shutdown")
	}
}

func TestSourceIPPrefersCloudflareHeaderOverProxyForwarding(t *testing.T) {
	builder := NewWebServer("testapp").
		CustomGET("/sourceip", func(c echo.Context) error {
			value, ok := c.Get(ContextKey_SourceIP).(string)
			if !ok {
				t.Fatalf("%s missing or not a string", ContextKey_SourceIP)
			}
			return c.String(http.StatusOK, value)
		})

	port := freePort(t)
	errCh := make(chan error, 1)

	go func() {
		errCh <- builder.Run(port)
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/sourceip", port)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("CF-Connecting-IP", "203.0.113.10")
	req.Header.Set("X-Forwarded-For", "198.51.100.20, 198.51.100.30")

	resp := doRequestWithRetry(t, errCh, req)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if string(body) != "203.0.113.10" {
		t.Fatalf("unexpected source ip: got %q want %q", string(body), "203.0.113.10")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := builder.echo.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown server: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("server exited with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after shutdown")
	}
}

func waitForServer(t *testing.T, errCh <-chan error, url string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	return doRequestWithRetry(t, errCh, req)
}

func doRequestWithRetry(t *testing.T, errCh <-chan error, req *http.Request) *http.Response {
	t.Helper()

	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Fatalf("server failed before responding: %v", err)
			}
		default:
		}

		resp, err := client.Do(req.Clone(context.Background()))
		if err == nil {
			return resp
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("server did not become ready at %s", req.URL.String())
	return nil
}

func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate free port: %v", err)
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}
