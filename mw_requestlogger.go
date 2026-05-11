package wshell

import (
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
)

func RequestLogger(baseLogger *zerolog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			lgr, _ := c.Get("logger").(*zerolog.Logger)
			if lgr == nil {
				lgr = baseLogger
			}
			req := c.Request()
			sourceIP, _ := c.Get(ContextKey_SourceIP).(string)

			lgr.Debug().
				Str("method", req.Method).
				Str("uri", req.RequestURI).
				Str("source_ip", sourceIP).
				Msg("http request")

			return next(c)
		}
	}
}
