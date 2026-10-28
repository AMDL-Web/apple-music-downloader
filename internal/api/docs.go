package api

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openAPISpec []byte

// swaggerUIHTML is the /docs page. It loads Swagger UI from unpkg.com at
// runtime, so it needs internet access; the page says so for offline installs.

const swaggerUIHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AMDL API reference</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.32.8/swagger-ui.css">
</head>
<body>
  <noscript>This page loads Swagger UI from unpkg.com and needs JavaScript and internet access. The raw contract is at <a href="/api/openapi.yaml">/api/openapi.yaml</a>.</noscript>
  <p style="font: 14px sans-serif; margin: 12px 20px">Swagger UI is loaded from unpkg.com, so this page needs internet access. Offline, read the contract at <a href="/api/openapi.yaml">/api/openapi.yaml</a>.</p>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.32.8/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.onload = function () {
      window.ui = SwaggerUIBundle({
        url: "/api/openapi.yaml",
        dom_id: "#swagger-ui",
        deepLinking: true,
        displayRequestDuration: true,
        tryItOutEnabled: true
      });
    };
  </script>
</body>
</html>`

func swaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerUIHTML))
}

func openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(openAPISpec)
}
