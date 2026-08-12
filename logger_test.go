package wshell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestLoggerFuncReceivesErrorDiagnostics(t *testing.T) {
	root := errors.New("root cause")
	wrapped := fmt.Errorf("outer failure: %w", root)
	ctx := context.WithValue(context.Background(), struct{}{}, "context value")

	var got LogEvent
	emitLog(func(event LogEvent) {
		got = event
	}, ctx, LogLevelError, "operation failed", wrapped, map[string]any{"answer": 42})

	if got.Level != LogLevelError || got.Message != "operation failed" {
		t.Fatalf("unexpected event metadata: %#v", got)
	}
	if got.Context != ctx {
		t.Fatal("event did not retain the supplied context")
	}
	if !errors.Is(got.Err, root) {
		t.Fatalf("event error does not retain its chain: %v", got.Err)
	}
	if got.Cause != root {
		t.Fatalf("cause: got %v want %v", got.Cause, root)
	}
	if got.Fields["answer"] != 42 {
		t.Fatalf("fields: got %v", got.Fields)
	}
	if got.Runtime.Caller.File == "" || got.Runtime.Caller.Line == 0 {
		t.Fatalf("missing caller information: %#v", got.Runtime.Caller)
	}
	if len(got.Runtime.Stack) == 0 {
		t.Fatal("missing runtime stack for error event")
	}
	if got.Time.IsZero() {
		t.Fatal("missing event timestamp")
	}
}

func TestLoggerFuncGetsIndependentFields(t *testing.T) {
	fields := map[string]any{"key": "before"}
	var got LogEvent
	emitLog(func(event LogEvent) {
		got = event
	}, nil, LogLevelInfo, "message", nil, fields)
	fields["key"] = "after"

	if got.Fields["key"] != "before" {
		t.Fatalf("logger fields changed with caller map: %v", got.Fields)
	}
	if len(got.Runtime.Stack) != 0 {
		t.Fatalf("non-error event unexpectedly captured a stack: %v", got.Runtime.Stack)
	}
}

func TestSystemLoggerIncludesStructuredErrorInformation(t *testing.T) {
	var output bytes.Buffer
	logger := log.New(&output, "", 0)
	root := errors.New("root")
	event := LogEvent{
		Level:   LogLevelError,
		Message: "failed",
		Err:     fmt.Errorf("wrapped: %w", root),
		Cause:   root,
		Fields:  map[string]any{"request_id": "abc"},
		Runtime: LogRuntimeInfo{
			Caller: runtime.Frame{File: "caller.go", Line: 12, Function: "caller"},
			Stack: []runtime.Frame{
				{File: "caller.go", Line: 12, Function: "caller"},
			},
		},
	}

	writeSystemLog(logger, event)
	for _, expected := range []string{
		`level=error`,
		`msg="failed"`,
		`error="wrapped: root"`,
		`cause="root"`,
		`request_id="abc"`,
		`caller.go:12`,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("system log %q does not contain %q", output.String(), expected)
		}
	}
}

func TestRequestLoggerProvidesRequestMetadata(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/patients/42?token=secret", nil)
	ctx := e.NewContext(req, httptest.NewRecorder())
	ctx.Set(ContextKey_SourceIP, "192.0.2.1")
	ctx.Set(ContextKey_Correlation, "correlation-1")

	var got LogEvent
	err := RequestLogger(func(event LogEvent) {
		got = event
	})(func(echo.Context) error { return nil })(ctx)
	if err != nil {
		t.Fatalf("request logger: %v", err)
	}

	if got.Fields["method"] != http.MethodGet || got.Fields["path"] != "/patients/42" {
		t.Fatalf("request fields: %v", got.Fields)
	}
	if strings.Contains(fmt.Sprint(got.Fields), "secret") {
		t.Fatalf("request logger exposed query data: %v", got.Fields)
	}
	if got.Fields[ContextKey_SourceIP] != "192.0.2.1" {
		t.Fatalf("source IP missing: %v", got.Fields)
	}
	if got.Fields[ContextKey_Correlation] != "correlation-1" {
		t.Fatalf("correlation ID missing: %v", got.Fields)
	}
}

func TestCorrelationContextEnricher(t *testing.T) {
	e := echo.New()
	ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	ctx.Set(ContextKey_Correlation, "correlation-1")

	err := CorrelationContextEnricher()(func(c echo.Context) error {
		if got := CorrelationIDFromContext(c.Request().Context()); got != "correlation-1" {
			t.Fatalf("correlation ID: got %q", got)
		}
		return nil
	})(ctx)
	if err != nil {
		t.Fatalf("context enricher: %v", err)
	}
}

func TestWithLoggerFunc(t *testing.T) {
	builder := NewWebServer("logger-test")
	called := false
	builder.WithLoggerFunc(func(LogEvent) { called = true })
	builder.logger(LogEvent{})
	if !called {
		t.Fatal("custom logger was not configured")
	}

	// A nil callback restores a usable default instead of disabling logging.
	builder.WithLoggerFunc(nil)
	if builder.logger == nil {
		t.Fatal("nil logger callback disabled logging")
	}
}
