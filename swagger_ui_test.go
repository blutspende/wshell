package wshell

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSwaggerUI(t *testing.T) {
	builder := NewWebServer("swagger32").EnableSwagger(true).
		UseOpenAPISpecs("/v1", writeOpenAPITestFile(t, openAPI32Document("QUERY")), "z.yaml").
		UseOpenAPISpecs("/v2", writeOpenAPITestFile(t, openAPI32Document("CUSTOM")), "a.yaml").
		WithPrimarySpec("z.yaml")
	if err := builder.prepare(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, contentType, contains string
		status                      int
	}{
		{"/swagger", "", "", 302},
		{"/swagger/index.html", "text/html", `"url":"z.yaml","name":"z.yaml"},{"url":"a.yaml"`, 200},
		{"/swagger/swagger-ui-bundle.js", "application/javascript", "5.32.0", 200},
		{"/swagger/swagger-ui-standalone-preset.js", "application/javascript", "SwaggerUIStandalonePreset", 200},
		{"/swagger/swagger-ui.css", "text/css", ".swagger-ui", 200},
		{"/swagger/z.yaml", "application/yaml", "3.2.0", 200},
		{"/swagger/z.json", "application/json", `"query"`, 200},
		{"/swagger/missing.js", "", "", 404},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			builder.echo.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.status || !strings.Contains(rec.Header().Get("Content-Type"), tc.contentType) || !strings.Contains(rec.Body.String(), tc.contains) {
				t.Fatalf("status=%d type=%s expected status=%d type=%s content=%q", rec.Code, rec.Header().Get("Content-Type"), tc.status, tc.contentType, tc.contains)
			}
		})
	}
}
