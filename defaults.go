package wshell

import (
	"fmt"

	"github.com/rs/zerolog"
)

// DefaultZerologAdapter maps wshell log events to the supplied Zerolog logger.
// It preserves the event context, timestamp, structured fields, original error,
// deepest cause, caller, and runtime stack.
func DefaultZerologAdapter(logger zerolog.Logger) LoggerFunc {
	return func(entry LogEvent) {
		level := zerolog.InfoLevel
		switch entry.Level {
		case LogLevelTrace:
			level = zerolog.TraceLevel
		case LogLevelDebug:
			level = zerolog.DebugLevel
		case LogLevelWarn:
			level = zerolog.WarnLevel
		case LogLevelError:
			level = zerolog.ErrorLevel
		}

		event := logger.WithLevel(level).
			Ctx(entry.Context).
			Time("event_time", entry.Time)

		for key, value := range entry.Fields {
			event.Interface(key, value)
		}
		if entry.Runtime.Caller.File != "" {
			event.Str("caller", fmt.Sprintf(
				"%s:%d",
				entry.Runtime.Caller.File,
				entry.Runtime.Caller.Line,
			))
		}
		if entry.Err != nil {
			event.Err(entry.Err)
		}
		if entry.Cause != nil {
			event.Str("cause", entry.Cause.Error())
		}
		if len(entry.Runtime.Stack) > 0 {
			stack := make([]string, 0, len(entry.Runtime.Stack))
			for _, frame := range entry.Runtime.Stack {
				stack = append(stack, fmt.Sprintf(
					"%s:%d %s",
					frame.File,
					frame.Line,
					frame.Function,
				))
			}
			event.Strs("stack", stack)
		}

		event.Msg(entry.Message)
	}
}
