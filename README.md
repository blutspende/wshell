# wshell

`wshell` is a lightweight Go library for building OpenAPI-driven web services
with a fluent API. It combines Echo with request validation, authentication,
role- and scope-based authorization, configurable logging, and common service
middleware while keeping application setup concise.

Install with

```shell
go get github.com/blutspende/wshell@latest
```

## Example of use

```go
package main

import (
    "log"
    "net/http"

    "github.com/blutspende/wshell"
    "github.com/labstack/echo/v4"
)

func main() {
    server := wshell.NewWebServer("example").
        // Load the contract used to validate requests below /api/v1.
        UseOpenAPISpecs("/api/v1", "openapi.yaml", "api.yaml").
        // Register an application endpoint on the underlying Echo server.
        CustomGET("/api/v1/hello", func(c echo.Context) error {
            return c.JSON(http.StatusOK, map[string]string{"message": "hello"})
        }).
        // Make the loaded specification available through Swagger UI.
        EnableSwagger(true)

    // Assemble the middleware stack and start serving requests.
    if err := server.Run(8080); err != nil {
        log.Fatal(err)
    }
}
```

For authentication, generated handlers, authorization, rate limiting, custom
logging, and error responses, see the
[more complex example](docs/more-complex-example.md).

## API and configuration

The following sections document the builder functions and request context
values exposed by wshell.

### Request context

Authenticated caller and request information can be read directly inside an
Echo handler:

```go
func (h *handler) Handle(ctx echo.Context) error {
    userEmail, _ := ctx.Get(wshell.ContextKey_UserEmail).(string)
    return nil
}
```

Available context keys:

- `ContextKey_UserID`
- `ContextKey_UserEmail`
- `ContextKey_GivenName`
- `ContextKey_FamilyName`
- `ContextKey_EmailVerified`
- `ContextKey_Roles`
- `ContextKey_Scopes`
- `ContextKey_SourceIP`
- `ContextKey_Correlation`

### Swagger UI

Load one or more OpenAPI documents and enable Swagger UI through the builder.
`WithPrimarySpec` selects the document shown first when multiple specifications
are registered:

```go
server := wshell.NewWebServer("example").
    UseOpenAPISpecs("/api/v1", "openapi.yaml", "api.yaml").
    WithPrimarySpec("api.yaml").
    EnableSwagger(true)
```

After the server starts, Swagger UI is available at `/swagger`.

### Request throttling

`ThrottleRequests` limits requests per resolved source IP within a fixed time
window. Supplying a Redis client stores counters centrally:

```go
redisClient := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

server := wshell.NewWebServer("example").
    WithCache(redisClient).
    ThrottleRequests(100, time.Minute)
```

Without Redis, or while Redis is unavailable, wshell falls back to an in-memory
counter. This is suitable only for a single application instance. Redis is a
mandatory prerequisite for horizontally scaled deployments because otherwise
each instance maintains an independent limit. Requests exceeding the limit
receive HTTP 429.

### Source IP

The source-IP middleware is enabled automatically by `Run` and was designed for
services deployed behind Cloudflare. The selected address is normalized and
stored under `ContextKey_SourceIP`.

Resolution uses this precedence:

1. `CF-Connecting-IP`
2. The last address in `X-Forwarded-For`
3. The direct peer address from `Request.RemoteAddr`

```go
sourceIP, _ := ctx.Get(wshell.ContextKey_SourceIP).(string)
```

Only trust forwarding headers when direct access to the service is restricted
to trusted proxies. During tests, set `CF-Connecting-IP`, `X-Forwarded-For`, or
`Request.RemoteAddr` explicitly depending on the fallback being tested. When no
header is set, `httptest.NewRequest` normally yields its local test peer address.

### Jaeger tracing

Wshell uses OpenTelemetry for tracing. The application configures an
OpenTelemetry provider and exports spans to Jaeger's OTLP gRPC endpoint; calling
`WithOpenTelemetry` then instruments HTTP requests:

```go
ctx := context.Background()
exporter, err := otlptracegrpc.New(
    ctx,
    otlptracegrpc.WithEndpoint("localhost:4317"),
    otlptracegrpc.WithInsecure(),
)
if err != nil {
    log.Fatal(err)
}

provider := sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(exporter),
    sdktrace.WithResource(resource.NewWithAttributes(
        semconv.SchemaURL,
        semconv.ServiceName("example"),
    )),
)
otel.SetTracerProvider(provider)
defer provider.Shutdown(context.Background())

server := wshell.NewWebServer("example").WithOpenTelemetry()
```

This expects Jaeger to accept OTLP/gRPC on `localhost:4317`. The exporter and
SDK are configured by the application and are intentionally not dependencies
of wshell.

### Custom error handling

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

### Logging

By default, wshell writes structured events through Go's `log.Default()`.
Applications can connect another logger with `WithLoggerFunc`. Every event
contains its level, message, timestamp, context, structured fields, original
error, deepest wrapped cause, caller information, and a runtime stack for error
events.

Wshell includes `DefaultZerologAdapter`, which maps all of this information to a
supplied Zerolog logger:

```go
logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

server := wshell.NewWebServer("example").
    WithLoggerFunc(wshell.DefaultZerologAdapter(logger))
```

A custom application adapter can wrap the default adapter to enrich every
event without reimplementing its error and runtime mapping:

```go
func applicationLogAdapter(logger zerolog.Logger) wshell.LoggerFunc {
    defaultAdapter := wshell.DefaultZerologAdapter(logger)

    return func(entry wshell.LogEvent) {
        fields := make(map[string]any, len(entry.Fields)+1)
        for key, value := range entry.Fields {
            fields[key] = value
        }
        fields["application"] = "patient-service"
        entry.Fields = fields

        defaultAdapter(entry)
    }
}

server := wshell.NewWebServer("example").
    WithLoggerFunc(applicationLogAdapter(logger))
```

### Role- and scope-based authorization

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
        anyOf:
          - patient-reader
          - auditor
      x-scopes:
        - patients:read
        - audit
```

Every role listed under `x-roles.anyOf` independently authorizes the operation;
the caller needs at least one of them. Roles are read from either the top-level
JWT `roles` claim or Keycloak's `realm_access.roles` claim. A missing JWT
produces HTTP 401; an authenticated caller without any listed role receives
HTTP 403.

Scopes work analogously through `x-scopes`; all listed scopes are required.
They are read from the JWT `scope` or `scp` claim. Both space-delimited strings
and string arrays are supported. Missing scopes result in HTTP 403.
