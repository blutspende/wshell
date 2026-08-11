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

const rolesExtension = "x-roles"

// RoleValidator enforces operation-level x-roles declarations in an OpenAPI
// specification. Authentication must run before this middleware and place the
// validated JWT roles in ContextKey_Roles.
//
// All roles listed by an operation are required. Operations without x-roles are
// left unchanged.
func RoleValidator(spec *openapi3.T) echo.MiddlewareFunc {
	return roleValidatorForBasePath("", spec)
}

func roleValidatorForBasePath(basePath string, spec *openapi3.T) echo.MiddlewareFunc {
	if spec == nil || spec.Paths == nil {
		return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}

	// the routerUtility has a convinient "Findroutes function" that can be used to find the operation for a given request
	routerUtility, err := gorillamux.NewRouter(spec)
	if err != nil {
		panic(fmt.Errorf("create OpenAPI router for role validation: %w", err))
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

			rawRoles, configured := route.Operation.Extensions[rolesExtension]
			if !configured {
				return next(c)
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
			if !hasRequiredRole(userRoles, requiredRoles) {
				return echo.NewHTTPError(http.StatusForbidden, "insufficient role")
			}

			return next(c)
		}
	}
}

func validateRoleExtensions(spec *openapi3.T) error {
	if spec == nil || spec.Paths == nil {
		return nil
	}

	for path, pathItem := range spec.Paths.Map() {
		for method, operation := range pathItem.Operations() {
			rawRoles, configured := operation.Extensions[rolesExtension]
			if !configured {
				continue
			}
			if _, err := rolesFromExtension(rawRoles); err != nil {
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

func rolesFromExtension(raw any) ([]string, error) {
	if raw == nil {
		return nil, errors.New("x-roles must be a non-empty array of strings")
	}

	var roles []string
	switch value := raw.(type) {
	case []string:
		roles = append([]string(nil), value...)
	case []any:
		var err error
		roles, err = stringsFromValue(value)
		if err != nil {
			return nil, errors.New("x-roles must contain only strings")
		}
	case json.RawMessage:
		if err := json.Unmarshal(value, &roles); err != nil {
			return nil, errors.New("x-roles must be an array of strings")
		}
	default:
		return nil, errors.New("x-roles must be an array of strings")
	}

	if len(roles) == 0 {
		return nil, errors.New("x-roles must contain at least one role")
	}
	for i, role := range roles {
		roles[i] = strings.TrimSpace(role)
		if roles[i] == "" {
			return nil, errors.New("x-roles must not contain an empty role")
		}
	}

	return roles, nil
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

func hasRequiredRole(userRoles, requiredRoles []string) bool {
	roleSet := make(map[string]struct{}, len(userRoles))
	for _, role := range userRoles {
		role = strings.TrimSpace(role)
		if role != "" {
			roleSet[role] = struct{}{}
		}
	}

	for _, required := range requiredRoles {
		if _, ok := roleSet[required]; !ok {
			return false
		}
	}
	return true
}
