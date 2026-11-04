package wshell

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
)

func requestBodyLimit(maxBytes int64) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			if req.ContentLength > maxBytes {
				return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "request body too large")
			}
			if req.Body != nil {
				req.Body = http.MaxBytesReader(c.Response(), req.Body, maxBytes)
			}
			err := next(c)
			var limitErr *http.MaxBytesError
			if errors.As(err, &limitErr) {
				return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "request body too large").SetInternal(err)
			}
			return err
		}
	}
}
