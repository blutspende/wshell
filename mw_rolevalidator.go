package wshell

// Middleware to enforce role-based access control based on OpenAPI spec extensions
// Usage:
// Add  x-roles to the specification

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
)

func RoleValidator(spec *openapi3.T) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			operation := whatOperation(c, spec)
			if operation == nil {
				return next(c)
			}

			// Rollen aus Spec
			roles := operation.Extensions["x-roles"]
			if roles == nil {
				return next(c)
			}

			userRoles := rolesFromContext(c)
			requiredRoles := rolesFromExtension(roles)
			if !hasRequiredRole(userRoles, requiredRoles) {
				return echo.NewHTTPError(http.StatusForbidden, "insufficient role")
			}

			return next(c)
		}
	}
}

func whatOperation(c echo.Context, spec *openapi3.T) *openapi3.Operation {
	if spec == nil || spec.Paths == nil {
		return nil
	}

	pathTemplate := normalizePath(c.Path())
	if pathTemplate != "" {
		if pathItem := spec.Paths.Find(pathTemplate); pathItem != nil {
			if op := operationForMethod(pathItem, c.Request().Method); op != nil {
				return op
			}
		}
	}

	for specPath, pathItem := range spec.Paths.Map() {
		echoPattern := specPathToEcho(specPath)
		if echoPattern == "" {
			continue
		}
		if echoPattern == c.Path() || strings.HasSuffix(c.Path(), echoPattern) {
			if op := operationForMethod(pathItem, c.Request().Method); op != nil {
				return op
			}
		}
	}

	return nil
}

func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	var b strings.Builder
	for i := 0; i < len(path); i++ {
		ch := path[i]
		if ch == ':' {
			start := i + 1
			end := start
			for end < len(path) && path[end] != '/' {
				end++
			}
			if start == end {
				continue
			}
			b.WriteByte('{')
			b.WriteString(path[start:end])
			b.WriteByte('}')
			i = end - 1
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func specPathToEcho(path string) string {
	if path == "" {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		ch := path[i]
		if ch == '{' {
			start := i + 1
			end := start
			for end < len(path) && path[end] != '}' {
				end++
			}
			if end >= len(path) {
				break
			}
			b.WriteByte(':')
			b.WriteString(path[start:end])
			i = end
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func operationForMethod(pathItem *openapi3.PathItem, method string) *openapi3.Operation {
	if pathItem == nil {
		return nil
	}

	switch strings.ToUpper(method) {
	case http.MethodGet:
		return pathItem.Get
	case http.MethodPost:
		return pathItem.Post
	case http.MethodPut:
		return pathItem.Put
	case http.MethodPatch:
		return pathItem.Patch
	case http.MethodDelete:
		return pathItem.Delete
	case http.MethodOptions:
		return pathItem.Options
	case http.MethodHead:
		return pathItem.Head
	case http.MethodTrace:
		return pathItem.Trace
	default:
		return nil
	}
}

func rolesFromContext(c echo.Context) []string {
	val := c.Get("roles")
	switch roles := val.(type) {
	case nil:
		return nil
	case []string:
		return roles
	case []interface{}:
		out := make([]string, 0, len(roles))
		for _, r := range roles {
			if rs, ok := r.(string); ok {
				out = append(out, rs)
			}
		}
		return out
	case string:
		return []string{roles}
	default:
		return nil
	}
}

func rolesFromExtension(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		return convertInterfaceSliceToStrings(v)
	case json.RawMessage:
		var roles []string
		if err := json.Unmarshal(v, &roles); err == nil {
			return roles
		}
		return nil
	default:
		if s, ok := v.(string); ok {
			return []string{s}
		}
		return nil
	}
}

func convertInterfaceSliceToStrings(values []interface{}) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if str, ok := value.(string); ok {
			result = append(result, str)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func hasRequiredRole(userRoles, requiredRoles []string) bool {
	if len(requiredRoles) == 0 {
		return true
	}
	if len(userRoles) == 0 {
		return false
	}

	roleSet := make(map[string]struct{}, len(userRoles))
	for _, role := range userRoles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		roleSet[role] = struct{}{}
	}

	for _, required := range requiredRoles {
		required = strings.TrimSpace(required)
		if required == "" {
			continue
		}
		if _, ok := roleSet[required]; !ok {
			return false
		}
	}

	return true
}
