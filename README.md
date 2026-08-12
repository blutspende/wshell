# wshell



Install with
```shell
go get github.com/blutspende/wshell@latest
```

Usage:
```go
type exampleHandler struct{    
}

func (e *exampleHandler)hello(ctx echo.ctx) {
    ctx.Json()
}

err = wshell.NewWebServer('testapp').
        RegisterHandler(func(e *echo.Echo) {
			v1.RegisterHandlers(e, &exampleHandler{})				
```


# Features

## Getting request information from the context

You can get information about the caller from the context

Example:
```userEmail, _ := ctx.Get(wshell.ContextKey_UserEmail).(string)```

  * ContextKey_UserID       
  * ContextKey_UserEmail    
  * ContextKey_GivenName    
  * ContextKey_FamilyName   
  * ContextKey_EmailVerified
  * ContextKey_Roles        
  * ContextKey_Scopes
  * ContextKey_SourceIP     
  * ContextKey_Correlation  

## Custom error handling

When using a custom error format, you can use the CustomErrorHandling from the builder to apply your own error-formats. This also catches
the validation errors returned by the api validation.

Example:
```
  w := wshell.NewWebserver("example").
      CustomErrorHandling(func(status int, message string, ctx echo.Context) {			

			_ = ctx.JSON(status, map[string]string{
				"Subcode": "418",
				"Message": message,
			}).
      ...
      Run(8080)
		}
```

## Logging

By default, wshell writes structured events through Go's `log.Default()`.
Applications can connect any logging library with `WithLoggerFunc`. Every event
contains its level, message, timestamp, context, structured fields, original
error, deepest wrapped cause, caller information, and a runtime stack for error
events.

For example, an application can adapt events to Zerolog without making Zerolog
a dependency of wshell:

```go
package main

import (
    "fmt"

    "github.com/blutspende/wshell"
    "github.com/rs/zerolog"
)

func zerologAdapter(logger zerolog.Logger) wshell.LoggerFunc {
    return func(entry wshell.LogEvent) {
        level := zerolog.InfoLevel
        switch entry.Level {
        case wshell.LogLevelTrace:
            level = zerolog.TraceLevel
        case wshell.LogLevelDebug:
            level = zerolog.DebugLevel
        case wshell.LogLevelWarn:
            level = zerolog.WarnLevel
        case wshell.LogLevelError:
            level = zerolog.ErrorLevel
        }

        event := logger.WithLevel(level).
            Ctx(entry.Context).
            Time("time", entry.Time)

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

func buildServer(logger zerolog.Logger) *wshell.APIBuilder {
    return wshell.NewWebServer("example").
        WithLoggerFunc(zerologAdapter(logger))
}
```

## Role- and scope-based authorization

Role and scope validation can be enabled independently through the fluent
builder API:

```go
server := wshell.NewWebServer("example").
    WithAuthentication("https://identity.example.com/realms/example").
    WithRoleValidation(true).
    WithScopeValidation(true)
```

Add `x-roles` and/or `x-scopes` to an OpenAPI operation alongside its
bearer-token security requirement:

```yaml
paths:
  /patients/{id}:
    get:
      security:
        - BearerAuth: []
      x-roles:
        - patient-reader
        - auditor
      x-scopes:
        - patients:read
        - audit
```

All listed roles are required. Roles are read from either the top-level JWT
`roles` claim or Keycloak's `realm_access.roles` claim. A missing JWT produces
HTTP 401; an authenticated caller lacking a required role receives HTTP 403.

Scopes work analogously through `x-scopes`; all listed scopes are required.
They are read from the JWT `scope` or `scp` claim. Both space-delimited strings
and string arrays are supported. Missing scopes result in HTTP 403.
