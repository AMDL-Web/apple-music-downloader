package api

import _ "embed"

// OpenAPI is internal/api/openapi.yaml, the contract of every route. The server
// does not serve it; the amdl CLI and MCP server build their commands and tool
// schemas from it (internal/capability), and the parity tests check the route
// table, error codes and runtime config against it.
//
//go:embed openapi.yaml
var OpenAPI []byte
