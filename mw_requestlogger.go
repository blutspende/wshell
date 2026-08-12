package wshell

import "github.com/labstack/echo/v4"

// RequestLogger logs basic request metadata without headers or credentials.
func RequestLogger(logger LoggerFunc) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			sourceIP, _ := c.Get(ContextKey_SourceIP).(string)
			correlationID, _ := c.Get(ContextKey_Correlation).(string)

			emitLog(logger, req.Context(), LogLevelDebug, "http request", nil, map[string]any{
				"method":               req.Method,
				"path":                 req.URL.Path,
				ContextKey_SourceIP:    sourceIP,
				ContextKey_Correlation: correlationID,
			})

			return next(c)
		}
	}
}
