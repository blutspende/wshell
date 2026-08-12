package wshell

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestStartIsNonBlockingAndShutdownIsGraceful(t *testing.T) {
	builder := NewWebServer("lifecycle-test").
		WithLoggerFunc(func(LogEvent) {}).
		CustomGET("/healthz", func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})

	running, err := builder.Start(0)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}

	tcpAddress, ok := running.Address().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address type: got %T want *net.TCPAddr", running.Address())
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", tcpAddress.Port)
	response := waitForRunningServer(t, running, url)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", response.StatusCode, http.StatusOK)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := running.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown server: %v", err)
	}
	if err := running.Wait(); err != nil {
		t.Fatalf("wait after graceful shutdown: %v", err)
	}

	select {
	case <-running.Done():
	default:
		t.Fatal("Done was not closed after shutdown")
	}
}

func TestStartReturnsPortBindingFailureSynchronously(t *testing.T) {
	first, err := NewWebServer("first").WithLoggerFunc(func(LogEvent) {}).Start(0)
	if err != nil {
		t.Fatalf("start first server: %v", err)
	}
	defer first.Close()

	port := first.Address().(*net.TCPAddr).Port
	second, err := NewWebServer("second").WithLoggerFunc(func(LogEvent) {}).Start(port)
	if err == nil {
		if second != nil {
			_ = second.Close()
		}
		t.Fatal("expected port binding failure")
	}
	if second != nil {
		t.Fatal("binding failure returned a running server")
	}
	if !strings.Contains(err.Error(), "listen on") {
		t.Fatalf("binding error: got %v", err)
	}
}

func TestBuilderCanOnlyBeStartedOnce(t *testing.T) {
	builder := NewWebServer("single-start").WithLoggerFunc(func(LogEvent) {})
	running, err := builder.Start(0)
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	defer running.Close()

	if second, err := builder.Start(0); err == nil || second != nil {
		t.Fatalf("second start: server=%v err=%v", second, err)
	}
}

func TestRunningServerReportsUnexpectedServeError(t *testing.T) {
	want := errors.New("serve failed")
	running := newLifecycleTestServer(func() error { return want })
	go running.run()

	err := running.Wait()
	if !errors.Is(err, want) {
		t.Fatalf("wait error: got %v want wrapped %v", err, want)
	}
}

func TestRunningServerConvertsServePanicToError(t *testing.T) {
	running := newLifecycleTestServer(func() error { panic("boom") })
	go running.run()

	err := running.Wait()
	if err == nil || !strings.Contains(err.Error(), "HTTP server panic: boom") {
		t.Fatalf("panic error: got %v", err)
	}
}

type lifecycleTestEcho struct{}

func (lifecycleTestEcho) Shutdown(context.Context) error { return nil }
func (lifecycleTestEcho) Close() error                   { return nil }

func newLifecycleTestServer(serve func() error) *RunningServer {
	return &RunningServer{
		echo:   lifecycleTestEcho{},
		done:   make(chan struct{}),
		serve:  serve,
		logger: func(LogEvent) {},
	}
}

func waitForRunningServer(t *testing.T, running *RunningServer, url string) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-running.Done():
			t.Fatalf("server stopped before responding: %v", running.Wait())
		default:
		}

		response, err := client.Get(url)
		if err == nil {
			return response
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("server did not become ready at %s", url)
	return nil
}
