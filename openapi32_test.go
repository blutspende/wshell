package wshell

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func openAPI32Document(method string) string {
	operation := `{
		"x-public": true,
		"x-roles": {"anyOf": ["reader"]},
		"x-scopes": ["widgets:read"],
		"parameters": [{"name":"id","in":"path","required":true,"schema":{"type":"integer"}}],
		"requestBody": {"required":true,"content":{"application/json":{"schema":{
			"type":"object","properties":{"kind":{"const":"widget"}},"required":["kind"]
		}}}},
		"responses":{"200":{"description":"ok"}}
	}`
	entry := `"query":` + operation
	if method != "QUERY" {
		entry = `"additionalOperations":{` + fmt.Sprintf("%q", method) + `:` + operation + `}`
	}
	return `{"openapi":"3.2.0","info":{"title":"test","version":"1"},"paths":{
		"/v1/widgets/{id}":{` + entry + `},
		"/v1/private":{"get":{"responses":{"200":{"description":"ok"}}}}
	}}`
}

func writeOpenAPITestFile(t *testing.T, document string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenAPI32Requests(t *testing.T) {
	for _, method := range []string{"QUERY", "CUSTOM"} {
		t.Run(method, func(t *testing.T) {
			builder := NewWebServer("openapi32").UseOpenAPISpecs("/v1", writeOpenAPITestFile(t, openAPI32Document(method)), "api.yaml")
			handler := func(c echo.Context) error { return c.NoContent(http.StatusOK) }
			builder.echo.Add(method, "/v1/widgets/:id", handler)
			builder.echo.POST("/v1/widgets/:id", handler)
			builder.echo.Add(method, "/v10/widgets/:id", handler)
			if err := builder.prepare(); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name, method, path, body string
				status                   int
			}{
				{"valid", method, "/v1/widgets/42", `{"kind":"widget"}`, 200},
				{"invalid const", method, "/v1/widgets/42", `{"kind":"other"}`, 400},
				{"invalid path parameter", method, "/v1/widgets/abc", `{"kind":"widget"}`, 400},
				{"wrong method", "POST", "/v1/widgets/42", `{"kind":"widget"}`, 405},
				{"unknown path", method, "/v1/missing", `{"kind":"widget"}`, 404},
				{"base path boundary", method, "/v10/widgets/42", `{"kind":"other"}`, 200},
			} {
				t.Run(tc.name, func(t *testing.T) {
					req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
					req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
					rec := httptest.NewRecorder()
					builder.echo.ServeHTTP(rec, req)
					if rec.Code != tc.status {
						t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
					}
				})
			}
		})
	}
}

func TestOpenAPI32Authorization(t *testing.T) {
	for _, method := range []string{"QUERY", "CUSTOM"} {
		t.Run(method, func(t *testing.T) {
			for _, tc := range []struct {
				name          string
				roles, scopes []string
				status        int
			}{
				{"allowed", []string{"reader"}, []string{"widgets:read"}, 200},
				{"missing JWT context", nil, nil, 401},
				{"insufficient role", []string{"other"}, []string{"widgets:read"}, 403},
				{"insufficient scope", []string{"reader"}, []string{"other"}, 403},
			} {
				t.Run(tc.name, func(t *testing.T) {
					builder := NewWebServer("authz32").WithRoleValidation(true).WithScopeValidation(true)
					// Supply the output of authentication to isolate operation authorization.
					builder.echo.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
						return func(c echo.Context) error {
							if tc.roles != nil {
								c.Set(ContextKey_Roles, tc.roles)
								c.Set(ContextKey_Scopes, tc.scopes)
							}
							return next(c)
						}
					})
					builder.UseOpenAPISpecs("/v1", writeOpenAPITestFile(t, openAPI32Document(method)), "api.yaml")
					builder.echo.Add(method, "/v1/widgets/:id", func(c echo.Context) error { return c.NoContent(200) })
					if err := builder.prepare(); err != nil {
						t.Fatal(err)
					}
					req := httptest.NewRequest(method, "/v1/widgets/42", strings.NewReader(`{"kind":"widget"}`))
					req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
					rec := httptest.NewRecorder()
					builder.echo.ServeHTTP(rec, req)
					if rec.Code != tc.status {
						t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
					}
				})
			}
		})
	}
}

func TestPublicAPIRetainsOpenAPI32Methods(t *testing.T) {
	for _, method := range []string{"QUERY", "CUSTOM"} {
		t.Run(method, func(t *testing.T) {
			builder := NewWebServer("public32").PublicAPI("/v1", writeOpenAPITestFile(t, openAPI32Document(method)), "api.yaml")
			if err := builder.prepare(); err != nil {
				t.Fatal(err)
			}
			paths := builder.specifications["api.yaml"].doc.Paths
			if paths.Value("/v1/widgets/{id}") == nil || paths.Value("/v1/private") != nil {
				t.Fatal("public path was removed or private path was retained")
			}
		})
	}
}

func TestOpenAPI32MissingCredentials(t *testing.T) {
	for _, method := range []string{"QUERY", "CUSTOM"} {
		t.Run(method, func(t *testing.T) {
			document := strings.Replace(openAPI32Document(method), `"paths":`, `"security":[{"BearerAuth":[]}],"components":{"securitySchemes":{"BearerAuth":{"type":"http","scheme":"bearer"}}},"paths":`, 1)
			builder := NewWebServer("secured32").UseOpenAPISpecs("/v1", writeOpenAPITestFile(t, document), "api.yaml")
			builder.echo.Add(method, "/v1/widgets/:id", func(c echo.Context) error { return c.NoContent(200) })
			if err := builder.prepare(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(method, "/v1/widgets/42", strings.NewReader(`{"kind":"widget"}`))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			builder.echo.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("missing credentials: status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestOpenAPI32RejectsMalformedAuthorization(t *testing.T) {
	for _, method := range []string{"QUERY", "CUSTOM"} {
		for _, invalid := range []struct{ old, replacement string }{
			{`"anyOf": ["reader"]`, `"anyOf": []`},
			{`"x-scopes": ["widgets:read"]`, `"x-scopes": []`},
		} {
			document := strings.Replace(openAPI32Document(method), invalid.old, invalid.replacement, 1)
			builder := NewWebServer("invalid-authz32").WithRoleValidation(true).WithScopeValidation(true).
				UseOpenAPISpecs("/v1", writeOpenAPITestFile(t, document), "api.yaml")
			if err := builder.prepare(); err == nil {
				t.Fatalf("%s accepted malformed authorization: %s", method, invalid.replacement)
			}
		}
	}
}

func TestInvalidOpenAPIPreventsStartup(t *testing.T) {
	for _, document := range []string{"{", `{"openapi":"3.2.0","info":{"title":"invalid"},"paths":{}}`, strings.Replace(openAPI32Document("QUERY"), `"query":`, `"unknownMethod":`, 1)} {
		t.Run(document[:1]+fmt.Sprint(len(document)), func(t *testing.T) {
			builder := NewWebServer("invalid").UseOpenAPISpecs("/v1", writeOpenAPITestFile(t, document), "invalid.yaml")
			server, err := builder.Start(0)
			if server != nil || err == nil || builder.echo.Listener != nil {
				t.Fatalf("invalid document opened listener: server=%v err=%v", server, err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "missing.json")
	builder := NewWebServer("reload").UseOpenAPISpecs("/v1", path, "api.yaml")
	if err := builder.prepare(); err == nil {
		t.Fatal("missing file should prevent startup")
	}
	builder.UseOpenAPISpecs("/v1", writeOpenAPITestFile(t, openAPI32Document("QUERY")), "api.yaml")
	if err := builder.prepare(); err != nil {
		t.Fatalf("valid reload did not clear previous error: %v", err)
	}
}
