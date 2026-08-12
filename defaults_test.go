package wshell

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestDefaultZerologAdapter(t *testing.T) {
	var output bytes.Buffer
	logger := zerolog.New(&output).Level(zerolog.TraceLevel)
	root := errors.New("root cause")
	eventTime := time.Date(2026, time.August, 12, 10, 30, 0, 0, time.UTC)

	DefaultZerologAdapter(logger)(LogEvent{
		Time:    eventTime,
		Context: context.Background(),
		Level:   LogLevelError,
		Message: "request failed",
		Err:     fmt.Errorf("wrapped: %w", root),
		Cause:   root,
		Fields: map[string]any{
			"status": 500,
		},
		Runtime: LogRuntimeInfo{
			Caller: runtime.Frame{File: "handler.go", Line: 42},
			Stack: []runtime.Frame{
				{File: "handler.go", Line: 42, Function: "example.Handle"},
			},
		},
	})

	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("decode Zerolog output %q: %v", output.String(), err)
	}

	assertJSONField(t, got, zerolog.LevelFieldName, "error")
	assertJSONField(t, got, zerolog.MessageFieldName, "request failed")
	assertJSONField(t, got, zerolog.ErrorFieldName, "wrapped: root cause")
	assertJSONField(t, got, "cause", "root cause")
	assertJSONField(t, got, "caller", "handler.go:42")
	assertJSONField(t, got, "event_time", eventTime.Format(time.RFC3339))
	if got["status"] != float64(500) {
		t.Fatalf("status: got %#v want 500", got["status"])
	}
	stack, ok := got["stack"].([]any)
	if !ok || len(stack) != 1 || stack[0] != "handler.go:42 example.Handle" {
		t.Fatalf("stack: got %#v", got["stack"])
	}
}

func assertJSONField(t *testing.T, fields map[string]any, key string, want any) {
	t.Helper()
	if got := fields[key]; got != want {
		t.Fatalf("%s: got %#v want %#v", key, got, want)
	}
}
