// Package api is the HTTP layer of amdl: the REST, SSE and WebSocket handlers
// under /api/v1, the Swagger UI at /docs and the embedded openapi.yaml served
// at /api/openapi.yaml. NewServer builds a Server from the runtime config
// store, the SQLite store, the event hub, the job manager and the wrapper,
// quality and developer-token services; (*Server).Routes returns the handler
// to serve, and (*Server).SetLibrarySync optionally attaches the library
// watcher (without it the library-sync endpoints answer 503).
//
// Invariants: openapi.yaml is the published contract for clients, so route
// and JSON-shape changes must update it. No endpoint is
// authenticated, deliberately; access control belongs to the deployment layer.
// Every JSON request body is read through decodeJSONBody (one JSON value, at
// most maxJSONBodyBytes, 413 when larger) and errors go out via writeError as
// {"error": "..."}. Routes wraps the mux in cors, then observeHTTP, which sets
// X-Request-ID and answers 500 for a panic raised before any response header.
package api
