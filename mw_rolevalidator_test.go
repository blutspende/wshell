package wshell

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
)

const roleTestSpec = `
openapi: 3.0.3
info:
  title: role test
  version: "1"
paths:
  /v1/widgets/{id}:
    get:
      x-roles:
        anyOf:
          - widget-reader
          - auditor
      x-scopes:
        - widgets:read
        - audit
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
    post:
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
  /v1/public:
    get:
      responses:
        "200":
          description: ok
  /v10/admin:
    get:
      x-roles:
        anyOf:
          - admin
      responses:
        "200":
          description: ok
`

func TestRoleValidator(t *testing.T) {
	spec := loadRoleTestSpec(t)

	tests := []struct {
		name       string
		method     string
		path       string
		roles      any
		setRoles   bool
		wantStatus int
		wantCalled bool
	}{
		{
			name:       "first anyOf role authorizes",
			path:       "/v1/widgets/42",
			roles:      []string{"widget-reader"},
			setRoles:   true,
			wantCalled: true,
		},
		{
			name:       "second anyOf role authorizes",
			path:       "/v1/widgets/42",
			roles:      []string{"auditor"},
			setRoles:   true,
			wantCalled: true,
		},
		{
			name:       "additional roles are allowed",
			path:       "/v1/widgets/42",
			roles:      []any{"admin", "auditor", "widget-reader"},
			setRoles:   true,
			wantCalled: true,
		},
		{
			name:       "none of the anyOf roles is present",
			path:       "/v1/widgets/42",
			roles:      []string{"unrelated-role"},
			setRoles:   true,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "authenticated JWT has no roles",
			path:       "/v1/widgets/42",
			roles:      []string{},
			setRoles:   true,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "JWT authentication is missing",
			path:       "/v1/widgets/42",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "operation without x-roles is unchanged",
			path:       "/v1/public",
			wantCalled: true,
		},
		{
			name:       "roles apply only to the annotated HTTP method",
			method:     http.MethodPost,
			path:       "/v1/widgets/42",
			wantCalled: true,
		},
		{
			name:       "similar path outside configured base path is unchanged",
			path:       "/v10/admin",
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tt.path, nil)
			ctx := e.NewContext(req, httptest.NewRecorder())
			if tt.setRoles {
				ctx.Set(ContextKey_Roles, tt.roles)
			}

			called := false
			handler := roleValidatorForBasePath("/v1", spec)(func(echo.Context) error {
				called = true
				return nil
			})
			err := handler(ctx)

			if called != tt.wantCalled {
				t.Fatalf("handler called: got %v want %v", called, tt.wantCalled)
			}
			if tt.wantStatus == 0 {
				if err != nil {
					t.Fatalf("unexpected middleware error: %v", err)
				}
				return
			}

			httpErr, ok := err.(*echo.HTTPError)
			if !ok {
				t.Fatalf("error type: got %T want *echo.HTTPError", err)
			}
			if httpErr.Code != tt.wantStatus {
				t.Fatalf("status: got %d want %d", httpErr.Code, tt.wantStatus)
			}
		})
	}
}

func TestScopeValidator(t *testing.T) {
	spec := loadRoleTestSpec(t)

	tests := []struct {
		name       string
		scopes     any
		setScopes  bool
		wantStatus int
		wantCalled bool
	}{
		{
			name:       "space-delimited scope claim contains every scope",
			scopes:     "openid widgets:read audit",
			setScopes:  true,
			wantCalled: true,
		},
		{
			name:       "array scope claim contains every scope",
			scopes:     []any{"audit", "widgets:read"},
			setScopes:  true,
			wantCalled: true,
		},
		{
			name:       "one required scope is missing",
			scopes:     "widgets:read",
			setScopes:  true,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "authenticated JWT has no scopes",
			scopes:     []string{},
			setScopes:  true,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "JWT authentication is missing",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			ctx := e.NewContext(
				httptest.NewRequest(http.MethodGet, "/v1/widgets/42", nil),
				httptest.NewRecorder(),
			)
			if tt.setScopes {
				ctx.Set(ContextKey_Scopes, tt.scopes)
			}

			called := false
			err := scopeValidatorForBasePath("/v1", spec)(func(echo.Context) error {
				called = true
				return nil
			})(ctx)

			if called != tt.wantCalled {
				t.Fatalf("handler called: got %v want %v", called, tt.wantCalled)
			}
			if tt.wantStatus == 0 {
				if err != nil {
					t.Fatalf("unexpected middleware error: %v", err)
				}
				return
			}

			httpErr, ok := err.(*echo.HTTPError)
			if !ok || httpErr.Code != tt.wantStatus {
				t.Fatalf("error: got %v want HTTP %d", err, tt.wantStatus)
			}
		})
	}
}

func TestCombinedRoleAndScopeValidator(t *testing.T) {
	spec := loadRoleTestSpec(t)
	e := echo.New()
	ctx := e.NewContext(
		httptest.NewRequest(http.MethodGet, "/v1/widgets/42", nil),
		httptest.NewRecorder(),
	)
	ctx.Set(ContextKey_Roles, []string{"widget-reader"})
	ctx.Set(ContextKey_Scopes, "widgets:read audit")

	called := false
	err := authorizationValidatorForBasePath("/v1", spec, true, true)(func(echo.Context) error {
		called = true
		return nil
	})(ctx)
	if err != nil || !called {
		t.Fatalf("combined authorization failed: called=%v err=%v", called, err)
	}
}

func TestRoleValidatorRejectsInvalidExtension(t *testing.T) {
	spec := loadRoleTestSpec(t)
	operation := spec.Paths.Find("/v1/widgets/{id}").Get
	operation.Extensions[rolesExtension] = map[string]any{
		"anyOf": []any{"widget-reader", 42},
	}

	if err := validateRoleExtensions(spec); err == nil {
		t.Fatal("expected invalid x-roles configuration to be rejected")
	}

	e := echo.New()
	ctx := e.NewContext(
		httptest.NewRequest(http.MethodGet, "/v1/widgets/42", nil),
		httptest.NewRecorder(),
	)
	ctx.Set(ContextKey_Roles, []string{"widget-reader"})
	err := RoleValidator(spec)(func(echo.Context) error { return nil })(ctx)

	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusInternalServerError {
		t.Fatalf("error: got %v want HTTP %d", err, http.StatusInternalServerError)
	}
}

func TestRunRejectsInvalidRoleConfigurationBeforeStartingServer(t *testing.T) {
	spec := loadRoleTestSpec(t)
	spec.Paths.Find("/v1/widgets/{id}").Get.Extensions[rolesExtension] = map[string]any{
		"anyOf": []any{},
	}
	builder := NewWebServer("role-test").WithRoleValidation(true)
	builder.specifications["invalid.yaml"] = &apiSpecification{
		basePath: "/v1",
		doc:      spec,
	}

	if err := builder.Run(0); err == nil {
		t.Fatal("expected Run to reject an empty x-roles declaration")
	}
}

func TestRunRejectsInvalidScopeConfigurationBeforeStartingServer(t *testing.T) {
	spec := loadRoleTestSpec(t)
	spec.Paths.Find("/v1/widgets/{id}").Get.Extensions[scopesExtension] = []any{"widgets:read audit"}
	builder := NewWebServer("scope-test").WithScopeValidation(true)
	builder.specifications["invalid.yaml"] = &apiSpecification{
		basePath: "/v1",
		doc:      spec,
	}

	if err := builder.Run(0); err == nil {
		t.Fatal("expected Run to reject an invalid x-scopes declaration")
	}
}

func TestRolesFromExtensionSupportsRawJSON(t *testing.T) {
	roles, err := rolesFromExtension(json.RawMessage(`{"anyOf":["reader","auditor"]}`))
	if err != nil {
		t.Fatalf("parse roles: %v", err)
	}
	if want := []string{"reader", "auditor"}; !reflect.DeepEqual(roles, want) {
		t.Fatalf("roles: got %v want %v", roles, want)
	}
}

func TestRolesFromExtensionRejectsAmbiguousList(t *testing.T) {
	if _, err := rolesFromExtension([]any{"reader", "auditor"}); err == nil {
		t.Fatal("expected x-roles without anyOf to be rejected")
	}
}

func TestExtractRoles(t *testing.T) {
	claims := jwt.MapClaims{
		"roles": []any{"application-reader", "shared"},
		"realm_access": map[string]any{
			"roles": []any{"realm-admin", "shared"},
		},
	}

	got := extractRoles(claims)
	want := []string{"application-reader", "shared", "realm-admin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("roles: got %v want %v", got, want)
	}
}

func TestExtractScopes(t *testing.T) {
	claims := jwt.MapClaims{
		"scope": "openid widgets:read shared",
		"scp":   []any{"audit", "shared"},
	}

	got := extractScopes(claims)
	want := []string{"openid", "widgets:read", "shared", "audit"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes: got %v want %v", got, want)
	}
}

func loadRoleTestSpec(t *testing.T) *openapi3.T {
	t.Helper()
	spec, err := openapi3.NewLoader().LoadFromData([]byte(roleTestSpec))
	if err != nil {
		t.Fatalf("load OpenAPI test specification: %v", err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Fatalf("validate OpenAPI test specification: %v", err)
	}
	return spec
}
