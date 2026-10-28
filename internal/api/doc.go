// Package api is the HTTP layer of amdl: the REST and SSE handlers under
// /api/v1, the web console mounted at /console/ (with / redirecting there),
// the Swagger UI at /docs and the embedded openapi.yaml served at
// /api/openapi.yaml. New builds a Server from Deps; (*Server).Routes returns
// the handler to serve. Optional dependencies left nil make their routes
// answer 503 feature_unavailable.
//
// Invariants: openapi.yaml is the published contract for clients, so route
// and JSON-shape changes must update it, and the route table in
// (*Server).routes must match it. No endpoint is authenticated, deliberately;
// access control belongs to the deployment layer. Every JSON request body is
// read through decodeJSONBody (one JSON value, at most maxJSONBodyBytes, 413
// when larger), and every error goes out via writeError as
// {"error": {"code", "message", "details"?}}. Routes wraps the mux in cors,
// then observeHTTP, which sets X-Request-ID and answers 500 internal_error for
// a panic raised before any response header.
package api
