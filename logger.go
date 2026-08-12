package wshell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"sort"
	"strings"
	"time"
)

// LogLevel describes the severity of a LogEvent.
type LogLevel string

const (
	LogLevelTrace LogLevel = "trace"
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// LogRuntimeInfo identifies where a log event originated. Stack contains the
// runtime stack at the logging call for events carrying an error.
type LogRuntimeInfo struct {
	Caller runtime.Frame
	Stack  []runtime.Frame
}

// LogEvent contains all information passed to a LoggerFunc. Err retains the
// original error chain while Cause contains its deepest single wrapped cause.
type LogEvent struct {
	Time    time.Time
	Context context.Context
	Level   LogLevel
	Message string
	Err     error
	Cause   error
	Fields  map[string]any
	Runtime LogRuntimeInfo
}

// LoggerFunc receives structured log events produced by wshell. Implementations
// should return promptly and must be safe for concurrent use.
type LoggerFunc func(LogEvent)

// SystemLoggerFunc writes a LogEvent through the standard library's default
// logger. It is the logger used by new APIBuilder instances.
func SystemLoggerFunc(event LogEvent) {
	writeSystemLog(log.Default(), event)
}

func emitLog(logger LoggerFunc, ctx context.Context, level LogLevel, message string, err error, fields map[string]any) {
	if logger == nil {
		logger = SystemLoggerFunc
	}
	if ctx == nil {
		ctx = context.Background()
	}

	logger(LogEvent{
		Time:    time.Now(),
		Context: ctx,
		Level:   level,
		Message: message,
		Err:     err,
		Cause:   deepestCause(err),
		Fields:  cloneLogFields(fields),
		Runtime: captureLogRuntime(err != nil),
	})
}

func captureLogRuntime(withStack bool) LogRuntimeInfo {
	depth := 1
	if withStack {
		depth = 32
	}
	pcs := make([]uintptr, depth)
	// Skip runtime.Callers, captureLogRuntime, and emitLog.
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])

	result := LogRuntimeInfo{}
	for {
		frame, more := frames.Next()
		if result.Caller.File == "" {
			result.Caller = frame
		}
		if withStack {
			result.Stack = append(result.Stack, frame)
		}
		if !more {
			break
		}
	}
	return result
}

func deepestCause(err error) error {
	if err == nil {
		return nil
	}

	cause := errors.Unwrap(err)
	if cause == nil {
		return nil
	}
	for i := 0; i < 100; i++ {
		next := errors.Unwrap(cause)
		if next == nil || next == cause {
			return cause
		}
		cause = next
	}
	return cause
}

func cloneLogFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(fields))
	for key, value := range fields {
		cloned[key] = value
	}
	return cloned
}

type systemLogWriter interface {
	Println(v ...any)
}

func writeSystemLog(logger systemLogWriter, event LogEvent) {
	var message strings.Builder
	message.WriteString("level=")
	message.WriteString(string(event.Level))
	message.WriteString(" msg=")
	message.WriteString(fmt.Sprintf("%q", event.Message))

	if caller := event.Runtime.Caller; caller.File != "" {
		message.WriteString(" caller=")
		message.WriteString(fmt.Sprintf("%q", fmt.Sprintf("%s:%d", caller.File, caller.Line)))
	}
	if event.Err != nil {
		message.WriteString(" error=")
		message.WriteString(fmt.Sprintf("%q", event.Err.Error()))
	}
	if event.Cause != nil {
		message.WriteString(" cause=")
		message.WriteString(fmt.Sprintf("%q", event.Cause.Error()))
	}

	keys := make([]string, 0, len(event.Fields))
	for key := range event.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		message.WriteByte(' ')
		message.WriteString(key)
		message.WriteByte('=')
		message.WriteString(fmt.Sprintf("%q", fmt.Sprint(event.Fields[key])))
	}

	if len(event.Runtime.Stack) > 0 {
		message.WriteString(" stack=")
		message.WriteString(fmt.Sprintf("%q", formatLogStack(event.Runtime.Stack)))
	}

	logger.Println(message.String())
}

func formatLogStack(frames []runtime.Frame) string {
	lines := make([]string, 0, len(frames))
	for _, frame := range frames {
		lines = append(lines, fmt.Sprintf("%s:%d %s", frame.File, frame.Line, frame.Function))
	}
	return strings.Join(lines, "\n")
}
