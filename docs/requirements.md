# Project requirements

## Purpose

`wshell` provides a fluent Go API for exposing HTTP services whose requests are
validated and secured from OpenAPI 3 specifications. Applications should be
able to register generated Echo handlers and opt into common service features
without assembling the complete middleware stack themselves.

## Public API

- Configuration is exposed through chainable `APIBuilder` methods.
- Existing exported names and context-key values are compatibility contracts.
- Applications can register generated Echo handlers and custom middleware.
- Applications can configure custom error responses.
- Public behavior and configuration examples are documented in `README.md`.

## OpenAPI support

- Requests are matched by HTTP method and OpenAPI path template.
- Parameterized paths such as `/patients/{id}` are supported.
- Request parameters and bodies are validated against their specification.
- Multiple OpenAPI specifications with different base paths can coexist.
- A middleware registered for `/v1` must not process `/v10`.
- A request must never be authenticated or authorized using an operation from a
  different specification.
- Unknown routes remain the responsibility of the OpenAPI request validator.
- Swagger UI can expose the configured specifications when enabled.
- CORS preflight requests retain the configured CORS behavior.
- XML request validation remains skipped for the `/fhir` base path.

## Authentication

### JWT bearer authentication

- OIDC configuration is resolved through the provider's discovery document.
- JWT signatures and token validity are checked against the provider's JWKS.
- Claims are used only after successful token validation.
- The authenticated identity is made available through exported Echo context
  keys.
- Invalid or missing bearer credentials on a secured operation result in HTTP
  401.

### API-key authentication

- API keys are read from `X-API-Key` and validated through the configured custom
  callback.
- The callback receives the detected source IP.
- A valid API key establishes application identity but does not implicitly
  provide JWT roles or scopes.
- Specifications may offer bearer and API-key schemes as alternatives.

### JWT claim mapping

The following role claims are supported:

- top-level `roles`
- Keycloak `realm_access.roles`

The following scope claims are supported:

- `scope`, including a space-delimited string
- `scp`, including a space-delimited string
- string arrays for either claim

Duplicate roles and scopes are removed. Extracted values are available through
`ContextKey_Roles` and `ContextKey_Scopes`.

## Operation authorization

Role and scope validation are independently enabled through:

```go
builder.
    WithRoleValidation(true).
    WithScopeValidation(true)
```

When both features are enabled, they share one OpenAPI operation lookup per
request and the caller must satisfy both requirements.

### `x-roles`

An operation can require roles as follows:

```yaml
get:
  security:
    - BearerAuth: []
  x-roles:
    - patient-reader
    - auditor
```

- `x-roles` must be a non-empty array of non-empty strings.
- Every listed role is required (AND semantics).
- Additional caller roles are allowed.
- Matching is exact and case-sensitive after trimming surrounding whitespace.
- Operations without `x-roles` have no role restriction.
- Missing validated JWT role context results in HTTP 401.
- An authenticated JWT caller missing a required role receives HTTP 403.
- Invalid declarations fail server startup when role validation is enabled.

### `x-scopes`

An operation can require scopes as follows:

```yaml
get:
  security:
    - BearerAuth: []
  x-scopes:
    - patients:read
    - audit
```

- `x-scopes` must be a non-empty array of non-empty strings.
- Every array entry represents exactly one scope and must not contain
  whitespace.
- Every listed scope is required (AND semantics).
- Additional caller scopes are allowed.
- Matching is exact and case-sensitive.
- Operations without `x-scopes` have no scope restriction.
- Missing validated JWT scope context results in HTTP 401.
- An authenticated JWT caller missing a required scope receives HTTP 403.
- Invalid declarations fail server startup when scope validation is enabled.

The exported `RoleValidator` and `ScopeValidator` middleware remain available
for direct use outside the builder.

## Request context

The library exposes authenticated and request-related information using these
context keys:

- `ContextKey_UserID`
- `ContextKey_UserEmail`
- `ContextKey_GivenName`
- `ContextKey_FamilyName`
- `ContextKey_EmailVerified`
- `ContextKey_Roles`
- `ContextKey_Scopes`
- `ContextKey_SourceIP`
- `ContextKey_Correlation`

## Cross-cutting service behavior

- Requests receive or retain a correlation ID, and the response exposes it.
- Source-IP detection prefers the supported proxy headers and stores a
  canonical IP value in the Echo context.
- Request logging includes available correlation and source information without
  exposing credentials.
- Rate limiting can use Redis or the existing in-memory behavior.
- Prometheus metrics and OpenTelemetry instrumentation are optional.
- Configured CORS origins, methods, and headers are honored.
- Merge-patch binding preserves its specialized request semantics.
- Custom error handlers receive validation, authentication, authorization, and
  application HTTP errors before a response is committed.

## Quality requirements

- Security configuration must fail closed rather than silently disabling a
  check.
- Reusable routers and clients are created during middleware setup, not per
  request.
- Tests do not require live identity providers, Redis instances, or other
  external infrastructure unless explicitly categorized as integration tests.
- Authentication and authorization changes include tests for success, HTTP 401,
  HTTP 403, routing boundaries, malformed configuration, and supported claim
  shapes.
- The complete test suite and static analysis must pass before release.
