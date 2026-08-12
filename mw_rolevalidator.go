package wshell

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/labstack/echo/v4"
)

const (
	rolesExtension  = "x-roles"
	scopesExtension = "x-scopes"
)

// RoleValidator enforces operation-level x-roles declarations in an OpenAPI
// specification. Authentication must run before this middleware and place the
// validated JWT roles in ContextKey_Roles.
//
// All roles listed by an operation are required. Operations without x-roles are
// left unchanged.
func RoleValidator(spec *openapi3.T) echo.MiddlewareFunc {
	return authorizationValidatorForBasePath("", spec, true, false)
}

func roleValidatorForBasePath(basePath string, spec *openapi3.T) echo.MiddlewareFunc {
	return authorizationValidatorForBasePath(basePath, spec, true, false)
}

// ScopeValidator enforces operation-level x-scopes declarations in an OpenAPI
// specification. Authentication must run before this middleware and place the
// validated JWT scopes in ContextKey_Scopes.
//
// All scopes listed by an operation are required. Operations without x-scopes
// are left unchanged.
func ScopeValidator(spec *openapi3.T) echo.MiddlewareFunc {
	return authorizationValidatorForBasePath("", spec, false, true)
}

func scopeValidatorForBasePath(basePath string, spec *openapi3.T) echo.MiddlewareFunc {
	return authorizationValidatorForBasePath(basePath, spec, false, true)
}

func authorizationValidatorForBasePath(
	basePath string,
	spec *openapi3.T,
	validateRoles bool,
	validateScopes bool,
) echo.MiddlewareFunc {
	if spec == nil || spec.Paths == nil {
		return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}

	// The router utility provides the same operation lookup semantics as the
	// OpenAPI request validator. It is created once when the middleware is set up,
	// not once per request.
	routerUtility, err := gorillamux.NewRouter(spec)
	if err != nil {
		panic(fmt.Errorf("create OpenAPI router for authorization validation: %w", err))
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !pathHasBasePath(c.Request().URL.Path, basePath) {
				return next(c)
			}

			route, _, err := routerUtility.FindRoute(c.Request())
			if err != nil || route == nil || route.Operation == nil {
				// Route lookup and its error response remain the responsibility of
				// the OpenAPI request validator.
				return next(c)
			}

			if validateRoles {
				if err := validateOperationRoles(c, route.Operation); err != nil {
					return err
				}
			}
			if validateScopes {
				if err := validateOperationScopes(c, route.Operation); err != nil {
					return err
				}
			}

			return next(c)
		}
	}
}

func validateOperationRoles(c echo.Context, operation *openapi3.Operation) error {
	rawRoles, configured := operation.Extensions[rolesExtension]
	if !configured {
		return nil
	}

	requiredRoles, err := rolesFromExtension(rawRoles)
	if err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"invalid x-roles configuration",
		).SetInternal(err)
	}

	userRoles, authenticatedWithJWT := rolesFromContext(c)
	if !authenticatedWithJWT {
		return echo.NewHTTPError(http.StatusUnauthorized, "JWT authentication required")
	}
	if !hasRequiredValues(userRoles, requiredRoles) {
		return echo.NewHTTPError(http.StatusForbidden, "insufficient role")
	}
	return nil
}

func validateOperationScopes(c echo.Context, operation *openapi3.Operation) error {
	rawScopes, configured := operation.Extensions[scopesExtension]
	if !configured {
		return nil
	}

	requiredScopes, err := scopesFromExtension(rawScopes)
	if err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"invalid x-scopes configuration",
		).SetInternal(err)
	}

	userScopes, authenticatedWithJWT := scopesFromContext(c)
	if !authenticatedWithJWT {
		return echo.NewHTTPError(http.StatusUnauthorized, "JWT authentication required")
	}
	if !hasRequiredValues(userScopes, requiredScopes) {
		return echo.NewHTTPError(http.StatusForbidden, "insufficient scope")
	}
	return nil
}

func validateRoleExtensions(spec *openapi3.T) error {
	return validateStringListExtension(spec, rolesExtension, rolesFromExtension)
}

func validateScopeExtensions(spec *openapi3.T) error {
	return validateStringListExtension(spec, scopesExtension, scopesFromExtension)
}

func validateStringListExtension(
	spec *openapi3.T,
	extension string,
	parse func(any) ([]string, error),
) error {
	if spec == nil || spec.Paths == nil {
		return nil
	}

	for path, pathItem := range spec.Paths.Map() {
		for method, operation := range pathItem.Operations() {
			rawValue, configured := operation.Extensions[extension]
			if !configured {
				continue
			}
			if _, err := parse(rawValue); err != nil {
				return fmt.Errorf("%s %s: %w", strings.ToUpper(method), path, err)
			}
		}
	}

	return nil
}

func pathHasBasePath(path, basePath string) bool {
	basePath = strings.TrimSuffix(basePath, "/")
	if basePath == "" {
		return true
	}
	return path == basePath || strings.HasPrefix(path, basePath+"/")
}

func rolesFromContext(c echo.Context) ([]string, bool) {
	value := c.Get(ContextKey_Roles)
	if value == nil {
		return nil, false
	}

	roles, err := stringsFromValue(value)
	if err != nil {
		return nil, true
	}
	return roles, true
}

func scopesFromContext(c echo.Context) ([]string, bool) {
	value := c.Get(ContextKey_Scopes)
	if value == nil {
		return nil, false
	}

	scopes, err := scopesFromValue(value)
	if err != nil {
		return nil, true
	}
	return scopes, true
}

func rolesFromExtension(raw any) ([]string, error) {
	return stringListFromExtension(raw, rolesExtension, "role", false)
}

func scopesFromExtension(raw any) ([]string, error) {
	return stringListFromExtension(raw, scopesExtension, "scope", true)
}

func stringListFromExtension(raw any, extension, itemName string, rejectWhitespace bool) ([]string, error) {
	if raw == nil {
		return nil, fmt.Errorf("%s must be a non-empty array of strings", extension)
	}

	var values []string
	switch value := raw.(type) {
	case []string:
		values = append([]string(nil), value...)
	case []any:
		var err error
		values, err = stringsFromValue(value)
		if err != nil {
			return nil, fmt.Errorf("%s must contain only strings", extension)
		}
	case json.RawMessage:
		if err := json.Unmarshal(value, &values); err != nil {
			return nil, fmt.Errorf("%s must be an array of strings", extension)
		}
	default:
		return nil, fmt.Errorf("%s must be an array of strings", extension)
	}

	if len(values) == 0 {
		return nil, fmt.Errorf("%s must contain at least one %s", extension, itemName)
	}
	for i, value := range values {
		values[i] = strings.TrimSpace(value)
		if values[i] == "" {
			return nil, fmt.Errorf("%s must not contain an empty %s", extension, itemName)
		}
		if rejectWhitespace && strings.ContainsAny(values[i], " \t\r\n") {
			return nil, fmt.Errorf("%s entries must contain exactly one scope", extension)
		}
	}

	return values, nil
}

func stringsFromValue(value any) ([]string, error) {
	switch values := value.(type) {
	case []string:
		return values, nil
	case []any:
		roles := make([]string, 0, len(values))
		for _, value := range values {
			role, ok := value.(string)
			if !ok {
				return nil, errors.New("value contains a non-string role")
			}
			roles = append(roles, role)
		}
		return roles, nil
	case string:
		return []string{values}, nil
	default:
		return nil, errors.New("roles value is not a string or an array of strings")
	}
}

func scopesFromValue(value any) ([]string, error) {
	values, err := stringsFromValue(value)
	if err != nil {
		return nil, err
	}

	scopes := make([]string, 0, len(values))
	for _, value := range values {
		scopes = append(scopes, strings.Fields(value)...)
	}
	return scopes, nil
}

func hasRequiredRole(userRoles, requiredRoles []string) bool {
	return hasRequiredValues(userRoles, requiredRoles)
}

func hasRequiredValues(actualValues, requiredValues []string) bool {
	valueSet := make(map[string]struct{}, len(actualValues))
	for _, value := range actualValues {
		value = strings.TrimSpace(value)
		if value != "" {
			valueSet[value] = struct{}{}
		}
	}

	for _, required := range requiredValues {
		if _, ok := valueSet[required]; !ok {
			return false
		}
	}
	return true
}
