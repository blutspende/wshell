package wshell

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	echo4mw "github.com/labstack/echo/v4/middleware"
	"github.com/oasdiff/yaml"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	echoSwagger "github.com/swaggo/echo-swagger"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type OapiRegisterFunc func(*echo.Echo)

type CustomErrorHandler (func(status int, message string, ctx echo.Context))
type CustomApiAuthentication func(ctx echo.Context, apitoken, sourceIP string) (email string, firstname string, lastname string, valid bool, apiError interface{})

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
	enableRolevalidation  bool
	logger                zerolog.Logger
	servers               *[]string
	oidcBaseURL           *string
	request_threshold     int
	request_window        time.Duration
	allow_origins         []string
	allow_methods         []string
	additionalMiddlewares []echo.MiddlewareFunc
	applicationName       string
	specifications        map[string]*apiSpecification
	primarySpecification  string
	CustomAPITokenChecker CustomApiAuthentication
	customErrorHandler    CustomErrorHandler
}

func NewWebServer(appName string) *APIBuilder {
	builder := &APIBuilder{
		echo:             echo.New(),
		enablePrometheus: true,
		enableSwagger:    true,
		//enableOpenTelemetry: false,
		//	oapi:                  []oapidescriptor{},
		request_threshold:    -1,
		request_window:       0,
		allow_origins:        []string{"*"},
		allow_methods:        []string{echo.GET, echo.POST, echo.PUT, echo.PATCH, echo.DELETE, echo.OPTIONS},
		enableRolevalidation: false,
		applicationName:      appName,
		specifications:       make(map[string]*apiSpecification),
	}
	builder.echo.HideBanner = true
	builder.echo.Binder = &MergePatchBinder{Binder: builder.echo.Binder}
	builder.logger = zerolog.Nop() // dont log by default
	return builder
}

func (b *APIBuilder) Run(listenPort int) error {

	//-- fundamental middlewares
	b.echo.Use(SetCorrelationID())
	b.echo.Use(SourceIP())
	b.echo.Use(CorrelationContextEnricher(&b.logger))
	b.echo.Use(RequestLogger(&b.logger))
	if b.request_threshold > 0 {
		b.echo.Use(RateLimiter(b.redisCache, b.request_threshold, b.request_window))
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
		AllowOrigins:     b.allow_origins,
		AllowMethods:     b.allow_methods,
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-API-Key"},
		AllowCredentials: true,
	}))

	//-- register specifications
	for key, spec := range b.specifications {

		//-- if swagger is enabled, serve specification as .yaml and .json
		if b.enableSwagger {
			//-- encode as json
			jsonData, err := spec.doc.MarshalJSON()
			if err != nil {
				b.logger.Error().Err(err).Msg("Failed to marshal OpenAPI spec to JSON")
				return err
			}
			//-- encode as yaml
			yamlInterface, err := spec.doc.MarshalYAML()
			if err != nil {
				b.logger.Error().Err(err).Msg("Failed to marshal OpenAPI spec to YAML")
				return err
			}
			yamlData, err := yaml.Marshal(yamlInterface)
			if err != nil {
				b.logger.Error().Err(err).Msg("Failed to convert YAML interface to bytes")
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

			b.echo.Use(AuthMiddleware(spec.basePath, spec.doc, b.oidcBaseURL, b.CustomAPITokenChecker))
			b.echo.Use(ValidatorMiddleware(spec.basePath, spec.doc, xmlSkipper))

			// Check x-role annotations if enabled
			if b.enableRolevalidation {
				b.echo.Use(RoleValidator(spec.doc))
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
				return
			}

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
				return
			}

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

	return b.echo.Start(fmt.Sprintf(":%d", listenPort))
}
