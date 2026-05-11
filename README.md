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