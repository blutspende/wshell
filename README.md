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

## Role-based authorization

Role validation can be enabled through the fluent builder API:

```go
server := wshell.NewWebServer("example").
    WithAuthentication("https://identity.example.com/realms/example").
    WithRoleValidation(true)
```

Add `x-roles` to an OpenAPI operation alongside its bearer-token security
requirement:

```yaml
paths:
  /patients/{id}:
    get:
      security:
        - BearerAuth: []
      x-roles:
        - patient-reader
        - auditor
```

All listed roles are required. Roles are read from either the top-level JWT
`roles` claim or Keycloak's `realm_access.roles` claim. A missing JWT produces
HTTP 401; an authenticated caller lacking a required role receives HTTP 403.
