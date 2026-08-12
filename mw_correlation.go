package wshell

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"time"

	"github.com/labstack/echo/v4"
)

type correlationContextKey struct{}

// CorrelationIDFromContext returns the correlation ID added to a request's Go
// context by CorrelationContextEnricher.
func CorrelationIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(correlationContextKey{}).(string)
	return value
}

// CorrelationContextEnricher copies the Echo correlation ID into the request's
// standard Go context so LoggerFunc implementations and downstream code can use
// it without depending on Echo.
func CorrelationContextEnricher() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cid, _ := c.Get(ContextKey_Correlation).(string)
			req := c.Request()
			ctx := context.WithValue(req.Context(), correlationContextKey{}, cid)
			c.SetRequest(req.WithContext(ctx))

			return next(c)
		}
	}
}

// SetCorrelationID provides a middleware that ensures every response carries
// an X-CORRELATION-ID header. Precedence: CF-RAY > X-CORRELATION-ID > generated.
func SetCorrelationID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			headers := req.Header

			var cid string
			if v := headers.Get("CF-RAY"); v != "" {
				cid = v
			} else if v := headers.Get("X-CORRELATION-ID"); v != "" {
				cid = v
			} else {
				cid = newCorrelationID()
			}

			// Expose in context and response header
			c.Set(ContextKey_Correlation, cid)
			c.Response().Header().Set("X-CORRELATION-ID", cid)

			if err := next(c); err != nil {
				c.Response().Header().Set("X-CORRELATION-ID", cid)
				return err
			}
			c.Response().Header().Set("X-CORRELATION-ID", cid)
			return nil
		}
	}
}

func newCorrelationID() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err == nil {
		return hex.EncodeToString(b)
	}
	return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
}
