package wshell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/oapi-codegen/echo-middleware"
	"github.com/rs/zerolog/log"
)

type SkipperFunc func(echo.Context) bool

var errAuthSkipped = errors.New("authentication skipped due to alternate scheme")

// AuthMiddleware handles authentication (JWT Bearer and API Key)
func AuthMiddleware(basePath string, spec *openapi3.T, oidcBaseURL *string,
	customAPITokenChecker CustomApiAuthentication) echo.MiddlewareFunc {

	var jwks *keyfunc.JWKS
	if oidcBaseURL != nil {
		jwksURL, err := resolveJWKSURL(*oidcBaseURL)
		if err != nil {
			log.Fatal().Err(err).Msgf("Failed to resolve JWKS endpoint from %s", *oidcBaseURL)
		}
		jwks, err = keyfunc.Get(jwksURL, keyfunc.Options{
			RefreshInterval:   time.Hour,
			RefreshUnknownKID: true,
		})
		if err != nil || jwks == nil {
			log.Fatal().Err(err).Msgf("Failed to initialize JWKS from %s", jwksURL)
		}
	}

	spec.Servers = nil

	return echomiddleware.OapiRequestValidatorWithOptions(spec, &echomiddleware.Options{
		Options: openapi3filter.Options{
			// Auth middleware must not mutate or validate request params/body.
			ExcludeRequestBody:          true,
			ExcludeRequestQueryParams:   true,
			ExcludeResponseBody:         true,
			ExcludeReadOnlyValidations:  true,
			ExcludeWriteOnlyValidations: true,
			SkipSettingDefaults:         true,
			AuthenticationFunc: func(ctx context.Context, input *openapi3filter.AuthenticationInput) error {

				// Skip authentication if no security requirements are defined
				if len(input.SecurityScheme.Type) == 0 {
					log.Debug().Msg("No security requirements defined, skipping authentication")
					return nil
				}

				if oidcBaseURL == nil {
					log.Debug().Msg("No OIDC configuration found, rejecting requests. No further info provided in the response for security reasons.")
					return echo.NewHTTPError(http.StatusUnauthorized, "")
				}

				echoCtx := echomiddleware.GetEchoContext(ctx)
				if echoCtx == nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "missing request context")
				}

				if echoCtx.Get(ContextKey_UserEmail) != nil {
					return nil
				}

				apiToken := echoCtx.Request().Header.Get("X-API-Key")
				auth := echoCtx.Request().Header.Get("Authorization")
				hasBearer := strings.HasPrefix(auth, "Bearer ")

				switch input.SecurityScheme.Type {
				case "apiKey":
					if apiToken == "" || customAPITokenChecker == nil {
						// if the API token is missing but there's a Bearer token and an alternative scheme that supports it, skip API token authentication
						if apiToken == "" && hasBearer && hasAlternativeScheme(input, "BearerAuth") {
							return errAuthSkipped
						}
						return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
					}

					//-- for API token validation, also provide the source IP address
					sourceIP := ""
					if echoCtx.Get(ContextKey_SourceIP) != nil {
						sourceIP = echoCtx.Get(ContextKey_SourceIP).(string)
					}
					email, firstname, lastname, valid, apiError := customAPITokenChecker(echoCtx, apiToken, sourceIP)
					if !valid {
						if apiError != nil {
							return echo.NewHTTPError(http.StatusUnauthorized, apiError)
						}
						return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
					}

					log.Trace().Msgf("API token authentication successful for email: %s", email)
					echoCtx.Set(ContextKey_UserEmail, email)
					echoCtx.Set(ContextKey_GivenName, firstname)
					echoCtx.Set(ContextKey_FamilyName, lastname)
					return nil

				case "http":
					// if there's an API token header but the current scheme doesn't support it, skip authentication to allow the next scheme to handle it
					if apiToken != "" && hasAlternativeScheme(input, "ApiKeyAuth") {
						return errAuthSkipped
					}
					if !hasBearer {
						return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
					}

					tokenStr := strings.TrimPrefix(auth, "Bearer ")
					token, err := jwt.Parse(tokenStr, jwks.Keyfunc)
					if err != nil || token == nil || !token.Valid {
						return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
					}

					claims, ok := token.Claims.(jwt.MapClaims)
					if !ok {
						return echo.NewHTTPError(http.StatusUnauthorized, "invalid token claims")
					}

					setUserContextFromClaims(echoCtx, claims)
					return nil
				default:
					return echo.NewHTTPError(http.StatusUnauthorized, "unsupported authentication scheme")
				}
			},
		},
		Skipper: func(c echo.Context) bool {
			// Only skip if not within the basePath
			if !strings.HasPrefix(c.Path(), basePath) {
				return true
			}
			return false
		},
	})
}

// ValidatorMiddleware handles OpenAPI schema validation
func ValidatorMiddleware(basePath string, spec *openapi3.T, skipper SkipperFunc) echo.MiddlewareFunc {
	spec.Servers = nil

	return echomiddleware.OapiRequestValidatorWithOptions(spec, &echomiddleware.Options{
		Options: openapi3filter.Options{
			// Validation middleware owns the full request schema pass.
			AuthenticationFunc: func(ctx context.Context, input *openapi3filter.AuthenticationInput) error {
				// Skip authentication - it's already been done
				return nil
			},
		},
		Skipper: func(c echo.Context) bool {
			if !strings.HasPrefix(c.Path(), basePath) {
				return true
			}

			if skipper != nil && skipper(c) {
				return true
			}

			return false
		},
		SilenceServersWarning: true,
	})
}

type oidcDiscovery struct {
	JWKSURI string `json:"jwks_uri"`
}

func resolveJWKSURL(base string) (string, error) {
	if base == "" {
		return "", errors.New("missing OIDC base URL")
	}

	discovery := strings.TrimSuffix(base, "/")
	if !strings.Contains(discovery, "/.well-known/") {
		discovery = fmt.Sprintf("%s/.well-known/openid-configuration", discovery)
	}

	req, err := http.NewRequest(http.MethodGet, discovery, nil)
	if err != nil {
		return "", fmt.Errorf("build discovery request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch discovery document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovery endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read discovery document: %w", err)
	}

	var doc oidcDiscovery
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("decode discovery document: %w", err)
	}
	if doc.JWKSURI == "" {
		return "", errors.New("discovery document missing jwks_uri")
	}
	return doc.JWKSURI, nil
}

func hasAlternativeScheme(input *openapi3filter.AuthenticationInput, altScheme string) bool {
	if input == nil || input.RequestValidationInput == nil || input.RequestValidationInput.Route == nil {
		return false
	}

	route := input.RequestValidationInput.Route
	var requirements openapi3.SecurityRequirements
	if route.Operation != nil && route.Operation.Security != nil {
		requirements = *route.Operation.Security
	} else if route.Spec != nil {
		requirements = route.Spec.Security
	}
	if len(requirements) == 0 {
		return false
	}

	current := input.SecuritySchemeName
	for _, req := range requirements {
		if _, ok := req[altScheme]; !ok {
			continue
		}
		if _, hasCurrent := req[current]; hasCurrent {
			continue
		}
		return true
	}
	return false
}

func setUserContextFromClaims(c echo.Context, claims jwt.MapClaims) {

	if sub, ok := claims["sub"].(string); ok {
		uid, _ := uuid.Parse(sub)
		c.Set(ContextKey_UserID, uid)
	}
	if email, ok := claims["email"].(string); ok {
		c.Set(ContextKey_UserEmail, email)
	}
	if given, ok := claims["given_name"].(string); ok {
		c.Set(ContextKey_GivenName, given)
	}
	if family, ok := claims["family_name"].(string); ok {
		c.Set(ContextKey_FamilyName, family)
	}
	if verified, ok := claims["email_verified"].(bool); ok {
		c.Set(ContextKey_EmailVerified, verified)
	}

	c.Set(ContextKey_Roles, extractRoles(claims))
}

func extractRoles(claims jwt.MapClaims) []string {
	realmAccess, ok := claims["realm_access"].(map[string]any)
	if !ok {
		return nil
	}

	rawRoles, ok := realmAccess["roles"].([]any)
	if !ok {
		return nil
	}

	roles := make([]string, 0, len(rawRoles))
	for _, role := range rawRoles {
		if s, ok := role.(string); ok {
			roles = append(roles, s)
		}
	}
	return roles
}
