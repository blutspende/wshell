package wshell

import (
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func TracingMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		req := c.Request()

		cid := req.Header.Get("X-Correlation-ID")
		if cid == "" {
			cid = uuid.New().String()
		}

		ctx, span := otel.Tracer("http").
			Start(req.Context(), req.Method+" "+req.URL.Path)

		span.SetAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.path", req.URL.Path),
			attribute.String("correlation.id", cid),
			attribute.String("component", "api"),
			attribute.String("service.layer", "http"),
		)

		c.SetRequest(req.WithContext(ctx))
		defer span.End()

		return next(c)
	}
}
