# wshell

Webservice Basis for our projects

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


# Cool features

Get SourceIp, Username from context

  * ContextKey_UserID       
  * ContextKey_UserEmail    
  * ContextKey_GivenName    
  * ContextKey_FamilyName   
  * ContextKey_EmailVerified
  * ContextKey_Roles        
  * ContextKey_SourceIP     
  * ContextKey_Correlation  

Example
```userEmail, _ := ctx.Get(wshell.ContextKey_UserEmail).(string)```
