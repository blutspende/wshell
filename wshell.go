package wshell

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	echo4mw "github.com/labstack/echo/v4/middleware"
	"github.com/oasdiff/yaml"
	"github.com/redis/go-redis/v9"
	echoSwagger "github.com/swaggo/echo-swagger"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type OapiRegisterFunc func(*echo.Echo)

type CustomErrorHandler (func(status int, message string, ctx echo.Context))
type CustomApiAuthentication func(ctx echo.Context, apitoken, sourceIP string) (email string, firstname string, lastname string, valid bool, err error)

type apiSpecification struct {
	basePath string
	doc      *openapi3.T
}

type APIBuilder struct {
	echo                  *echo.Echo
	redisCache            *redis.Client
	enablePrometheus      bool
	enableSwagger         bool
	enableOpenTelemetry   bool
	enableRoleValidation  bool
	enableScopeValidation bool
	logger                LoggerFunc
	servers               *[]string
	oidcBaseURL           *string
	requestThreshold      int
	requestWindow         time.Duration
	allowOrigins          []string
	allowMethods          []string
	allowHeaders          []string
	additionalMiddlewares []echo.MiddlewareFunc
	applicationName       string
	specifications        map[string]*apiSpecification
	primarySpecification  string
	CustomAPITokenChecker CustomApiAuthentication
	customErrorHandler    CustomErrorHandler
	lifecycleMu           sync.Mutex
	startAttempted        bool
}

func NewWebServer(appName string) *APIBuilder {
	builder := &APIBuilder{
		echo:             echo.New(),
		enablePrometheus: false,
		enableSwagger:    false,
		//enableOpenTelemetry: false,
		//	oapi:                  []oapidescriptor{},
		requestThreshold:      -1,
		requestWindow:         0,
		allowOrigins:          []string{"*"},
		allowMethods:          []string{echo.GET, echo.POST, echo.PUT, echo.PATCH, echo.DELETE, echo.OPTIONS},
		allowHeaders:          []string{"Authorization", "Content-Type", "X-API-Key"},
		enableRoleValidation:  false,
		enableScopeValidation: false,
		applicationName:       appName,
		specifications:        make(map[string]*apiSpecification),
	}
	builder.echo.HideBanner = true
	builder.echo.Binder = &MergePatchBinder{Binder: builder.echo.Binder}
	builder.logger = SystemLoggerFunc
	return builder
}

func (b *APIBuilder) prepare() error {
	if b.enableRoleValidation || b.enableScopeValidation {
		for name, spec := range b.specifications {
			if b.enableRoleValidation {
				if err := validateRoleExtensions(spec.doc); err != nil {
					return fmt.Errorf("invalid role configuration in OpenAPI specification %q: %w", name, err)
				}
			}
			if b.enableScopeValidation {
				if err := validateScopeExtensions(spec.doc); err != nil {
					return fmt.Errorf("invalid scope configuration in OpenAPI specification %q: %w", name, err)
				}
			}
		}
	}

	//-- fundamental middlewares
	b.echo.Use(SetCorrelationID())
	b.echo.Use(SourceIP())
	b.echo.Use(CorrelationContextEnricher())
	b.echo.Use(RequestLogger(b.logger))
	if b.requestThreshold > 0 {
		b.echo.Use(RateLimiter(b.redisCache, b.requestThreshold, b.requestWindow))
	}

	//-- use opentelemetry
	if b.enableOpenTelemetry {
		b.echo.Use(otelecho.Middleware(b.applicationName,
			otelecho.WithMeterProvider(otel.GetMeterProvider()),
			otelecho.WithTracerProvider(otel.GetTracerProvider()),
		))

		b.echo.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {

				// -- prefer upstream Correlation-ID
				if cid := c.Request().Header.Get("X-Correlation-Id"); cid != "" {
					c.Set("correlation_id", cid)
					c.Response().Header().Set("X-Correlation-Id", cid)
					return next(c)
				}

				// -- Fallback: TraceID from OTel context
				span := trace.SpanFromContext(c.Request().Context())
				if sc := span.SpanContext(); sc.IsValid() {
					cid := sc.TraceID().String()
					c.Set("correlation_id", cid)
					c.Response().Header().Set("X-Correlation-Id", cid)
				}

				return next(c)
			}
		})
	}

	//-- load OpenAPI spec and convert to JSON
	if b.enableSwagger {

		//-- redirect swagger to index (convenient for users)
		b.echo.GET("/swagger", func(c echo.Context) error {
			c.Redirect(302, "/swagger/index.html")
			return nil
		})

		//-- make /swagger path catch everything below it
		//-- and provide multiple specs in swagger-ui
		b.echo.GET("/swagger/*", func(c echo.Context) error {
			if c.Request().URL.RawQuery != "" {
				return c.Redirect(http.StatusFound, "/swagger")
			}
			return echoSwagger.EchoWrapHandler(func(cfg *echoSwagger.Config) {
				urls := make([]string, 0, len(b.specifications))

				if _, ok := b.specifications[b.primarySpecification]; ok {
					urls = append(urls, b.primarySpecification)
				}

				keys := make([]string, 0, len(b.specifications))
				for k := range b.specifications {
					if k != b.primarySpecification {
						keys = append(keys, k)
					}
				}
				sort.Strings(keys)
				urls = append(urls, keys...)

				cfg.URLs = urls
			})(c)
		})
	}

	//-- CORS middleware must run before OpenAPI validation so browser preflight
	//-- OPTIONS requests are answered by CORS instead of being rejected
	b.echo.Use(echo4mw.CORSWithConfig(echo4mw.CORSConfig{
		AllowOrigins:     b.allowOrigins,
		AllowMethods:     b.allowMethods,
		AllowHeaders:     b.allowHeaders,
		AllowCredentials: true,
	}))

	//-- register specifications
	for key, spec := range b.specifications {

		//-- if swagger is enabled, serve specification as .yaml and .json
		if b.enableSwagger {
			//-- encode as json
			jsonData, err := spec.doc.MarshalJSON()
			if err != nil {
				emitLog(b.logger, nil, LogLevelError, "failed to marshal OpenAPI spec to JSON", err, map[string]any{
					"specification": key,
				})
				return err
			}
			//-- encode as yaml
			yamlInterface, err := spec.doc.MarshalYAML()
			if err != nil {
				emitLog(b.logger, nil, LogLevelError, "failed to marshal OpenAPI spec to YAML", err, map[string]any{
					"specification": key,
				})
				return err
			}
			yamlData, err := yaml.Marshal(yamlInterface)
			if err != nil {
				emitLog(b.logger, nil, LogLevelError, "failed to convert OpenAPI YAML", err, map[string]any{
					"specification": key,
				})
				return err
			}
			b.echo.GET("swagger/"+key, func(c echo.Context) error {
				return c.Blob(http.StatusOK, "application/yaml", yamlData)
			})
			b.echo.GET("swagger/"+strings.Trim(key, ".yaml")+".json", func(c echo.Context) error {
				return c.Blob(http.StatusOK, "application/json", jsonData)
			})
		}

		//-- Add validation schema
		if spec.basePath != "" {
			var xmlSkipper SkipperFunc

			if spec.basePath == "/fhir" {
				xmlSkipper = func(c echo.Context) bool {
					ct := c.Request().Header.Get("Content-Type")
					mt, _, _ := mime.ParseMediaType(ct)
					if mt == "application/xml" || mt == "text/xml" || strings.HasSuffix(mt, "+xml") {
						return true
					}
					return false
				}
			}

			authMiddleware, err := newAuthMiddleware(
				spec.basePath,
				spec.doc,
				b.oidcBaseURL,
				b.CustomAPITokenChecker,
				b.logger,
			)
			if err != nil {
				return fmt.Errorf("configure authentication for OpenAPI specification %q: %w", key, err)
			}
			b.echo.Use(authMiddleware)
			b.echo.Use(ValidatorMiddleware(spec.basePath, spec.doc, xmlSkipper))

			// Roles and scopes share one operation lookup, even when both are enabled.
			if b.enableRoleValidation || b.enableScopeValidation {
				b.echo.Use(authorizationValidatorForBasePath(
					spec.basePath,
					spec.doc,
					b.enableRoleValidation,
					b.enableScopeValidation,
				))
			}
		}
	}

	//-- Prometheus endpoint
	if b.enablePrometheus {
		b.echo.GET("/metrics", echoprometheus.NewHandler())
	}

	for _, mw := range b.additionalMiddlewares {
		b.echo.Use(mw)
	}

	//-- custom error handler
	if b.customErrorHandler != nil {
		b.echo.HTTPErrorHandler = func(err error, c echo.Context) {

			//-- create only a response if no response has been sent yet, otherwise just log the error
			if c.Response().Committed {
				b.logHTTPError(err, c)
				return
			}
			b.logHTTPError(err, c)

			status := http.StatusInternalServerError
			message := "Internal Server Error"
			var he *echo.HTTPError
			if errors.As(err, &he) {
				status = he.Code
				message = fmt.Sprintf("%v", he.Message)
			}
			b.customErrorHandler(status, message, c)
		}
	} else {
		b.echo.HTTPErrorHandler = func(err error, c echo.Context) {

			//-- create only a response if no response has been sent yet, otherwise just log the error
			if c.Response().Committed {
				b.logHTTPError(err, c)
				return
			}
			b.logHTTPError(err, c)

			status := http.StatusInternalServerError
			var he *echo.HTTPError
			if errors.As(err, &he) {
				status = he.Code
			}

			//-- Error messages are returned with status if not internal
			if status != http.StatusInternalServerError {
				c.String(status, err.Error())
			} else {
				c.String(status, "Internal Server Error")
			}
		}
	}

	return nil
}

func (b *APIBuilder) logHTTPError(err error, c echo.Context) {
	status := http.StatusInternalServerError
	var httpError *echo.HTTPError
	if errors.As(err, &httpError) {
		status = httpError.Code
	}

	level := LogLevelWarn
	if status >= http.StatusInternalServerError {
		level = LogLevelError
	}

	req := c.Request()
	emitLog(b.logger, req.Context(), level, "http request failed", err, map[string]any{
		"method":               req.Method,
		"path":                 req.URL.Path,
		"status":               status,
		ContextKey_SourceIP:    c.Get(ContextKey_SourceIP),
		ContextKey_Correlation: c.Get(ContextKey_Correlation),
	})
}
