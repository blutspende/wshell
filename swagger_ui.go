package wshell

import (
	"bytes"
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// swaggerUIAssets contains the official Swagger UI v5.32.0 distribution.
// Its license and third-party notices are included alongside the assets.
//
//go:embed internal/swaggerui/*
var swaggerUIAssets embed.FS

func newSwaggerUIHandler(urls []string) (echo.HandlerFunc, error) {
	type specURL struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	specs := make([]specURL, 0, len(urls))
	for _, name := range urls {
		specs = append(specs, specURL{URL: name, Name: name})
	}
	data, err := json.Marshal(specs)
	if err != nil {
		return nil, err
	}
	page, err := template.New("swagger").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>Swagger UI</title>
<link rel="icon" href="data:,">
<link rel="stylesheet" href="./swagger-ui.css"></head>
<body><div id="swagger-ui"></div>
<script src="./swagger-ui-bundle.js"></script>
<script src="./swagger-ui-standalone-preset.js"></script>
<script>window.onload = function () {
window.ui = SwaggerUIBundle({urls: {{.}}, dom_id: '#swagger-ui', deepLinking: true, validatorUrl: null,
presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset], layout: 'StandaloneLayout'});
};</script></body></html>`)
	if err != nil {
		return nil, err
	}
	var html bytes.Buffer
	// json.Marshal escapes HTML characters in caller-supplied names.
	if err := page.Execute(&html, template.JS(data)); err != nil {
		return nil, err
	}
	return func(c echo.Context) error {
		if c.Request().URL.RawQuery != "" {
			return c.Redirect(http.StatusFound, "/swagger")
		}
		name := c.Param("*")
		if name == "" || name == "index.html" {
			return c.Blob(http.StatusOK, echo.MIMETextHTMLCharsetUTF8, html.Bytes())
		}
		switch name {
		case "swagger-ui.css", "swagger-ui-bundle.js", "swagger-ui-standalone-preset.js":
			data, err := swaggerUIAssets.ReadFile("internal/swaggerui/" + name)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError).SetInternal(err)
			}
			contentType := "application/javascript"
			if strings.HasSuffix(name, ".css") {
				contentType = "text/css"
			}
			return c.Blob(http.StatusOK, contentType, data)
		default:
			return echo.NewHTTPError(http.StatusNotFound)
		}
	}, nil
}
