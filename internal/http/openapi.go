package http

import (
	_ "embed"
	"net/http"
)

//go:embed openapi_spec.json
var openapiSpec []byte

// DocsHandler serves the OpenAPI spec and Swagger UI.
type DocsHandler struct {
	token string
}

// NewDocsHandler creates a handler for API documentation endpoints.
func NewDocsHandler(token string) *DocsHandler {
	return &DocsHandler{token: token}
}

// RegisterRoutes registers documentation routes on the given mux.
func (h *DocsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/openapi.json", h.handleSpec)
	mux.HandleFunc("GET /docs", h.handleSwaggerUI)
	mux.HandleFunc("GET /docs/", h.handleSwaggerUI)
}

func (h *DocsHandler) handleSpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(openapiSpec)
}

func (h *DocsHandler) handleSwaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(swaggerHTML))
}

const swaggerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>GoClaw API Documentation</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.18.2/swagger-ui.css">
  <style>
    html { box-sizing: border-box; overflow-y: scroll; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin: 0; background: #fafafa; }
    .topbar { display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.18.2/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: "/v1/openapi.json",
      dom_id: "#swagger-ui",
      deepLinking: true,
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: "BaseLayout",
      defaultModelsExpandDepth: 2,
      defaultModelExpandDepth: 2,
      tryItOutEnabled: true,
    });
  </script>
</body>
</html>`
