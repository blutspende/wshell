# More complex example

This example combines an OpenAPI-generated Echo handler with OIDC
authentication, role and scope authorization, Redis-backed rate limiting,
Swagger UI, custom error responses, and a Zerolog adapter.

The generated package in the example is assumed to have been created from the
following specification with `oapi-codegen` or an equivalent generator.

## OpenAPI specification

Save this as `api/openapi.yaml`:

```yaml
openapi: 3.0.3
info:
  title: Patient API
  version: 1.0.0
paths:
  /api/v1/patients/{id}:
    get:
      operationId: getPatient
      security:
        - BearerAuth: []
      x-roles:
        anyOf:
          - patient-reader
          - patient-admin
      x-scopes:
        - patients:read
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: Patient found
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Patient"
        "401":
          description: Authentication required
        "403":
          description: Insufficient permissions
        "404":
          description: Patient not found
components:
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
  schemas:
    Patient:
      type: object
      required:
        - id
        - displayName
      properties:
        id:
          type: string
        displayName:
          type: string
```

## Application setup

Replace `example.com/patient-service/internal/api` with the package generated
from the specification. The exact generated handler method names can vary with
the generator configuration.

```go
package main

import (
    "net/http"
    "os"
    "time"

    "example.com/patient-service/internal/api"
    "github.com/blutspende/wshell"
    "github.com/labstack/echo/v4"
    "github.com/redis/go-redis/v9"
    "github.com/rs/zerolog"
)

type patientHandler struct{}

// GetPatient implements the operation generated from operationId getPatient.
func (patientHandler) GetPatient(c echo.Context, id string) error {
    email, _ := c.Get(wshell.ContextKey_UserEmail).(string)

    return c.JSON(http.StatusOK, api.Patient{
        Id:          id,
        DisplayName: "Example patient requested by " + email,
    })
}

type errorResponse struct {
    Status  int    `json:"status"`
    Message string `json:"message"`
}

func main() {
    // Redis makes the rate limit consistent across multiple service instances.
    redisClient := redis.NewClient(&redis.Options{
        Addr: envOrDefault("REDIS_ADDR", "localhost:6379"),
    })
    defer redisClient.Close()

    // The application configures Zerolog and uses wshell's default adapter.
    logger := zerolog.New(os.Stdout).
        With().
        Timestamp().
        Str("service", "patient-service").
        Logger()

    server := wshell.NewWebServer("patient-service").
        WithLoggerFunc(wshell.DefaultZerologAdapter(logger)).
        // Discover the JWKS endpoint and validate bearer tokens.
        WithAuthentication(envOrDefault(
            "OIDC_ISSUER",
            "https://identity.example.com/realms/patients",
        )).
        // Enforce operation-level x-roles and x-scopes declarations.
        WithRoleValidation(true).
        WithScopeValidation(true).
        // Permit 100 requests per source IP and minute using Redis.
        WithCache(redisClient).
        ThrottleRequests(100, time.Minute).
        // Configure browser access and expose the OpenAPI document in Swagger.
        AllowOrigins("https://portal.example.com").
        AllowHeaders("Authorization", "Content-Type", "X-API-Key").
        EnableSwagger(true).
        WithPrimarySpec("patient-api.yaml").
        UseOpenAPISpecs("/api/v1", "api/openapi.yaml", "patient-api.yaml").
        // Register the handlers generated from the OpenAPI document.
        RegisterHandler(func(e *echo.Echo) {
            api.RegisterHandlers(e, patientHandler{})
        }).
        // Use the application's response format for all HTTP errors.
        CustomErrorHandling(func(status int, message string, c echo.Context) {
            _ = c.JSON(status, errorResponse{Status: status, Message: message})
        })

    if err := server.Run(8080); err != nil {
        logger.Fatal().Err(err).Msg("patient service stopped")
    }
}

func envOrDefault(name, fallback string) string {
    if value := os.Getenv(name); value != "" {
        return value
    }
    return fallback
}

```

## Runtime behavior

With this configuration:

- OpenAPI validation applies to requests below `/api/v1`.
- The bearer token is verified using the configured OIDC provider.
- `GET /api/v1/patients/{id}` requires either the `patient-reader` or
  `patient-admin` role and additionally the `patients:read` scope.
- The caller receives HTTP 401 for missing authentication and HTTP 403 for
  missing permissions.
- Rate-limit counters are shared through Redis. If Redis is unavailable, the
  middleware falls back to its per-instance in-memory limiter.
- Swagger UI is available below `/swagger`.
- Wshell log events are emitted through the application's Zerolog instance.
- HTTP errors use the application's JSON error representation.
