# AGENTS.md

## Scope

These instructions apply to the entire repository.

## Project overview

`wshell` is a Go library for building OpenAPI-driven HTTP services with a
fluent API. It is based on Echo and provides reusable middleware for request
validation, authentication, authorization, observability, and other common
service concerns.

Read [docs/requirements.md](docs/requirements.md) before changing externally
visible behavior. It is the source of truth for the project's functional and
security requirements.

## Development workflow

- Inspect the affected code and tests before changing behavior.
- Preserve unrelated changes already present in the working tree.
- Keep changes focused on the requested outcome.
- Prefer established project patterns over introducing new abstractions.
- Avoid unnecessary runtime dependencies.
- Update public documentation when behavior or configuration changes.
- Do not commit, publish, or tag changes unless explicitly requested.

## Go conventions

- Use the Go version declared in `go.mod`.
- Format changed Go files with `gofmt`.
- Follow normal Go naming and documentation conventions.
- Add documentation comments for new exported symbols.
- Return errors instead of panicking during request processing.
- Keep exported APIs backward compatible where practical.
- Builder methods must preserve fluent chaining by returning `*APIBuilder`.
- Use the exported `ContextKey_*` constants instead of duplicating their string
  values.

## Architecture guidelines

- `wshell.go` owns builder state, middleware assembly, and server startup.
- `wshell_builder.go` contains the public fluent configuration methods.
- `mw_validator_and_auth.go` handles OpenAPI validation and authentication.
- `mw_rolevalidator.go` handles OpenAPI-driven authorization.
- Other `mw_*.go` files contain focused cross-cutting middleware.
- Keep authentication, authorization, and schema validation conceptually
  separate, even when they share routing infrastructure.
- Construct routers, clients, compiled schemas, and similar reusable objects at
  setup time rather than once per request.
- Respect middleware ordering and the boundaries of each registered OpenAPI
  specification.
- Return `*echo.HTTPError` for request failures so custom error handling remains
  effective.
- Minimize the use external libraries
- Consider the builder to be consistent and user-friendly

## Security guidelines

- Treat authentication and authorization changes as security-sensitive.
- Default to denying access when required security information is absent or
  malformed.
- Never authorize from an unverified token or caller-controlled context data.
- Never log bearer tokens, API keys, complete JWT claims, or other credentials.
- Validate security-related configuration before opening the server listener
  when possible.
- Preserve the status-code and matching semantics documented in
  `docs/requirements.md`.

## Testing and validation

For Go changes, run:

```shell
gofmt -w <changed-go-files>
go test ./...
go vet ./...
git diff --check
```

Prefer table-driven tests, `httptest`, and in-memory OpenAPI documents. Unit
tests should be deterministic and should not depend on external services.

On restricted Windows environments, the default Go build cache may be
unwritable. A temporary cache can be used without modifying the repository:

```powershell
$env:GOCACHE = Join-Path $env:TEMP "wshell-go-build"
go test ./...
go vet ./...
```

Tests for routing or security behavior should normally cover successful access,
missing credentials, insufficient permissions, malformed configuration,
parameterized paths, HTTP-method selection, and specification/base-path
boundaries.

## Documentation

- `README.md` explains the public API and common usage.
- `docs/requirements.md` records behavioral requirements and security
  contracts.
- `AGENTS.md` contains repository-wide working instructions only.

Keep these documents consistent, but avoid copying detailed requirements into
this file.
